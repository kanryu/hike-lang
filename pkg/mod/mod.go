package mod

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Module struct {
	Name           string            // モジュール名 (例: hike-lang)
	Version        string            // Hikeバージョン
	RootDir        string            // hike.mod が存在する絶対パス
	Replaces       map[string]string // replace ディレクティブ (例: "std/json" => "../../std/json")
	Requires       map[string]string // require ディレクティブ (例: "github.com/example/lib" => "v0.1.0")
	RequireOrder   []string          // require ディレクティブの宣言順
	NativePackages map[string]map[string]NativeTarget
}

type NativeTarget struct {
	Links  []string
	Assets []string
}

type NativeDependency struct {
	ModuleRoot  string
	PackagePath string
	Links       []string
	Assets      []string
}

// cloneModuleString detaches persisted module metadata from scanner-backed
// strings. The self-hosted string scanner may reuse its input buffer between
// lines, so retaining a substring view would corrupt names and map keys on the
// next scan.
func cloneModuleString(value string) string {
	buf := make([]byte, len(value))
	for i := 0; i < len(value); i++ {
		buf[i] = value[i]
	}
	return string(buf)
}

// FindModuleRoot は開始ディレクトリから親ディレクトリを遡り、hike.mod を探索してモジュール情報を構築します
func FindModuleRoot(startDir string) (*Module, error) {
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		cwd, _ := os.Getwd()
		return newSyntheticModule(cwd), err
	}

	cur := absDir
	for {
		modFile := filepath.Join(cur, "hike.mod")
		if fi, statErr := os.Stat(modFile); statErr == nil {
			if fi.IsDir() {
				return newSyntheticModule(cur), fmt.Errorf("module file is a directory: %s", modFile)
			}
			return parseModFile(modFile, cur)
		} else if !os.IsNotExist(statErr) {
			return newSyntheticModule(cur), fmt.Errorf("stat module file %s: %w", modFile, statErr)
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	// hike.mod が見つからない場合は開始ディレクトリをルートとする
	// 仮モジュールを生成する。これはHikeの単一ファイル利用を許可する
	// 正常系であり、エラーにはしない。
	return newSyntheticModule(absDir), nil
}

func newSyntheticModule(rootDir string) *Module {
	return &Module{
		Name:           filepath.Base(rootDir),
		RootDir:        rootDir,
		Replaces:       make(map[string]string),
		Requires:       make(map[string]string),
		NativePackages: make(map[string]map[string]NativeTarget),
	}
}

func parseModFile(modPath string, rootDir string) (*Module, error) {
	f, err := os.Open(modPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mod := &Module{
		RootDir:        rootDir,
		Replaces:       make(map[string]string),
		Requires:       make(map[string]string),
		NativePackages: make(map[string]map[string]NativeTarget),
	}

	hasModuleName := false
	packagePath := ""
	targetName := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			if parts[0] == "module" {
				return mod, fmt.Errorf("invalid module directive in %s", modPath)
			}
			if line == "}" {
				if targetName != "" {
					targetName = ""
				} else if packagePath != "" {
					packagePath = ""
				}
			}
			continue
		}

		switch parts[0] {
		case "module":
			mod.Name = cloneModuleString(parts[1])
			hasModuleName = true
		case "hike":
			mod.Version = cloneModuleString(parts[1])
		case "require":
			if len(parts) >= 3 {
				path := cloneModuleString(parts[1])
				version := cloneModuleString(parts[2])
				if _, exists := mod.Requires[path]; !exists {
					mod.RequireOrder = append(mod.RequireOrder, path)
				}
				mod.Requires[path] = version
			}
		case "replace", "GoReplace":
			// 形式1: replace std/json => ../../std/json
			// 形式2: replace std/json ../../std/json
			if len(parts) >= 4 && parts[2] == "=>" {
				mod.Replaces[cloneModuleString(parts[1])] = cloneModuleString(parts[3])
			} else if len(parts) >= 3 {
				mod.Replaces[cloneModuleString(parts[1])] = cloneModuleString(parts[2])
			}
		}

		// Native package configuration is intentionally parsed in addition to
		// the line-oriented module directives above. Unknown nested directives
		// remain harmless for older module files.
		if strings.HasPrefix(line, "package ") {
			packagePath = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "package ")), "{")
			packagePath = strings.TrimSpace(packagePath)
			if _, ok := mod.NativePackages[packagePath]; !ok {
				mod.NativePackages[packagePath] = make(map[string]NativeTarget)
			}
			continue
		}
		if strings.HasPrefix(line, "target ") && packagePath != "" {
			targetName = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "target ")), "{")
			targetName = strings.TrimSpace(targetName)
			if _, ok := mod.NativePackages[packagePath][targetName]; !ok {
				mod.NativePackages[packagePath][targetName] = NativeTarget{}
			}
			continue
		}
		if targetName != "" && (strings.HasPrefix(line, "link:") || strings.HasPrefix(line, "assets:")) {
			value := quotedModuleValue(line)
			if value != "" {
				config := mod.NativePackages[packagePath][targetName]
				if strings.HasPrefix(line, "link:") {
					config.Links = append(config.Links, value)
				} else {
					config.Assets = append(config.Assets, value)
				}
				mod.NativePackages[packagePath][targetName] = config
			}
			continue
		}
		if line == "}" {
			if targetName != "" {
				targetName = ""
			} else if packagePath != "" {
				packagePath = ""
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return mod, fmt.Errorf("read module file %s: %w", modPath, err)
	}
	if !hasModuleName || mod.Name == "" {
		return mod, fmt.Errorf("module directive not found in %s", modPath)
	}

	return mod, nil
}

