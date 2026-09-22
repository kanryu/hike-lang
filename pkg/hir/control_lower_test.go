package hir_test

import (
	"testing"

	"hikec-go/pkg/backend/structuredcfg"
	"hikec-go/pkg/hir"
)

func TestLowerStructuredBodyResolvesControlIDs(t *testing.T) {
	block := &hir.BlockNode{Label: "exit"}
	loop := &hir.LoopNode{Label: "continue"}
	branch := &hir.BrNode{TargetID: 1, Target: loop.Label}
	loop.Body = hir.ControlBody{branch}
	block.Body = hir.ControlBody{loop}
	fn := &hir.Function{
		ControlNodes:   []hir.ControlElement{block, loop, branch},
		StructuredBody: hir.ControlBody{block},
	}
	block.ID, block.ControlDepth = 0, 1
	loop.ID, loop.ControlDepth = 1, 2
	branch.ID, branch.ControlDepth = 2, 3

	blocks, err := structuredcfg.LowerStructuredBody(fn)
	if err != nil {
		t.Fatalf("LowerStructuredBody failed: %v", err)
	}
	if len(blocks) < 3 {
		t.Fatalf("got %d CFG blocks, want loop header and exit blocks", len(blocks))
	}
	if blocks[2].Terminator == nil {
		t.Fatal("loop body has no terminator")
	}
	jump, ok := blocks[2].Terminator.(*hir.InstrJump)
	if !ok {
		t.Fatalf("loop body terminator = %T, want jump", blocks[2].Terminator)
	}
	if jump.Target == "" {
		t.Fatal("resolved control ID produced an empty target")
	}
}
