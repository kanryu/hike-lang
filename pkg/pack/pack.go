// Package pack implements the native packaging phase for hikepack.
// It is deliberately separate from the compiler/code-generation packages.
package pack

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	gocode "hikec-go/pkg/codegen/go"
	"hikec-go/pkg/mod"
	"hikec-go/pkg/target"
)

// BuildSyso reads hike.mod below moduleDir and builds the configured native
// packages for targetName. External archives are relocated into Go-linkable
// COFF .syso objects next to the generated bindings.
func BuildSyso(moduleDir, targetName string, verbose bool) error {
	tgt, err := target.ParseTarget(targetName)
	if err != nil {
		return err
	}
	m, err := mod.FindModuleRoot(moduleDir)
	if err != nil {
		return fmt.Errorf("read hike.mod: %w", err)
	}
	if len(m.NativePackages) == 0 {
		return fmt.Errorf("no native package configuration found in %s", filepath.Join(m.RootDir, "hike.mod"))
	}

	packages := make([]string, 0, len(m.NativePackages))
	for packagePath := range m.NativePackages {
		packages = append(packages, packagePath)
	}
	sort.Strings(packages)
	built := 0
	for _, packagePath := range packages {
		config, ok := m.NativeConfig(packagePath, targetName)
		if !ok && tgt.Name != targetName {
			config, ok = m.NativeConfig(packagePath, tgt.Name)
		}
		if !ok {
			continue
		}
		built++
		packageDir := filepath.Clean(filepath.Join(m.RootDir, filepath.FromSlash(strings.TrimPrefix(packagePath, "./"))))
		if verbose {
			fmt.Printf("[hikepack] package %s (%s)\n", packagePath, targetName)
		}
		outputSyso, err := gocode.OutputSysoPath(packageDir, targetName)
		if err != nil {
			return err
		}
		var tempDir, tempObject string
		if config.HikeObject {
			tempDir, err = os.MkdirTemp(packageDir, ".hikepack-")
			if err != nil {
				return fmt.Errorf("create temporary packaging directory: %w", err)
			}
			tempObject = filepath.Join(tempDir, filepath.Base(outputSyso)+".o")
			defer os.RemoveAll(tempDir)
		}
		if err := gocode.BuildGoPackage(gocode.GoBuildOptions{Dir: packageDir, OutputFile: tempObject, TargetName: targetName, EmitSyso: config.HikeObject, Verbose: verbose}); err != nil {
			return fmt.Errorf("build package %s: %w", packagePath, err)
		}
		archives := make([]string, 0, len(config.Links))
		fileArchives := make([]string, 0, len(config.Links))
		for _, link := range config.Links {
			if strings.HasPrefix(link, "-") {
				archives = append(archives, link)
			} else if filepath.IsAbs(link) {
				archives = append(archives, link)
				fileArchives = append(fileArchives, link)
			} else {
				resolved := filepath.Join(m.RootDir, filepath.FromSlash(link))
				archives = append(archives, resolved)
				fileArchives = append(fileArchives, resolved)
			}
		}
		if len(fileArchives) > 0 {
			if config.HikeObject {
				if err := replaceFile(tempObject, outputSyso); err != nil {
					return fmt.Errorf("write Hike object %s: %w", outputSyso, err)
				}
			} else if err := os.Remove(outputSyso); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stale Hike syso %s: %w", outputSyso, err)
			}
			if err := emitArchivePackage(m, packageDir, packagePath, archives, outputSyso, targetName); err != nil {
				return fmt.Errorf("package external archives for %s: %w", packagePath, err)
			}
			continue
		}
		if len(archives) == 0 && config.HikeObject {
			if err := replaceFile(tempObject, outputSyso); err != nil {
				return fmt.Errorf("write Hike object %s: %w", outputSyso, err)
			}
			continue
		}
		objects := []string(nil)
		if config.HikeObject {
			objects = []string{tempObject}
		}
		if err := CreateSyso(archives, objects, outputSyso, targetName, verbose); err != nil {
			return fmt.Errorf("package %s: %w", packagePath, err)
		}
	}
	if built == 0 {
		return fmt.Errorf("no native package configuration found for target %q", targetName)
	}
	return nil
}

