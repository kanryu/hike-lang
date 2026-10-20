package wabt

import (
	"strings"
	"testing"

	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

func TestWabtStructuredReturnDoesNotUsePCDispatcher(t *testing.T) {
	fn := &hir.Function{
		Name:        "structuredReturn",
		ReturnTypes: []sema.Type{sema.TypeInt},
		StructuredBody: hir.ControlBody{
			&hir.ReturnNode{Values: []hir.Value{&hir.ConstInt{Val: 7, Typ: sema.TypeInt}}},
		},
	}

	emitter := New(&hir.Program{Functions: []*hir.Function{fn}}, nil)
	emitter.function(fn)
	wat := emitter.b.String()
	if strings.Contains(wat, "(local $pc i32)") || strings.Contains(wat, "$cfg_dispatch") {
		t.Fatalf("structured return unexpectedly used the CFG dispatcher:\n%s", wat)
	}
	if !strings.Contains(wat, "(return (i64.const 7))") {
		t.Fatalf("structured return was not emitted directly:\n%s", wat)
	}
}
