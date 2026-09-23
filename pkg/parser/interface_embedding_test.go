package parser

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/sema"
)

func TestInterfaceEmbedding(t *testing.T) {
	source := `package sample

type Reader interface {
    Read() int
}
type ReadWriter interface {
    Reader
    Write() int
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	reader, ok := program.Decls[0].(*ast.TypeDecl)
	if !ok {
		t.Fatalf("expected Reader type declaration, got %T", program.Decls[0])
	}
	if len(reader.Type.(*ast.InterfaceType).Embedded) != 0 {
		t.Fatal("Reader must not contain embedded interfaces")
	}
	decl, ok := program.Decls[1].(*ast.TypeDecl)
	if !ok {
		t.Fatalf("expected ReadWriter type declaration, got %T", program.Decls[1])
	}
	iface := decl.Type.(*ast.InterfaceType)
	if len(iface.Embedded) != 1 || iface.Embedded[0].TokenLiteral() != "Reader" {
		t.Fatalf("expected Reader embedding, got %#v", iface.Embedded)
	}

	ctx, err := sema.Analyze(program)
	if err != nil {
		t.Fatalf("embedded interface semantic analysis failed: %v", err)
	}
	resolved := ctx.ResolveType(&ast.NamedType{Name: &ast.Identifier{Value: "ReadWriter"}})
	readWriter, ok := resolved.(*sema.InterfaceType)
	if !ok || !readWriter.HasMethod("Read") || !readWriter.HasMethod("Write") {
		t.Fatalf("embedded methods were not promoted: %v", resolved)
	}
}

func TestGroupedTypedParameters(t *testing.T) {
	source := `package sample
func WriteWasmJSRuntimeMode(destPath, mode string, programs ...*int) error { return nil }
func AnalyzeWithReporterModes(prog *int, reporter *int, filename string, regionEnabled, goHikeEnabled bool) *int { return nil }
func registerBuiltinCapabilities(receiverName, methodName string, params, returns []Type, fn *FuncType, ctx *Context) {}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	fn, ok := program.Decls[0].(*ast.FuncDecl)
	if !ok || len(fn.Params) != 3 {
		t.Fatalf("expected three expanded parameters, got %T with %d params", program.Decls[0], len(fn.Params))
	}
	if fn.Params[0].Name.Value != "destPath" || fn.Params[1].Name.Value != "mode" || fn.Params[2].Name.Value != "programs" {
		t.Fatalf("unexpected parameter names: %s, %s, %s", fn.Params[0].Name.Value, fn.Params[1].Name.Value, fn.Params[2].Name.Value)
	}
	if fn.Params[0].Type.TokenLiteral() != "string" || fn.Params[1].Type.TokenLiteral() != "string" {
		t.Fatalf("grouped parameters did not receive string type")
	}
	fn, ok = program.Decls[1].(*ast.FuncDecl)
	if !ok || len(fn.Params) != 5 {
		t.Fatalf("expected five expanded parameters in trailing group, got %T with %d params", program.Decls[1], len(fn.Params))
	}
	fn, ok = program.Decls[2].(*ast.FuncDecl)
	if !ok || len(fn.Params) != 6 {
		t.Fatalf("expected six parameters in slice-typed groups, got %T with %d params", program.Decls[2], len(fn.Params))
	}
	if _, ok := fn.Params[2].Type.(*ast.SliceType); !ok {
		t.Fatalf("params did not receive a slice type: %T", fn.Params[2].Type)
	}
	if _, ok := fn.Params[3].Type.(*ast.SliceType); !ok {
		t.Fatalf("slice-typed grouped parameters were not expanded correctly")
	}
}

