package mod

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Module struct {
	Name    string // モジュール名 (例: hike-lang)
	Version string // Hikeバージョン
	RootDir string // hike.mod が存在する絶対パス
}

// 親ディレクトリを遡って hike.mod を探索
func FindModuleRoot(startDir string) (*Module, error) {
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}

	cur := absDir
	for {
		modFile := filepath.Join(cur, "hike.mod")
		if fi, err := os.Stat(modFile); err == nil && !fi.IsDir() {
			return parseModFile(modFile, cur)
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	return nil, fmt.Errorf("hike.mod not found in '%s' or any parent directories", startDir)
}

func parseModFile(modPath string, rootDir string) (*Module, error) {
	f, err := os.Open(modPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mod := &Module{RootDir: rootDir}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			switch parts[0] {
			case "module":
				mod.Name = parts[1]
			case "hike":
				mod.Version = parts[1]
			}
		}
	}

	if mod.Name == "" {
		mod.Name = filepath.Base(rootDir)
	}

	return mod, nil
}

// インポートパス（"std/json", "examples/json" 等）をファイルシステム上の絶対パスに解決
func (m *Module) ResolvePackagePath(fromDir string, importPath string) (string, error) {
	// 1. 相対パス (./ または ../)
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		target := filepath.Join(fromDir, importPath)
		if fi, err := os.Stat(target); err == nil && fi.IsDir() {
			return target, nil
		}
		return "", fmt.Errorf("relative package directory not found: %s", target)
	}

	// 2. モジュールルート起点 (std/json, pkg/parser など)
	// 自モジュール名プレフィックスが指定されている場合 (hike-lang/std/json) は除去
	cleanPath := importPath
	if strings.HasPrefix(cleanPath, m.Name+"/") {
		cleanPath = strings.TrimPrefix(cleanPath, m.Name+"/")
	}

	modTarget := filepath.Join(m.RootDir, cleanPath)
	if fi, err := os.Stat(modTarget); err == nil && fi.IsDir() {
		return modTarget, nil
	}

	// 3. コンパイラ組み込み標準ライブラリ（フォールバック）
	if exePath, err := os.Executable(); err == nil {
		exeStd := filepath.Join(filepath.Dir(exePath), "..", cleanPath)
		if fi, err := os.Stat(exeStd); err == nil && fi.IsDir() {
			return exeStd, nil
		}
	}

	return "", fmt.Errorf("package '%s' not found in module '%s' (checked: %s)", importPath, m.Name, modTarget)
}
