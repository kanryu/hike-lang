package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/codegen"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
	"hikec-go/pkg/sema"
)

type Compiler struct {
	loadedPkgs map[string]*ast.Program
}

func New() *Compiler {
	return &Compiler{
		loadedPkgs: make(map[string]*ast.Program),
	}
}

// pkg/compiler/compiler.go 内の parseFile
func (c *Compiler) parseFile(path string) (*ast.Program, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	l := lexer.New(string(src))
	p := parser.New(l)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parse errors in %s:\n%s", path, strings.Join(p.Errors(), "\n"))
	}

	return prog, nil
}

func (c *Compiler) loadPackage(importPath string) (*ast.Program, error) {
	if prog, exists := c.loadedPkgs[importPath]; exists {
		return prog, nil
	}

	// ディレクトリ内の全 .hike ファイルを探索
	files, err := filepath.Glob(filepath.Join(importPath, "*.hike"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("package not found: %s", importPath)
	}

	pkgProg := &ast.Program{
		Package: filepath.Base(importPath),
		Decls:   []ast.Decl{},
	}

	for _, file := range files {
		prog, err := c.parseFile(file)
		if err != nil {
			return nil, err
		}
		pkgProg.Decls = append(pkgProg.Decls, prog.Decls...)
	}

	c.loadedPkgs[importPath] = pkgProg
	return pkgProg, nil
}

func (c *Compiler) CompileFile(entryPath string) (string, error) {
	c.loadedPkgs = make(map[string]*ast.Program)
	entryProg, err := c.parseFile(entryPath)
	if err != nil {
		return "", err
	}

	// 単一の統合 AST プログラムを作成
	mergedProg := &ast.Program{
		Package: entryProg.Package,
		Decls:   []ast.Decl{},
	}

	// 1. 依存パッケージを再帰的にロードして結合
	for _, imp := range entryProg.Imports {
		pkgProg, err := c.loadPackage(imp.Path)
		if err != nil {
			return "", err
		}

		// パッケージ内の関数・型にプレフィックスを付与してシンボル衝突を回避
		pkgName := pkgProg.Package
		for _, decl := range pkgProg.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				// C言語 extern 宣言（Body == nil）はそのまま、HIKE 関数は pkg_Func 名に変更
				if fn.Body != nil {
					fn.Name.Value = pkgName + "_" + fn.Name.Value
				}
			}
			mergedProg.Decls = append(mergedProg.Decls, decl)
		}
	}

	// 2. エントリファイル自体の宣言を追加
	mergedProg.Decls = append(mergedProg.Decls, entryProg.Decls...)

	// 3. 型検査とコード生成
	semaCtx, err := sema.Analyze(mergedProg)
	if err != nil {
		return "", fmt.Errorf("semantic error: %w", err)
	}

	cg := codegen.New(mergedProg, semaCtx, nil, entryPath, false)
	return cg.Generate(), nil
}
