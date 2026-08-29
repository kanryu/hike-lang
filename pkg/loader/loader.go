package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

type Loader struct {
	loadedFiles map[string]bool
	allDecls    []ast.Decl
	imports     []*ast.ImportDecl
	verbose     bool
}

func New(verbose bool) *Loader {
	return &Loader{
		loadedFiles: make(map[string]bool),
		allDecls:    []ast.Decl{},
		imports:     []*ast.ImportDecl{},
		verbose:     verbose,
	}
}

func (l *Loader) Load(entryPath string) (*ast.Program, error) {
	absPath, err := filepath.Abs(entryPath)
	if err != nil {
		return nil, err
	}

	err = l.loadFileRecursive(absPath)
	if err != nil {
		return nil, err
	}

	return &ast.Program{
		Package: "main",
		Imports: l.imports,
		Decls:   l.allDecls,
	}, nil
}

func (l *Loader) loadFileRecursive(absPath string) error {
	if l.loadedFiles[absPath] {
		return nil
	}
	l.loadedFiles[absPath] = true

	if l.verbose {
		fmt.Printf("[LOADER] Parsing file: %s\n", absPath)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", absPath, err)
	}

	lex := lexer.New(string(content))
	p := parser.New(lex)
	p.SetVerbose(l.verbose)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return fmt.Errorf("parse error in %s: %s", absPath, strings.Join(p.Errors(), "; "))
	}

	// 非mainパッケージの関数・定数の名前マングリング
	if prog.Package != "main" && prog.Package != "" {
		for _, decl := range prog.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fd.Body != nil && fd.Receiver == nil {
					if !strings.HasPrefix(fd.Name.Value, prog.Package+"_") {
						fd.Name.Value = prog.Package + "_" + fd.Name.Value
					}
				}
			}
			if cd, ok := decl.(*ast.ConstDecl); ok {
				if !strings.HasPrefix(cd.Name.Value, prog.Package+"_") {
					cd.Name.Value = prog.Package + "_" + cd.Name.Value
				}
			}
		}
	}

	// インポート先の再帰的読み込み
	baseDir := filepath.Dir(absPath)
	for _, imp := range prog.Imports {
		var targetPath string
		cleanPath := strings.Trim(imp.Path, "\"")

		if strings.HasPrefix(cleanPath, "std/") || strings.HasPrefix(cleanPath, "hikec/") {
			rootDir := l.findProjectRoot(baseDir)
			targetPath = filepath.Join(rootDir, filepath.FromSlash(cleanPath))
		} else {
			targetPath = filepath.Join(baseDir, filepath.FromSlash(cleanPath))
		}

		if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
			files, _ := filepath.Glob(filepath.Join(targetPath, "*.hike"))
			for _, f := range files {
				if err := l.loadFileRecursive(f); err != nil {
					return err
				}
			}
		} else {
			if !strings.HasSuffix(targetPath, ".hike") {
				targetPath += ".hike"
			}
			if err := l.loadFileRecursive(targetPath); err != nil {
				return err
			}
		}
	}

	l.allDecls = append(l.allDecls, prog.Decls...)
	return nil
}

func (l *Loader) findProjectRoot(startDir string) string {
	dir := startDir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return startDir
}
