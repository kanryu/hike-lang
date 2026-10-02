package parser

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
)

func TestGoHikeGoStatementBecomesAsync(t *testing.T) {
	source := `package sample
func worker(value int) {}
func main() {
	go worker(42)
}
`

	p := New(lexer.New(source))
	p.SetGoHikeMode(true)
	p.SetCompileFork(true)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}

	mainFn := program.Decls[1].(*ast.FuncDecl)
	if len(mainFn.Body.Statements) != 1 {
		t.Fatalf("expected one statement, got %d", len(mainFn.Body.Statements))
	}
	stmt, ok := mainFn.Body.Statements[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("expected expression statement, got %T", mainFn.Body.Statements[0])
	}
	async, ok := stmt.Expr.(*ast.AsyncExpr)
	if !ok {
		t.Fatalf("expected AsyncExpr, got %T", stmt.Expr)
	}
	fn, ok := async.Fn.(*ast.FuncLit)
	if !ok || len(fn.Params) != 0 || len(fn.Body.Statements) != 1 {
		t.Fatalf("expected zero-argument closure around go call, got %#v", async.Fn)
	}
	if _, ok := fn.Body.Statements[0].(*ast.ExprStmt); !ok {
		t.Fatalf("expected closure body call, got %T", fn.Body.Statements[0])
	}
}
