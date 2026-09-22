package hir

import "testing"

func TestLowerStructuredBodyResolvesControlIDs(t *testing.T) {
	block := &BlockNode{Label: "exit"}
	loop := &LoopNode{Label: "continue"}
	branch := &BrNode{TargetID: 1, Target: loop.Label}
	loop.Body = ControlBody{branch}
	block.Body = ControlBody{loop}
	fn := &Function{
		ControlNodes:   []ControlElement{block, loop, branch},
		StructuredBody: ControlBody{block},
	}
	block.ID, block.ControlDepth = 0, 1
	loop.ID, loop.ControlDepth = 1, 2
	branch.ID, branch.ControlDepth = 2, 3

	blocks, err := LowerStructuredBody(fn)
	if err != nil {
		t.Fatalf("LowerStructuredBody failed: %v", err)
	}
	if len(blocks) < 3 {
		t.Fatalf("got %d CFG blocks, want loop header and exit blocks", len(blocks))
	}
	if blocks[2].Terminator == nil {
		t.Fatal("loop body has no terminator")
	}
	jump, ok := blocks[2].Terminator.(*InstrJump)
	if !ok {
		t.Fatalf("loop body terminator = %T, want jump", blocks[2].Terminator)
	}
	if jump.Target == "" {
		t.Fatal("resolved control ID produced an empty target")
	}
}
