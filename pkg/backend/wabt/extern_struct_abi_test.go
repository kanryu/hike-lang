package wabt

import (
	"strings"
	"testing"

	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

func testExternalStruct() *sema.StructType {
	return &sema.StructType{
		Name: "Image",
		Fields: []sema.Field{
			{Name: "width", Type: sema.TypeInt32},
			{Name: "height", Type: sema.TypeInt32},
			{Name: "data", Type: &sema.PointerType{Base: sema.TypeByte}},
		},
	}
}

func TestWabtExternStructUsesLinearMemoryPointerABI(t *testing.T) {
	image := testExternalStruct()
	program := &hir.Program{Functions: []*hir.Function{
		{Name: "take_image", IsExtern: true, Params: []*hir.Reg{{Typ: image}}},
		{Name: "make_image", IsExtern: true, ReturnTypes: []sema.Type{image}},
	}}

	wat := New(program, nil).Emit()
	if !strings.Contains(wat, `(import "env" "take_image" (func $take_image (param i32)))`) {
		t.Fatalf("struct extern parameter was not lowered to an i32 pointer:\n%s", wat)
	}
	if !strings.Contains(wat, `(import "env" "make_image" (func $make_image (result i32)))`) {
		t.Fatalf("struct extern result was not lowered to an i32 pointer:\n%s", wat)
	}
}

func TestWabtExternStructCallCopiesReturnedMemory(t *testing.T) {
	image := testExternalStruct()
	program := &hir.Program{Functions: []*hir.Function{
		{Name: "make_image", IsExtern: true, ReturnTypes: []sema.Type{image}},
	}}
	emitter := New(program, nil)
	emitter.emitCallStatic(&hir.InstrCallStatic{
		CalleeName: "make_image",
		Dst:        &hir.Reg{ID: 2, Typ: image},
	})

	call := emitter.b.String()
	if !strings.Contains(call, `(call $make_image)`) {
		t.Fatalf("struct extern call was not emitted:\n%s", call)
	}
	if !strings.Contains(call, `(memory.copy (local.get $v2) (call $make_image) (i32.const `) {
		t.Fatalf("struct extern result was not copied into the Hike destination:\n%s", call)
	}
}

func TestWabtExternStructArgumentUsesExistingMemoryAddress(t *testing.T) {
	image := testExternalStruct()
	program := &hir.Program{Functions: []*hir.Function{
		{Name: "take_image", IsExtern: true, Params: []*hir.Reg{{Typ: image}}},
	}}
	emitter := New(program, nil)
	emitter.emitCallStatic(&hir.InstrCallStatic{
		CalleeName: "take_image",
		Args:       []hir.Value{&hir.Reg{ID: 1, Typ: image}},
	})

	call := emitter.b.String()
	if !strings.Contains(call, `(call $take_image (local.get $v1))`) {
		t.Fatalf("struct extern argument was not passed as its linear-memory address:\n%s", call)
	}
}
