package parser

import (
	"os"
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
)

func TestLoaderSourceParsesAsWholeFile(t *testing.T) {
	content, err := os.ReadFile("../../pkg/loader/loader.go")
	if err != nil {
		t.Fatalf("read loader.go: %v", err)
	}
	p := New(lexer.New(string(content)))
	p.SetVerbose(false)
	p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("loader.go must parse as a whole file: %v", p.Errors())
	}
}

func TestRangeOverInlineStringSliceLiteral(t *testing.T) {
	source := `package loader

func apply(content string) int {
    for _, prefix := range []string{"//go:replace ", "//hike:go-replace "} {
        if prefix == content { return 1 }
    }
    return 0
}

`
	p := New(lexer.New(source))
	p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("range over inline slice literal must parse: %v", p.Errors())
	}
}

// TestLoaderBodySyntaxReproduction keeps the loader's failing control-flow
// shape isolated from semantic and lowering tests. It is intentionally close
// to loader.go: package discovery uses range loops, short declarations,
// multiple assignment, and a negated map lookup in a compound condition.
func TestLoaderBodySyntaxReproduction(t *testing.T) {
	source := `package loader

type Loader struct {
    visitedFiles map[string]bool
    visitedPkgs map[string]bool
}

func (l *Loader) Load(entryPaths []string) error {
    fileQueue := []string{}
    for _, path := range entryPaths {
        absPath, err := filepath.Abs(path)
        if err != nil {
            return err
        }
        isDir, err := isDirectory(absPath)
        if err != nil {
            return err
        }
        if isDir {
            files, err := l.findHikeFilesInDir(absPath)
            if err != nil {
                return err
            }
            fileQueue = append(fileQueue, files...)
        } else {
            fileQueue = append(fileQueue, absPath)
        }
    }

    for len(fileQueue) > 0 {
        curFile := fileQueue[0]
        fileQueue = fileQueue[1:]
        if l.visitedFiles[curFile] {
            continue
        }
        l.visitedFiles[curFile] = true
        content, err := readFile(curFile)
        if err != nil {
            return err
        }
        for _, imp := range imports(content) {
            if module == nil {
                continue
            }
            pkgDir, err := resolvePackage(module, curFile, imp)
            if err == nil && pkgDir != "" && !l.visitedPkgs[pkgDir] {
                l.visitedPkgs[pkgDir] = true
                files, err := l.findHikeFilesInDir(pkgDir)
                if err != nil {
                    return err
                }
                fileQueue = append(fileQueue, files...)
            }
        }
    }
    return nil
}

func (l *Loader) findHikeFilesInDir(dir string) ([]string, error) {
    entries, err := readDir(dir)
    if err != nil {
        return nil, err
    }
    files := []string{}
    for _, entry := range entries {
        path := join(dir, entry)
        isSource := hasSuffix(entry, ".hike")
        if !entryIsDir(entry) && isSource && l.visitedFiles[path] {
            files = append(files, path)
        }
    }
    return files, nil
}

`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("loader reproduction must parse: %v", p.Errors())
	}
	if len(program.Decls) != 3 {
		t.Fatalf("expected Loader plus two methods, got %d declarations", len(program.Decls))
	}
	if _, ok := program.Decls[1].(*ast.FuncDecl); !ok {
		t.Fatalf("expected Load function, got %T", program.Decls[1])
	}
}

// TestLoaderTypeSwitchReproduction mirrors the exact declaration shape that
// failed when loader.go was parsed in GoHike mode.
func TestLoaderTypeSwitchReproduction(t *testing.T) {
	source := `package loader

import "hikec-go/pkg/ast"

func manglePackageDecls(pkgName string, decls []ast.Decl) []ast.Decl {
    var mangled []ast.Decl
    for _, decl := range decls {
        switch d := decl.(type) {
        case *ast.FuncDecl:
            if d.Body != nil && (pkgName != "main" || d.Name.Value != "main") {
                d.Name.Value = pkgName + "_" + d.Name.Value
            }
            mangled = append(mangled, d)
        case *ast.CFuncDecl:
            mangled = append(mangled, d)
        case *ast.TypeDecl:
            mangled = append(mangled, d)
        default:
            mangled = append(mangled, d)
        }
    }
    return mangled
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("loader type-switch reproduction must parse: %v", p.Errors())
	}
	if len(program.Decls) != 1 {
		t.Fatalf("expected one function declaration, got %d", len(program.Decls))
	}
}

func TestMethodErrorResultInIfInitReproduction(t *testing.T) {
	source := `package exec

type Cmd struct{}
func (c *Cmd) Run() error { return nil }
func check(cmd *Cmd) int {
    if err := cmd.Run(); err != nil { return 1 }
    return 0
}
`
	p := New(lexer.New(source))
	p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("method error-result if-init must parse: %v", p.Errors())
	}
}
