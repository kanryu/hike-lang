package parser

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
)

func TestNamedGroupedReturnTypes(t *testing.T) {
	program := New(lexer.New("package main\nfunc decode(b []byte) (payload, relocs []byte, count int, err error) { return nil, nil, 0, nil }\n")).ParseProgram()
	if len(program.Decls) != 1 {
		t.Fatalf("declarations = %d", len(program.Decls))
	}
	fn := program.Decls[0].(*ast.FuncDecl)
	if got := len(fn.ReturnTypes); got != 4 {
		literals := make([]string, len(fn.ReturnTypes))
		for i, typ := range fn.ReturnTypes {
			literals[i] = typ.TokenLiteral()
		}
		t.Fatalf("return types = %d, want 4: %#v", got, literals)
	}
	wantNames := []string{"payload", "relocs", "count", "err"}
	for i, want := range wantNames {
		if fn.ReturnNames[i] != want {
			t.Fatalf("return name[%d] = %q, want %q", i, fn.ReturnNames[i], want)
		}
	}
}