func quotedModuleValue(line string) string {
	first := strings.IndexByte(line, '"')
	if first < 0 {
		return ""
	}
	last := strings.LastIndexByte(line, '"')
	if last <= first {
		return ""
	}
	return line[first+1 : last]
}

func (m *Module) NativeConfig(packagePath, targetName string) (NativeTarget, bool) {
	if m == nil {
		return NativeTarget{}, false
	}
	packagePath = filepath.ToSlash(filepath.Clean(packagePath))
	packagePath = strings.TrimPrefix(packagePath, "./")
	for key, targets := range m.NativePackages {
		cleanKey := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(key)), "./")
		if cleanKey != packagePath {
			continue
		}
		config, ok := targets[targetName]
		return config, ok
	}
	return NativeTarget{}, false
}

// ResolvePackagePath はインポートパスをファイルシステム上の絶対パスディレクトリに解決します
func (m *Module) ResolvePackagePath(fromDir string, importPath string) (string, error) {
	// 1. 相対パス指定 (./ または ../)
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		target := filepath.Clean(filepath.Join(fromDir, importPath))
		if fi, err := os.Stat(target); err == nil && fi.IsDir() {
			return target, nil
		}
		return "", fmt.Errorf("relative package directory not found: %s", target)
	}

	// 2. hike.mod の replace ディレクティブ判定
	if m.Replaces != nil {
		// Prefer the most specific replacement. For example, a replacement
		// for "github.com/acme/project/internal/helpers" must win over the
		// module-wide replacement for "github.com/acme/project".
		bestFrom := ""
		bestTarget := ""
		for fromMod, targetRel := range m.Replaces {
			if (importPath == fromMod || strings.HasPrefix(importPath, fromMod+"/")) && len(fromMod) > len(bestFrom) {
				bestFrom = fromMod
				bestTarget = targetRel
			}
		}
		if bestFrom != "" {
			relSub := strings.TrimPrefix(importPath, bestFrom)
			relSub = strings.TrimPrefix(relSub, "/")
			// A replacement may be absolute, which is common in generated or
			// self-hosting test modules. Do not prefix it with the module root;
			// filepath.Join does not discard an earlier root on every platform.
			basePath := bestTarget
			if !filepath.IsAbs(basePath) {
				basePath = filepath.Join(m.RootDir, basePath)
			}
			targetPath := filepath.Clean(filepath.Join(basePath, relSub))
			if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
				return targetPath, nil
			}
		}
	}

	// 自モジュール名プレフィックスが指定されている場合は除去
	cleanPath := importPath
	if m.Name != "" && strings.HasPrefix(cleanPath, m.Name+"/") {
		cleanPath = strings.TrimPrefix(cleanPath, m.Name+"/")
	}

	// 3. 取得済み依存モジュール。依存のモジュール名をそのまま
	// ディレクトリ階層へ写像するため、GitHub以外のホストも扱える。
	if m.RootDir != "" {
		depTarget := filepath.Join(m.RootDir, ".hike", "deps", filepath.FromSlash(importPath))
		if fi, err := os.Stat(depTarget); err == nil && fi.IsDir() {
			return depTarget, nil
		}
	}

	// 4. モジュールルート起点 (std/json, pkg/parser など)
	if m.RootDir != "" {
		modTarget := filepath.Join(m.RootDir, cleanPath)
		if fi, err := os.Stat(modTarget); err == nil && fi.IsDir() {
			return modTarget, nil
		}
		// Go-Hike's standard-library replacements are rooted below std in
		// the repository. Keep this fallback available when a self-hosted
		// module map cannot retain a replacement key across its scanner pass.
		stdTarget := filepath.Join(m.RootDir, "std", cleanPath)
		if fi, err := os.Stat(stdTarget); err == nil && fi.IsDir() {
			return stdTarget, nil
		}
		// cmd/hikec keeps its compatibility module two levels below the
		// repository root. Resolve repository-local packages there as well.
		repoRoot := filepath.Join(m.RootDir, "..", "..")
		for _, candidate := range []string{
			filepath.Join(repoRoot, cleanPath),
			filepath.Join(repoRoot, "std", cleanPath),
		} {
			if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
				return candidate, nil
			}
		}

	}

	// 5. コンパイラ実行バイナリ隣接標準ライブラリ（フォールバック）
	if exePath, err := os.Executable(); err == nil {
		exeStd := filepath.Join(filepath.Dir(exePath), "..", cleanPath)
		if fi, err := os.Stat(exeStd); err == nil && fi.IsDir() {
			return exeStd, nil
		}
	}

	// 6. 呼び出し元ファイルディレクトリ直下探索
	currTarget := filepath.Join(fromDir, importPath)
	if fi, err := os.Stat(currTarget); err == nil && fi.IsDir() {
		return currTarget, nil
	}

	return "", fmt.Errorf("package '%s' not found in module '%s' (checked: %s)", importPath, m.Name, filepath.Join(m.RootDir, cleanPath))
}
