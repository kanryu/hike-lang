package parser

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/token"
)

func TestIfStatementWithInit(t *testing.T) {
	source := `package helpers

func Add(delta int) {
	if counter := AddInt(&value, delta); counter == 0 {
		value = 1
	} else if counter < 0 {
		value = -1
	}
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("if initializer was not parsed: %v", p.Errors())
	}
	if len(program.Decls) != 1 {
		t.Fatalf("declaration count = %d, want 1", len(program.Decls))
	}
}

func TestArrayTypeAndForInitializer(t *testing.T) {
	source := `package p

type Counters [4]uint32

func scan(text string) {
	for i, n := 0, len(text); i < n; i++ {
		_ = i
	}
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("array/for source was not parsed: %v", p.Errors())
	}
	typeDecl := program.Decls[0].(*ast.TypeDecl)
	if _, ok := typeDecl.Type.(*ast.ArrayType); !ok {
		t.Fatalf("array declaration type = %T, want *ast.ArrayType", typeDecl.Type)
	}
	fn := program.Decls[1].(*ast.FuncDecl)
	loop := fn.Body.Statements[0].(*ast.ForStmt)
	init, ok := loop.Init.(*ast.AssignStmt)
	if !ok || init.Token.Type != token.DEFINE {
		t.Fatalf("for initializer = %#v, want := assignment", loop.Init)
	}
}
