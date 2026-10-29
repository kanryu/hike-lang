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

func TestSwitchTableSizeUsesSmallestFortyPercentCandidate(t *testing.T) {
	for _, tc := range []struct {
		entries int
		want    int
	}{
		{entries: 1, want: 17},
		{entries: 6, want: 17},
		{entries: 7, want: 31},
		{entries: 12, want: 31},
		{entries: 13, want: 61},
		{entries: 24, want: 61},
		{entries: 25, want: 127},
		{entries: 50, want: 127},
		{entries: 51, want: 251},
		{entries: 101, want: 251},
	} {
		if got := switchTableSize(tc.entries); got != tc.want {
			t.Fatalf("switchTableSize(%d) = %d, want %d", tc.entries, got, tc.want)
		}
	}
}

func TestSwitchBucketUsesSelectedTableSize(t *testing.T) {
	if got, ok := switchBucketForExpr(&ast.IntegerLiteral{Value: -1}, nil, false, false, 17); !ok || got != 16 {
		t.Fatalf("negative integer bucket = (%d, %v), want (16, true)", got, ok)
	}
	value := "compiler"
	want := stringSwitchHash(value, false) % 31
	if got := stringSwitchBucket(value, false, 31); got != want {
		t.Fatalf("string bucket = %d, want %d", got, want)
	}
}

func TestFuncLitContainsRecoverWithoutReflection(t *testing.T) {
	fn := &ast.FuncLit{Body: &ast.BlockStmt{Statements: []ast.Statement{
		&ast.IfStmt{Consequence: &ast.BlockStmt{Statements: []ast.Statement{
			&ast.ExprStmt{Expr: &ast.CallExpr{Function: &ast.Identifier{Value: "recover_cause"}}},
		}}},
	}}}
	if !funcLitContainsRecover(fn) {
		t.Fatal("nested recover call was not detected")
	}
	if funcLitContainsRecover(&ast.FuncLit{Body: &ast.BlockStmt{Statements: []ast.Statement{
		&ast.ExprStmt{Expr: &ast.CallExpr{Function: &ast.Identifier{Value: "other"}}},
	}}}) {
		t.Fatal("unrelated call was reported as recover")
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

func TestTailCallMarkingRequiresCleanupFreeReturn(t *testing.T) {
	dst := &hir.Reg{ID: 1}
	call := &hir.InstrCallStatic{Dst: dst, CalleeName: "recurse"}
	l := &Lowerer{curFunc: &hir.Function{Name: "recurse"}, curBlock: &basicBlock{Instructions: []hir.Instruction{call}}}
	l.markLastStaticCallTail(dst)
	if !call.IsTail {
		t.Fatal("cleanup-free direct return call was not marked as tail")
	}

	cases := []struct {
		name  string
		setup func(*Lowerer)
	}{
		{name: "defer", setup: func(l *Lowerer) {
			l.deferStack = []*ast.CallExpr{{}}
		}},
		{name: "managed parameter", setup: func(l *Lowerer) {
			l.managedParams = []hir.Value{&hir.Reg{ID: 2}}
		}},
		{name: "managed local", setup: func(l *Lowerer) {
			l.managedScopes = []managedScope{{slots: []*hir.Reg{{ID: 3}}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dst := &hir.Reg{ID: 1}
			call := &hir.InstrCallStatic{Dst: dst, CalleeName: "recurse"}
			l := &Lowerer{curFunc: &hir.Function{Name: "recurse"}, curBlock: &basicBlock{Instructions: []hir.Instruction{call}}}
			tc.setup(l)
			l.markLastStaticCallTail(dst)
			if call.IsTail {
				t.Fatal("call with pending cleanup was marked as tail")
			}
		})
	}
}
