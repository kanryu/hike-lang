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
	decl, ok := program.Decls[0].(*ast.TypeDecl)
	if !ok {
		t.Fatalf("expected Reader type declaration, got %T", program.Decls[1])
	}
	if len(decl.Type.(*ast.InterfaceType).Embedded) != 0 {
		t.Fatal("Reader must not contain embedded interfaces")
	}
	decl, ok = program.Decls[1].(*ast.TypeDecl)
	if !ok {
		t.Fatalf("expected ReadWriter type declaration, got %T", program.Decls[2])
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
