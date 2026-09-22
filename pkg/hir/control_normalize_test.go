package hir

import "testing"

func TestFlattenTransparentBlocksPreservesControlIDs(t *testing.T) {
	root := &BlockNode{ID: 0, ControlDepth: 1, Label: "root"}
	exit := &BlockNode{ID: 1, ControlDepth: 2, Label: "exit", FunctionExit: true}
	ifNode := &IfNode{ID: 2, ControlDepth: 2, Label: "if", Cond: &ConstBool{Val: true}}
	normal := &BlockNode{
		ID:                      3,
		ControlDepth:            3,
		Label:                   "normal",
		ContinuationPlaceholder: true,
		Body:                    ControlBody{&InstructionNode{Instruction: &InstrUnreachable{}}},
	}
	ifNode.Then = ControlBody{normal}
	root.Body = ControlBody{ifNode, exit}
	fn := &Function{
		ControlRoot:    root,
		ControlExit:    exit,
		ControlNodes:   []ControlElement{root, exit, ifNode, normal},
		StructuredBody: ControlBody{root},
	}

	FlattenTransparentBlocks(fn)

	if len(ifNode.Then) != 1 {
		t.Fatalf("normalized then body length = %d, want 1", len(ifNode.Then))
	}
	if _, ok := ifNode.Then[0].(*InstructionNode); !ok {
		t.Fatalf("normalized then body = %T, want instruction", ifNode.Then[0])
	}
	if got, ok := fn.ControlNodeAt(normal.Index()); !ok || got != normal {
		t.Fatal("normal block ID was removed or remapped")
	}
}
