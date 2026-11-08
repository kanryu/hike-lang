package lower

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

// TestCopyBuiltinKeepsSliceSideEffects reproduces the self-hosting failure
// where the compiler's own copy(dst, src) calls are lowered to a constant zero.
// Those calls move structured-HIR elements while lowering control flow; losing
// the side effect leaves the function-exit sentinel in the middle of the body.
func TestCopyBuiltinKeepsSliceSideEffects(t *testing.T) {
	root := New(&ast.Program{Package: "repro"}, &sema.Context{})
	root.curBlock = &basicBlock{}
	sliceType := &sema.SliceType{Elem: sema.TypeInt}
	root.symbols["dst"] = &hir.Reg{ID: 1, Typ: &sema.PointerType{Base: sliceType}}
	root.symbols["src"] = &hir.Reg{ID: 2, Typ: &sema.PointerType{Base: sliceType}}
	call := &ast.CallExpr{
		Function: &ast.Identifier{Value: "copy"},
		Args: []ast.Expression{
			&ast.Identifier{Value: "dst"},
			&ast.Identifier{Value: "src"},
		},
	}

	value := root.Call.lowerCallRemainder(call)
	if _, ok := value.(*hir.ConstInt); ok {
		t.Fatal("copy was lowered to a constant; its slice move side effect was discarded")
	}
	var hasMemmove bool
	for _, instruction := range root.curBlock.Instructions {
		if call, ok := instruction.(*hir.InstrCallStatic); ok && call.CalleeName == "__hike_memmove" {
			hasMemmove = true
		}
	}
	if !hasMemmove {
		t.Fatal("copy did not emit the overlap-safe memmove operation")
	}
}
