package lower

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
)

func TestNumericSwitchCasesBuildDenseTableMetadata(t *testing.T) {
	switchStmt := &ast.SwitchStmt{Cases: []*ast.CaseClause{
		{Values: []ast.Expression{&ast.IntegerLiteral{Value: 2}}},
		{Values: []ast.Expression{&ast.IntegerLiteral{Value: 4}, &ast.IntegerLiteral{Value: 5}}},
		{Values: nil},
	}}
	cases, min, max, ok := numericSwitchCases(switchStmt)
	if !ok || min != 2 || max != 5 || len(cases) != 2 {
		t.Fatalf("numeric switch metadata = (%v, %d, %d, %v)", cases, min, max, ok)
	}
	if _, _, _, ok := numericSwitchCases(&ast.SwitchStmt{Cases: []*ast.CaseClause{{Values: []ast.Expression{&ast.StringLiteral{}}}}}); ok {
		t.Fatal("non-numeric switch case was accepted as a table")
	}
}

func TestFunctionControlLayoutKeepsExitAtRootTail(t *testing.T) {
	l := &Lowerer{}
	fn := &hir.Function{Name: "f"}
	l.curFunc = fn
	l.structuredStack = []*hir.ControlBody{}
	l.initFunctionControl(fn)

	if len(fn.ControlNodes) != 2 || fn.ControlNodes[0] != fn.ControlRoot || fn.ControlNodes[1] != fn.ControlExit {
		t.Fatalf("initial control table = %#v, want root followed by exit", fn.ControlNodes)
	}
	if len(fn.ControlRoot.Body) != 1 || fn.ControlRoot.Body[0] != fn.ControlExit {
		t.Fatalf("initial root body = %#v, want only function exit", fn.ControlRoot.Body)
	}

	block := &hir.BlockNode{Label: "if"}
	l.appendStructuredNode(block)
	if len(fn.ControlRoot.Body) != 2 || fn.ControlRoot.Body[0] != block || fn.ControlRoot.Body[1] != fn.ControlExit {
		t.Fatalf("after control insertion = %#v, want block then exit", fn.ControlRoot.Body)
	}

	next := l.appendContinuationAfter(&fn.ControlRoot.Body, block, 0)
	if next == nil || len(fn.ControlRoot.Body) != 3 || fn.ControlRoot.Body[1] != next || fn.ControlRoot.Body[2] != fn.ControlExit {
		t.Fatalf("after continuation insertion = %#v, want block, continuation, exit", fn.ControlRoot.Body)
	}
	if block.Next() != next.Index() {
		t.Fatalf("block.Next() = %d, want continuation ID %d", block.Next(), next.Index())
	}

	replacement := &hir.IfNode{Label: "next-if"}
	l.appendStructuredNode(replacement)
	if len(fn.ControlRoot.Body) != 3 || fn.ControlRoot.Body[1] != replacement || fn.ControlRoot.Body[2] != fn.ControlExit {
		t.Fatalf("after continuation replacement = %#v, want block, if, exit", fn.ControlRoot.Body)
	}
	if next.ReplacedByID != replacement.Index() {
		t.Fatalf("placeholder replacement ID = %d, want %d", next.ReplacedByID, replacement.Index())
	}
}
