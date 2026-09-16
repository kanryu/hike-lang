package transform

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

func TestParseSimpleTypeExprPreservesFixedArray(t *testing.T) {
	typ := parseSimpleTypeExpr(token.Token{}, "[32]byte")
	array, ok := typ.(*ast.ArrayType)
	if !ok {
		t.Fatalf("[32]byte was restored as %T, want *ast.ArrayType", typ)
	}
	if array.Len != 32 {
		t.Fatalf("array length = %d, want 32", array.Len)
	}
	elem, ok := array.Elem.(*ast.NamedType)
	if !ok || elem.Name.Value != "byte" {
		t.Fatalf("array element = %#v, want byte", array.Elem)
	}
}

func TestInferredArrayLiteralGetsInitializerLength(t *testing.T) {
	program := parser.New(lexer.New(`
package main

func main() int {
	lut := [...]byte{1, 2, 4}
    return int(lut[2])
}
`)).ParseProgram()
	ctx, err := sema.Analyze(program)
	if err != nil {
		t.Fatalf("sema.Analyze() failed: %v", err)
	}
	var mainFn *ast.FuncDecl
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Value == "main" {
			mainFn = fn
		}
	}
	if mainFn == nil || mainFn.Body == nil || len(mainFn.Body.Statements) == 0 {
		t.Fatal("main function body was not parsed")
	}
	assignment, ok := mainFn.Body.Statements[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("first statement = %T, want *ast.AssignStmt", mainFn.Body.Statements[0])
	}
	array, ok := ctx.InferExprType(assignment.Right[0], nil).(*sema.ArrayType)
	if !ok || array.Len != 3 || array.TypeName() != "[3]byte" {
		t.Fatalf("inferred array type = %T %v, want [3]byte", ctx.InferExprType(assignment.Right[0], nil), array)
	}
}

func TestExtractStructAndTypeArgsPreservesConstArgument(t *testing.T) {
	typ := &ast.NamedType{
		Token: token.Token{},
		Name:  &ast.Identifier{Value: "matrix_Matrix__int_const_8_const_8"},
	}
	name, args, isPointer := extractStructAndTypeArgs(typ)
	if name != "matrix_Matrix" || isPointer || len(args) != 3 {
		t.Fatalf("specialized type = %q, %#v, ptr=%t", name, args, isPointer)
	}
	if got, ok := args[0].(*ast.NamedType); !ok || got.Name.Value != "int" {
		t.Fatalf("type argument = %#v, want int", args[0])
	}
	for i, arg := range args[1:] {
		constArg, ok := arg.(*ast.ConstArg)
		if !ok {
			t.Fatalf("argument %d = %T, want *ast.ConstArg", i+1, arg)
		}
		literal, ok := constArg.Expr.(*ast.IntegerLiteral)
		if !ok || literal.Value != 8 {
			t.Fatalf("const argument %d = %#v, want 8", i+1, constArg.Expr)
		}
	}
}

func TestTransformGenericArrayReturn(t *testing.T) {
	program := parser.New(lexer.New(`
package main

func Identity[T](value T) T {
    return value
}

func main() int {
    var input [32]byte
    output := Identity[[32]byte](input)
    return int(output[0])
}
`)).ParseProgram()
	ctx, err := sema.Analyze(program)
	if err != nil {
		t.Fatalf("sema.Analyze() failed: %v", err)
	}
	if _, err := New(program, ctx).Transform(); err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}

	fn, ok := ctx.Functions["Identity_[32]byte"]
	if !ok {
		t.Fatalf("specialized Identity function was not materialized: %#v", ctx.Functions)
	}
	if len(fn.ReturnTypes) != 1 || fn.ReturnTypes[0].TypeName() != "[32]byte" {
		t.Fatalf("specialized return type = %#v, want [32]byte", fn.ReturnTypes)
	}
}
