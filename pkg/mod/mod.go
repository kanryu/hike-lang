package mod

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Module struct {
	Name         string            // モジュール名 (例: hike-lang)
	Version      string            // Hikeバージョン
	RootDir      string            // hike.mod が存在する絶対パス
	Replaces     map[string]string // replace ディレクティブ (例: "std/json" => "../../std/json")
	Requires     map[string]string // require ディレクティブ (例: "github.com/example/lib" => "v0.1.0")
	RequireOrder []string          // require ディレクティブの宣言順
}

// FindModuleRoot は開始ディレクトリから親ディレクトリを遡り、hike.mod を探索してモジュール情報を構築します
func FindModuleRoot(startDir string) (*Module, error) {
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		cwd, _ := os.Getwd()
		return &Module{RootDir: cwd, Replaces: make(map[string]string), Requires: make(map[string]string)}, err
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

	// hike.mod が見つからない場合はカレントディレクトリをルートとする仮モジュールを生成
	cwd, _ := os.Getwd()
	return &Module{
		Name:     filepath.Base(cwd),
		RootDir:  cwd,
		Replaces: make(map[string]string),
		Requires: make(map[string]string),
	}, nil
}

func parseModFile(modPath string, rootDir string) (*Module, error) {
	f, err := os.Open(modPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mod := &Module{
		RootDir:  rootDir,
		Replaces: make(map[string]string),
		Requires: make(map[string]string),
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		switch parts[0] {
		case "module":
			mod.Name = parts[1]
		case "hike":
			mod.Version = parts[1]
		case "require":
			if len(parts) >= 3 {
				if _, exists := mod.Requires[parts[1]]; !exists {
					mod.RequireOrder = append(mod.RequireOrder, parts[1])
				}
				mod.Requires[parts[1]] = parts[2]
			}
		case "replace":
			// 形式1: replace std/json => ../../std/json
			// 形式2: replace std/json ../../std/json
			if len(parts) >= 4 && parts[2] == "=>" {
				mod.Replaces[parts[1]] = parts[3]
			} else if len(parts) >= 3 {
				mod.Replaces[parts[1]] = parts[2]
			}
		}
	}

	if mod.Name == "" {
		mod.Name = filepath.Base(rootDir)
	}

	return mod, nil
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
		for fromMod, targetRel := range m.Replaces {
			if importPath == fromMod || strings.HasPrefix(importPath, fromMod+"/") {
				relSub := strings.TrimPrefix(importPath, fromMod)
				relSub = strings.TrimPrefix(relSub, "/")
				// A replacement may be absolute, which is common in generated or
				// self-hosting test modules. Do not prefix it with the module root;
				// filepath.Join does not discard an earlier root on every platform.
				basePath := targetRel
				if !filepath.IsAbs(basePath) {
					basePath = filepath.Join(m.RootDir, basePath)
				}
				targetPath := filepath.Clean(filepath.Join(basePath, relSub))
				if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
					return targetPath, nil
				}
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