func emitArchivePackage(m *mod.Module, packageDir, packagePath string, archives []string, outputSyso, targetName string) error {
	tgt, err := target.ParseTarget(targetName)
	if err != nil {
		return err
	}
	goos, goarch := resolveGoEnv(tgt)
	base := strings.TrimSuffix(filepath.Base(outputSyso), filepath.Ext(outputSyso))
	nativeDir := filepath.Join(filepath.Dir(packageDir), filepath.Base(packageDir)+"_native")
	if err := os.MkdirAll(nativeDir, 0755); err != nil {
		return err
	}
	for _, stale := range []string{
		filepath.Join(nativeDir, base+"_archive*.syso"),
	} {
		files, _ := filepath.Glob(stale)
		for _, file := range files {
			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stale native archive %q: %w", file, err)
			}
		}
	}
	if err := os.Remove(outputSyso); err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, old := range []string{filepath.Join(packageDir, "hikepack_archive_"+goos+".go")} {
		_ = os.Remove(old)
	}
	var linkFlags []string
	fileArchives := make([]string, 0, len(archives))
	for _, archive := range archives {
		if strings.HasPrefix(archive, "-") {
			linkFlags = append(linkFlags, archive)
			continue
		}
		fileArchives = append(fileArchives, archive)
	}
	importPath := moduleImportPath(m.Name, m.RootDir, nativeDir)
	if err := writeArchivePackageSource(packageDir, filepath.Base(nativeDir), importPath, goos, goarch, linkFlags); err != nil {
		return err
	}
	for _, archive := range fileArchives {
		if strings.HasPrefix(archive, "-") {
			continue
		}
		if _, err := os.Stat(archive); err != nil {
			return fmt.Errorf("external archive %q: %w", archive, err)
		}
	}
	// Keep Rust/C static archives byte-for-byte intact. A Rust std-enabled
	// staticlib contains its own archive layout, import members, and runtime
	// objects; flattening it with llvm-ar/clang -r can corrupt the COFF symbol
	// table. Go's external linker can consume a .syso file whose contents are
	// still an archive, so copy each archive without rewriting it.
	for i, archive := range fileArchives {
		name := fmt.Sprintf("%s_archive_%s_%s_%d.syso", base, goos, goarch, i)
		dst := filepath.Join(nativeDir, name)
		if err := copyFile(archive, dst); err != nil {
			return fmt.Errorf("copy external archive %q: %w", archive, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func writeArchivePackageSource(packageDir, nativePackage, importPath, goos, goarch string, linkFlags []string) error {
	packageName, err := detectPackageName(packageDir)
	if err != nil {
		return err
	}
	rootSource := filepath.Join(packageDir, "hikepack_archive_"+goos+".go")
	root := fmt.Sprintf("//go:build %s && %s\n\npackage %s\n\nimport _ %q\n", goos, goarch, packageName, importPath)
	if err := os.WriteFile(rootSource, []byte(root), 0644); err != nil {
		return err
	}
	nativeSource := filepath.Join(filepath.Dir(packageDir), nativePackage, "link_"+goos+"_"+goarch+".go")
	native := fmt.Sprintf("//go:build %s && %s\n\npackage cflatnative\n", goos, goarch)
	var cgoFlags []string
	for _, flag := range linkFlags {
		// Go's cgo security filter rejects arbitrary -Wl options. The archive
		// is already materialized as a .syso object, so -Wl,-u,<symbol> is not
		// needed; retain only ordinary library/search-path flags.
		if strings.HasPrefix(flag, "-l") || strings.HasPrefix(flag, "-L") {
			cgoFlags = append(cgoFlags, flag)
		}
	}
	if len(cgoFlags) > 0 {
		native += "\n// #cgo " + goos + "," + goarch + " LDFLAGS: " + strings.Join(cgoFlags, " ") + "\nimport \"C\"\n\n// Native system libraries are deferred to the final Go external link.\n"
	}
	return os.WriteFile(nativeSource, []byte(native), 0644)
}

func detectPackageName(dir string) (string, error) {
	re := regexp.MustCompile(`(?m)^\s*package[ \t]+([A-Za-z_][A-Za-z0-9_]*)`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return "", err
		}
		if match := re.FindSubmatch(data); len(match) == 2 {
			return string(match[1]), nil
		}
	}
	return "", fmt.Errorf("cannot determine Go package name in %s", dir)
}

func moduleImportPath(moduleName, rootDir, dir string) string {
	rel, err := filepath.Rel(rootDir, dir)
	if err != nil {
		rel = dir
	}
	return strings.TrimSuffix(moduleName, "/") + "/" + filepath.ToSlash(rel)
}

// ExtractArchives packages static archives into one Go-compatible .syso
// object. The name is retained for compatibility with the earlier pack API.
func ExtractArchives(archives []string, outputSyso, targetName string, verbose bool) error {
	return CreateSyso(archives, nil, outputSyso, targetName, verbose)
}

// CreateSyso combines archive files and object files with an LLVM MRI script,
// then relocates the temporary archive with clang -r into one .syso object.
// Keeping the archive temporary avoids making Go treat the .syso as a static
// archive during external linking.
func CreateSyso(archives, objects []string, outputSyso, targetName string, verbose bool) error {
	tgt, err := target.ParseTarget(targetName)
	if err != nil {
		return fmt.Errorf("target error: %w", err)
	}
	outputDir := filepath.Dir(outputSyso)
	base := strings.TrimSuffix(filepath.Base(outputSyso), filepath.Ext(outputSyso))
	goos, goarch := resolveGoEnv(tgt)
	stalePattern := filepath.Join(outputDir, fmt.Sprintf("%s_archive*_%s_%s.syso", base, goos, goarch))
	staleFiles, _ := filepath.Glob(stalePattern)
	for _, stale := range staleFiles {
		if err := os.Remove(stale); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove stale archive object %q: %w", stale, err)
		}
	}

	tempDir, err := os.MkdirTemp(outputDir, ".hikepack-archive-")
	if err != nil {
		return fmt.Errorf("failed to create archive temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	combinedArchive := filepath.Join(tempDir, base+".a")
	_, err = createMRIArchive(archives, objects, combinedArchive, verbose)
	if err != nil {
		return err
	}
	clangArgs := []string{"-r", "-o", outputSyso}
	if tgt.Triple != "" {
		clangArgs = append(clangArgs, "--target="+tgt.Triple)
	}
	clangArgs = append(clangArgs, "-nostdlib")
	// An archive renamed to .syso is not searched as a Go host object. Force
	// every archive member into the relocatable COFF object so the Go linker
	// can resolve symbols called by generated assembly stubs.
	clangArgs = append(clangArgs, "-Wl,--whole-archive", combinedArchive, "-Wl,--no-whole-archive")
	// System/import libraries must not be passed here. They are deferred to the
	// final Go external link by the generated native package's cgo LDFLAGS. If
	// they were passed alongside --whole-archive, every PE import member would
	// be absorbed and duplicate definitions would be produced.
	if verbose {
		fmt.Printf("[hikepack] Running: clang %s\n", strings.Join(clangArgs, " "))
	}
	cmd := exec.Command("clang", clangArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clang archive relocation failed: %w", err)
	}
	// Rust staticlibs can carry compiler bitcode in .llvmbc sections. Those
	// sections are not needed by the Go external linker and can produce invalid
	// PE section placement on Windows, so remove them from the final .syso.
	objcopy := exec.Command("llvm-objcopy", "--remove-section=.llvmbc", outputSyso)
	objcopy.Stdout = os.Stdout
	objcopy.Stderr = os.Stderr
	if err := objcopy.Run(); err != nil {
		// Some large COFF files emitted by GNU ld have no string table even
		// though the relocatable object is usable. In that case there is no
		// .llvmbc section to remove; keep the linker output and let Go validate
		// it at the final link step.
		if verbose {
			fmt.Printf("[hikepack] warning: skipping optional LLVM bitcode stripping: %v\n", err)
		}
	}
	return nil
}

func createMRIArchive(archives, objects []string, outputArchive string, verbose bool) ([]string, error) {
	var script strings.Builder
	var linkFlags []string
	script.WriteString("CREATE ")
	script.WriteString(mriQuote(outputArchive))
	script.WriteByte('\n')
	for _, archive := range archives {
		if strings.HasPrefix(archive, "-") {
			linkFlags = append(linkFlags, archive)
			continue
		}
		absArchive, err := filepath.Abs(archive)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve archive path %q: %w", archive, err)
		}
		if _, err := os.Stat(absArchive); err != nil {
			return nil, fmt.Errorf("external archive %q: %w", archive, err)
		}
		filteredArchive, err := filterImportLibraryMembers(absArchive, filepath.Dir(outputArchive))
		if err != nil {
			return nil, err
		}
		script.WriteString("ADDLIB ")
		script.WriteString(mriQuote(filteredArchive))
		script.WriteByte('\n')
	}
	for _, object := range objects {
		absObject, err := filepath.Abs(object)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve object path %q: %w", object, err)
		}
		if _, err := os.Stat(absObject); err != nil {
			return nil, fmt.Errorf("object file %q: %w", object, err)
		}
		script.WriteString("ADDMOD ")
		script.WriteString(mriQuote(absObject))
		script.WriteByte('\n')
	}
	script.WriteString("SAVE\nEND\n")
	if verbose {
		fmt.Printf("[hikepack] Running: llvm-ar -M for %s\n", outputArchive)
	}
	cmd := exec.Command("llvm-ar", "-M")
	cmd.Stdin = strings.NewReader(script.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("llvm-ar MRI archive creation failed: %w", err)
	}
	return linkFlags, nil
}

// filterImportLibraryMembers removes embedded PE import-library members from a
// Rust staticlib. Rust's std/GUI dependency graph can copy the same
// user32.dll, kernel32.dll, etc. member hundreds of times. ADDLIB would keep
// all copies, and clang -r would either report duplicate definitions or emit a
// malformed COFF object. The final Go link receives the system -l flags
// separately, so these members are not needed in the relocatable archive.
func filterImportLibraryMembers(archive, tempParent string) (string, error) {
	dir, err := os.MkdirTemp(tempParent, ".hikepack-filter-")
	if err != nil {
		return "", fmt.Errorf("create filtered archive directory: %w", err)
	}
	// The filtered archive is consumed before createMRIArchive returns.
	defer os.RemoveAll(dir)

	extract := exec.Command("llvm-ar", "x", archive)
	extract.Dir = dir
	extract.Stdout = os.Stdout
	extract.Stderr = os.Stderr
	if err := extract.Run(); err != nil {
		return "", fmt.Errorf("extract archive %q: %w", archive, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read extracted archive %q: %w", archive, err)
	}
	objects := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(strings.ToLower(entry.Name()), ".dll") {
			if !entry.IsDir() {
				if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
					return "", fmt.Errorf("remove import member %q: %w", entry.Name(), err)
				}
			}
			continue
		}
		objects = append(objects, entry.Name())
	}
	filtered := filepath.Join(tempParent, filepath.Base(archive)+".filtered.a")
	_ = os.Remove(filtered)
	if len(objects) == 0 {
		return "", fmt.Errorf("archive %q contains no non-import object members", archive)
	}
	var script strings.Builder
	script.WriteString("CREATE ")
	script.WriteString(mriQuote(filtered))
	script.WriteByte('\n')
	for _, object := range objects {
		script.WriteString("ADDMOD ")
		script.WriteString(mriQuote(filepath.Join(dir, object)))
		script.WriteByte('\n')
	}
	script.WriteString("SAVE\nEND\n")
	create := exec.Command("llvm-ar", "-M")
	create.Stdin = strings.NewReader(script.String())
	create.Stdout = os.Stdout
	create.Stderr = os.Stderr
	if err := create.Run(); err != nil {
		return "", fmt.Errorf("create filtered archive %q: %w", archive, err)
	}
	return filtered, nil
}

func replaceFile(src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
}

func mriQuote(path string) string {
	return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
}

func resolveGoEnv(tgt *target.Target) (string, string) {
	if tgt == nil {
		return runtime.GOOS, runtime.GOARCH
	}
	switch {
	case strings.Contains(tgt.Triple, "windows"):
		return "windows", "amd64"
	case strings.Contains(tgt.Triple, "linux"):
		return "linux", "amd64"
	case strings.Contains(tgt.Triple, "darwin"):
		return "darwin", "arm64"
	default:
		return runtime.GOOS, runtime.GOARCH
	}
}
