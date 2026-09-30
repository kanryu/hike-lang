package sema

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

func TestAnalyzeModeInfersUntypedGlobalMap(t *testing.T) {
	program := parser.New(lexer.New(`
package main

var values = map[string]int{"answer": 42}
var texts = map[string]string{"answer": "forty-two"}

func main() int {
	return values["answer"]
}
`)).ParseProgram()

	ctx, err := AnalyzeMode(program, true)
	if err != nil {
		t.Fatalf("AnalyzeMode() error = %v", err)
	}

	mapType, ok := ctx.Globals["values"].(*MapType)
	if !ok {
		t.Fatalf("values type = %T, want *MapType", ctx.Globals["values"])
	}
	if mapType.Key != TypeString || mapType.Value != TypeInt {
		t.Fatalf("values type = map[%s]%s, want map[string]int", mapType.Key.TypeName(), mapType.Value.TypeName())
	}
	if !mapType.IsSingleValue {
		t.Fatal("numeric map should use the single-value map ABI")
	}
	textMap, ok := ctx.Globals["texts"].(*MapType)
	if !ok || textMap.IsSingleValue {
		t.Fatal("string map should use the boxed map ABI")
	}
	if got := ctx.InferExprType(program.Decls[2].(*ast.FuncDecl).Body.Statements[0].(*ast.ReturnStmt).Values[0], nil); got != TypeInt {
		t.Fatalf("map index type = %s, want int", got.TypeName())
	}
}
