package llvm

import (
	"strings"
	"testing"

	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

func TestLowerStructuredLoopRestoresTransientStack(t *testing.T) {
	loop := &hir.LoopNode{ID: 1, Label: "loop"}
	exit := &hir.BlockNode{ID: 2, Label: "exit"}
	loop.Exit = exit
	loop.Body = hir.ControlBody{
		&hir.InstructionNode{Instruction: &hir.InstrAllocaDynamic{
			Dst:       &hir.Reg{ID: 3, Typ: &sema.PointerType{Base: sema.TypeByte}},
			Size:      &hir.ConstInt{Val: 32, Typ: sema.TypeInt},
			AllocType: sema.TypeByte,
		}},
		&hir.InstructionNode{Instruction: &hir.InstrAlloca{
			Dst:       &hir.Reg{ID: 4, Typ: &sema.PointerType{Base: sema.TypeInt}},
			AllocType: sema.TypeInt,
		}},
	}
	exit.Body = hir.ControlBody{loop}
	fn := &hir.Function{Name: "main", StructuredBody: hir.ControlBody{exit}}

	count := func() (int, int) {
		blocks, err := lowerStructuredBody(fn)
		if err != nil {
			t.Fatalf("lowerStructuredBody failed: %v", err)
		}
		var saves, restores int
		for _, block := range blocks {
			for _, inst := range block.Instructions {
				switch inst.(type) {
				case *hir.InstrStackSave:
					saves++
				case *hir.InstrStackRestore:
					restores++
				}
			}
		}
		return saves, restores
	}

	for run := 0; run < 2; run++ {
		saves, restores := count()
		if saves != 1 {
			t.Fatalf("run %d: expected one loop stack save, got %d", run, saves)
		}
		if restores != 2 {
			t.Fatalf("run %d: expected iteration and exit stack restores, got %d", run, restores)
		}
	}
}

func TestEmitStackSaveRestoreUsesLLVMIntrinsics(t *testing.T) {
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	token := &hir.Reg{ID: 1, Typ: &sema.PointerType{Base: sema.TypeByte}}
	emitter.emitInstruction(&hir.InstrStackSave{Dst: token})
	emitter.emitInstruction(&hir.InstrStackRestore{Token: token})
	ir := emitter.b.String()
	if !strings.Contains(ir, "call i8* @llvm.stacksave()") {
		t.Fatalf("missing llvm.stacksave emission: %s", ir)
	}
	if !strings.Contains(ir, "call void @llvm.stackrestore(i8* %v1)") {
		t.Fatalf("missing llvm.stackrestore emission: %s", ir)
	}
}
