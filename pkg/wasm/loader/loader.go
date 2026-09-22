// Package loader provides a filesystem-free package loader for WebAssembly.
// JavaScript supplies package sources through a single in-memory package map.
package loader

import (
	"encoding/json"
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

type SourceProvider interface {
	Sources(importPath string) ([]string, bool)
}

type JSONSourceProvider struct{ Packages map[string][]string }

func NewJSONSourceProvider(data []byte) (*JSONSourceProvider, error) {
	packages := make(map[string][]string)
	if err := json.Unmarshal(data, &packages); err != nil {
		return nil, fmt.Errorf("invalid wasm package bundle: %w", err)
	}
	return &JSONSourceProvider{Packages: packages}, nil
}

func (p *JSONSourceProvider) Sources(importPath string) ([]string, bool) {
	if p == nil {
		return nil, false
	}
	sources, ok := p.Packages[importPath]
	return sources, ok
}

type Loader struct {
	provider SourceProvider
	verbose  bool
	visited  map[string]bool
}

func New(provider SourceProvider) *Loader {
	return &Loader{provider: provider, visited: make(map[string]bool)}
}

func (l *Loader) SetVerbose(enabled bool) { l.verbose = enabled }

func (l *Loader) Load(entryPackage string) (*ast.Program, error) {
	if l.provider == nil {
		return nil, fmt.Errorf("wasm package loader requires a source provider")
	}
	if entryPackage == "" {
		return nil, fmt.Errorf("wasm package loader requires an entry package")
	}
	program := &ast.Program{Package: "main", Imports: []*ast.ImportDecl{}, Decls: []ast.Decl{}}
	if err := l.loadPackage(entryPackage, program); err != nil {
		return nil, err
	}
	return program, nil
}

func (l *Loader) loadPackage(importPath string, combined *ast.Program) error {
	if l.visited[importPath] {
		return nil
	}
	l.visited[importPath] = true
	sources, ok := l.provider.Sources(importPath)
	if !ok {
		return fmt.Errorf("wasm package not found: %s", importPath)
	}
	var packageName string
	for index, source := range sources {
		p := parser.New(lexer.New(source))
		p.SetVerbose(l.verbose)
		file := p.ParseProgram()
		if len(p.Errors()) != 0 {
			return fmt.Errorf("parse error in wasm package %s source %d: %s", importPath, index, strings.Join(p.Errors(), "\n"))
		}
		if packageName == "" {
			packageName = file.Package
		}
		for _, imp := range file.Imports {
			if err := l.loadPackage(imp.Path, combined); err != nil {
				return err
			}
			combined.Imports = append(combined.Imports, imp)
		}
		if packageName == "main" || importPath == "main" {
			combined.Decls = append(combined.Decls, file.Decls...)
		} else {
			for _, decl := range file.Decls {
				mangleDecl(importPath, decl)
				combined.Decls = append(combined.Decls, decl)
			}
		}
	}
	return nil
}

func mangleDecl(pkg string, decl ast.Decl) {
	pkg = strings.NewReplacer("/", "_", "-", "_").Replace(pkg)
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Body != nil {
			d.Name.Value = pkg + "_" + d.Name.Value
		}
	case *ast.TypeDecl:
		d.Name.Value = pkg + "_" + d.Name.Value
	case *ast.VarDecl:
		d.Name.Value = pkg + "_" + d.Name.Value
	case *ast.MemoryBlockDecl:
		for _, variable := range d.Vars {
			variable.Name.Value = pkg + "_" + variable.Name.Value
		}
	case *ast.ConstDecl:
		d.Name.Value = pkg + "_" + d.Name.Value
	}
}