func TestGoHikeControlSyntax(t *testing.T) {
	source := `package sample
	type ControlNode int
	type ControlElement interface { SetControlPosition(index, depth int); SetControlLinks(next, breakTarget, continueTarget int) }
	type ControlBody []ControlNode
func check(nodes []string, visible map[string]bool) {
    for _, target := range append(append([]string{}, nodes...), "default") {
        if !visible[target] { return }
    }
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	control, ok := program.Decls[1].(*ast.TypeDecl)
	if !ok || len(control.Type.(*ast.InterfaceType).Methods) != 2 {
		t.Fatalf("expected grouped interface methods to parse")
	}
	links := control.Type.(*ast.InterfaceType).Methods[1]
	if len(links.ParamTypes) != 3 {
		t.Fatalf("expected three grouped parameters, got %d", len(links.ParamTypes))
	}
}

func TestTypeSwitchMultiplePointerCases(t *testing.T) {
	source := `package sample
func inspect(v any) int {
	 switch v.(type) {
    case *ast.IntegerLiteral, *ast.CharLiteral:
        return 1
    case *ast.FloatLiteral:
        return 2
    default:
        return 0
    }
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	fn := program.Decls[0].(*ast.FuncDecl)
	switchStmt, ok := fn.Body.Statements[0].(*ast.TypeSwitchStmt)
	if !ok {
		t.Fatalf("expected type switch, got %T", fn.Body.Statements[0])
	}
	if len(switchStmt.Cases) != 3 || len(switchStmt.Cases[0].Types) != 2 {
		t.Fatalf("expected two types in first case, got %#v", switchStmt.Cases)
	}
}

func TestGoStyleSliceLiteral(t *testing.T) {
	source := `package sample
type Method struct { Name string; ParamTypes []int; ReturnTypes []int }
type InterfaceType struct { Name string; Methods []Method }
func makeInterface() *InterfaceType {
    iface := &InterfaceType{
        Name: "error",
        Methods: []Method{
            {Name: "Error", ParamTypes: []int{}, ReturnTypes: []int{1}},
        },
    }
    return iface
}
`
	p := New(lexer.New(source))
	p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
}

func TestGoStyleForRangeAndCompoundCondition(t *testing.T) {
	source := `package sample
func scan(items []int, seen map[int]bool) int {
    total := 0
    for _, value := range items {
        if !seen[value] && value > 0 {
            total = total + value
        }
    }
    return total
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	fn := program.Decls[0].(*ast.FuncDecl)
	if _, ok := fn.Body.Statements[1].(*ast.ForRangeStmt); !ok {
		t.Fatalf("expected for-range statement, got %T", fn.Body.Statements[1])
	}
}

func TestLoaderDependencyLoopSyntax(t *testing.T) {
	source := `package loader
func load(imports []string, visited map[string]bool, err error, pkgDir string) {
    for _, imp := range imports {
        if err == nil && pkgDir != "" && !visited[pkgDir] {
            visited[imp] = true
        }
    }
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	if len(program.Decls) != 1 {
		t.Fatalf("expected one function declaration, got %d", len(program.Decls))
	}
}

func TestLoaderLikeFunctionBodySyntax(t *testing.T) {
	source := `package loader
func load(entryPaths []string, visited map[string]bool, module any) error {
    fileQueue := []string{}
    for _, path := range entryPaths {
        absPath, err := resolve(path)
        if err != nil {
            return err
        }
        isDir, err := stat(absPath)
        if err != nil {
            return err
        }
        if isDir {
            files, err := list(absPath)
            if err != nil {
                return err
            }
            fileQueue = append(fileQueue, files...)
        } else {
            fileQueue = append(fileQueue, absPath)
        }
    }
    for len(fileQueue) > 0 {
        current := fileQueue[0]
        fileQueue = fileQueue[1:]
        if visited[current] {
            continue
        }
        visited[current] = true
        content, err := read(current)
        if err != nil {
            return err
        }
        for _, imp := range imports(content) {
            if module == nil {
                continue
            }
            pkgDir, err := resolvePackage(module, current, imp)
            if err == nil && pkgDir != "" && !visited[pkgDir] {
                visited[pkgDir] = true
                files, err := list(pkgDir)
                if err != nil {
                    return err
                }
                fileQueue = append(fileQueue, files...)
            }
        }
    }
    return nil
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	if len(program.Decls) != 1 {
		t.Fatalf("expected one function declaration, got %d", len(program.Decls))
	}
}
