package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hikec-go/pkg/mod"
)

// runGet downloads Hike modules into .hike/deps, following the same basic
// convention as `go get`: the module path determines both the clone URL and
// the local dependency path.
func runGet(args []string) {
	module, err := mod.FindModuleRoot(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: find module root: %v\n", err)
		os.Exit(1)
	}

	requests := make(map[string]string)
	order := make([]string, 0)
	explicitRequests := len(args) > 0
	if len(args) == 0 {
		for _, path := range module.RequireOrder {
			requests[path] = module.Requires[path]
			order = append(order, path)
		}
		// Modules constructed programmatically or older callers may not have
		// RequireOrder populated; retain deterministic fallback behavior.
		if len(order) == 0 {
			for path, version := range module.Requires {
				requests[path] = version
				order = append(order, path)
			}
			sort.Strings(order)
		}
	} else {
		for _, arg := range args {
			path, version := splitModuleVersion(arg)
			if path == "" {
				fmt.Fprintln(os.Stderr, "Error: get requires a module path")
				os.Exit(1)
			}
			if version == "" {
				version = module.Requires[path]
				if version == "" {
					version = "latest"
				}
			}
			requests[path] = version
			order = append(order, path)
		}
	}

	if len(requests) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no modules specified and hike.mod has no require directives")
		os.Exit(1)
	}

	for _, path := range order {
		if err := fetchModule(module, path, requests[path], explicitRequests); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if err := ensureRequirements(module, requests); err != nil {
		fmt.Fprintf(os.Stderr, "Error: update hike.mod: %v\n", err)
		os.Exit(1)
	}
}

func splitModuleVersion(arg string) (string, string) {
	if idx := strings.LastIndex(arg, "@"); idx > 0 {
		return arg[:idx], arg[idx+1:]
	}
	return arg, ""
}

func fetchModule(module *mod.Module, modulePath, version string, replaceExisting bool) error {
	if filepath.IsAbs(modulePath) || filepath.Clean(filepath.FromSlash(modulePath)) != filepath.FromSlash(modulePath) || strings.HasPrefix(modulePath, "../") {
		return fmt.Errorf("invalid module path %q", modulePath)
	}
	if !strings.Contains(modulePath, "/") {
		return fmt.Errorf("module path %q must include a host", modulePath)
	}

	depsRoot := filepath.Join(module.RootDir, ".hike", "deps")
	destination := filepath.Join(depsRoot, filepath.FromSlash(modulePath))
	if rel, err := filepath.Rel(depsRoot, destination); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("module path escapes dependency directory: %q", modulePath)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}

	if version != "" && version != "latest" {
		return downloadModuleArchive(module.RootDir, destination, modulePath, version)
	}

	if replaceExisting {
		if err := os.RemoveAll(destination); err != nil {
			return fmt.Errorf("replace existing dependency %s: %w", destination, err)
		}
	}

	if _, err := os.Stat(filepath.Join(destination, ".git")); err == nil {
		cmd := exec.Command("git", "-C", destination, "pull", "--ff-only")
		if output, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("update %s: %w\n%s", modulePath, runErr, strings.TrimSpace(string(output)))
		}
	} else {
		if _, statErr := os.Stat(destination); statErr == nil {
			return fmt.Errorf("dependency directory already exists and is not a Git checkout: %s", destination)
		}
		url := "https://" + modulePath + ".git"
		cmd := exec.Command("git", "clone", url, destination)
		if output, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("clone %s: %w\n%s", modulePath, runErr, strings.TrimSpace(string(output)))
		}
	}

	fmt.Printf("Downloaded %s@%s -> %s\n", modulePath, version, destination)
	return nil
}

func downloadModuleArchive(rootDir, destination, modulePath, version string) error {
	if !strings.HasPrefix(modulePath, "github.com/") {
		return fmt.Errorf("source archives are currently supported for github.com modules only: %s", modulePath)
	}
	archiveURL := "https://" + modulePath + "/archive/refs/tags/" + url.PathEscape(version) + ".tar.gz"
	client := &http.Client{Timeout: 2 * time.Minute}
	response, err := client.Get(archiveURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", archiveURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned HTTP %s", archiveURL, response.Status)
	}

	depsRoot := filepath.Join(rootDir, ".hike", "deps")
	stage, err := os.MkdirTemp(depsRoot, ".archive-*")
	if err != nil {
		return fmt.Errorf("create archive staging directory: %w", err)
	}
	defer os.RemoveAll(stage)

	gz, err := gzip.NewReader(response.Body)
	if err != nil {
		return fmt.Errorf("open source archive: %w", err)
	}
	defer gz.Close()
	if err := extractSourceArchive(gz, stage); err != nil {
		return fmt.Errorf("extract %s: %w", modulePath, err)
	}

	if err := os.RemoveAll(destination); err != nil {
		return fmt.Errorf("replace existing dependency %s: %w", destination, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	if err := os.Rename(stage, destination); err != nil {
		return fmt.Errorf("install dependency %s: %w", destination, err)
	}
	fmt.Printf("Downloaded %s@%s source archive -> %s\n", modulePath, version, destination)
	return nil
}

func extractSourceArchive(source io.Reader, destination string) error {
	reader := tar.NewReader(source)
	prefix := ""
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(header.Name)
		parts := strings.Split(name, "/")
		if len(parts) < 2 {
			continue
		}
		if prefix == "" {
			prefix = parts[0] + "/"
		}
		if !strings.HasPrefix(name, prefix) {
			return fmt.Errorf("archive contains multiple roots")
		}
		rel := strings.TrimPrefix(name, prefix)
		if rel == "" {
			continue
		}
		if filepath.IsAbs(filepath.FromSlash(rel)) || rel == ".." || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("archive path escapes destination: %q", header.Name)
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("archive links are not supported: %q", header.Name)
		}
	}
}

func ensureRequirements(module *mod.Module, requests map[string]string) error {
	path := filepath.Join(module.RootDir, "hike.mod")
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	lines := make([]string, 0)
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	updated := make(map[string]bool)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "require" {
			if version, ok := requests[parts[1]]; ok {
				lines = append(lines, fmt.Sprintf("require %s %s", parts[1], version))
				updated[parts[1]] = true
				seen[parts[1]] = true
				continue
			}
			seen[parts[1]] = true
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	paths := make([]string, 0, len(requests))
	for path := range requests {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !updated[path] && !seen[path] {
			lines = append(lines, fmt.Sprintf("require %s %s", path, requests[path]))
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}
