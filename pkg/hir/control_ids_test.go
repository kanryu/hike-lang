package hir

import "testing"

func TestFunctionControlNodeAtUsesZeroBasedIndex(t *testing.T) {
	block := &BlockNode{ID: 0, Label: "block.0"}
	loop := &LoopNode{ID: 1, Label: "loop.1"}
	fn := &Function{ControlNodes: []ControlElement{block, loop}}

	got, ok := fn.ControlNodeAt(1)
	if !ok || got != loop {
		t.Fatalf("ControlNodeAt(1) = (%v, %v), want loop node", got, ok)
	}
	if _, ok := fn.ControlNodeAt(2); ok {
		t.Fatal("out-of-range control ID was accepted")
	}
}

func TestFunctionBranchDepthUsesAbsoluteDepthMetadata(t *testing.T) {
	block := &BlockNode{ID: 0, ControlDepth: 1, Label: "block"}
	branch := &BrNode{ID: 1, ControlDepth: 2, TargetID: 0, Target: block.Label}
	fn := &Function{ControlNodes: []ControlElement{block, branch}}

	depth, ok := fn.BranchDepth(branch, branch.TargetID)
	if !ok || depth != 0 {
		t.Fatalf("BranchDepth = (%d, %v), want (0, true)", depth, ok)
	}
}

func TestFunctionBranchDepthLeavesScopeValidationToStructuredResolver(t *testing.T) {
	outer := &BlockNode{ID: 0, ControlDepth: 1, Label: "outer"}
	sibling := &BlockNode{ID: 1, ControlDepth: 1, Label: "sibling"}
	branch := &BrNode{ID: 2, ControlDepth: 2, TargetID: 1, Target: sibling.Label}
	fn := &Function{ControlNodes: []ControlElement{outer, sibling, branch}}

	if depth, ok := fn.BranchDepth(branch, branch.TargetID); !ok || depth != 0 {
		t.Fatalf("BranchDepth = (%d, %v), want (0, true); lexical scope is checked separately", depth, ok)
	}
}

func TestLoopControlLinksExposeBreakAndContinueTargets(t *testing.T) {
	exit := &BlockNode{ID: 0, ControlDepth: 1, Label: "loop.exit"}
	loop := &LoopNode{ID: 1, ControlDepth: 2, Label: "loop.head"}
	loop.SetControlLinks(exit.Index(), exit.Index(), loop.Index())

	if loop.Next() != exit.Index() || loop.BreakTarget() != exit.Index() || loop.ContinueTarget() != loop.Index() {
		t.Fatal("loop control links were not preserved")
	}
}
