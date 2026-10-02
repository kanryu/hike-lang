package parser

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/token"
)

func TestIotaIsLexedAsDedicatedToken(t *testing.T) {
	l := lexer.New("iota")
	if got := l.NextToken(); got.Type != token.IOTA {
		t.Fatalf("iota token type = %q, want %q", got.Type, token.IOTA)
	}
}

func TestConstGroupExpandsIotaAndImplicitExpressions(t *testing.T) {
	source := `package main

const (
	_ int = iota
	LOWEST
	LOR
	LAND
	EQUALS
)
`

	p := New(lexer.New(source))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("const group unexpectedly failed to parse: %v", errs)
	}

	want := []int64{0, 1, 2, 3, 4}
	if len(program.Decls) != len(want) {
		t.Fatalf("declaration count = %d, want %d", len(program.Decls), len(want))
	}
	for i, decl := range program.Decls {
		cd, ok := decl.(*ast.ConstDecl)
		if !ok {
			t.Fatalf("declaration %d has type %T, want *ast.ConstDecl", i, decl)
		}
		value, ok := evalTestConstInt(cd.Value)
		if !ok {
			t.Fatalf("constant %s has non-integer value expression %T", cd.Name.Value, cd.Value)
		}
		if value != want[i] {
			t.Errorf("constant %s = %d, want %d", cd.Name.Value, value, want[i])
		}
	}
}

func TestConstExpressionRejectsRepeatedIotaReferences(t *testing.T) {
	source := `package main

const (
	First = iota
	Second = iota
	Double = iota + iota
	Inherited
)
`

	p := New(lexer.New(source))
	_ = p.ParseProgram()
	if errs := p.Errors(); len(errs) == 0 {
		t.Fatal("expected repeated iota references to be rejected")
	}
}

func TestConstGroupSupportsIotaOffsetWithMatchingNames(t *testing.T) {
	source := `package main

const (
	Zero = iota
	One
	Eleven = iota + 9
	Twelve
)
`

	p := New(lexer.New(source))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("iota offset group unexpectedly failed to parse: %v", errs)
	}

	want := []int64{0, 1, 11, 12}
	for i, decl := range program.Decls {
		cd := decl.(*ast.ConstDecl)
		value, ok := evalTestConstInt(cd.Value)
		if !ok || value != want[i] {
			t.Errorf("constant %s = %d, want %d", cd.Name.Value, value, want[i])
		}
	}
}

func evalTestConstInt(expr ast.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return e.Value, true
	case *ast.BinaryExpr:
		left, lok := evalTestConstInt(e.Left)
		right, rok := evalTestConstInt(e.Right)
		if !lok || !rok {
			return 0, false
		}
		switch e.Operator {
		case "+":
			return left + right, true
		case "-":
			return left - right, true
		}
	}
	return 0, false
}
