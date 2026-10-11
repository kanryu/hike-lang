package llvm

import (
	"strings"
	"testing"

	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

func TestExternStructValueUsesByvalABI(t *testing.T) {
	image := &sema.StructType{
		Name: "Image",
		Fields: []sema.Field{
			{Name: "width", Type: sema.TypeInt32},
			{Name: "height", Type: sema.TypeInt32},
			{Name: "channel", Type: sema.TypeInt32},
			{Name: "data", Type: &sema.PointerType{Base: sema.TypeByte}},
		},
	}
	extern := &hir.Function{
		Name:     "take_image",
		IsExtern: true,
		Params:   []*hir.Reg{{Typ: image}},
	}
	program := &hir.Program{Functions: []*hir.Function{extern}}
	emitter := NewEmitter(program, nil, "x86_64-w64-windows-gnu", "", false)
	emitter.emitExternalDeclaration(extern)

	declaration := emitter.b.String()
	want := "declare void @take_image(%struct.Image* byval(%struct.Image) align 8)"
	if !strings.Contains(declaration, want) {
		t.Fatalf("extern declaration does not use the Windows C ABI byval form: %s", declaration)
	}

	emitter.b.Reset()
	emitter.emitCallStatic(&hir.InstrCallStatic{
		CalleeName: "take_image",
		Args:       []hir.Value{&hir.Reg{ID: 1, Typ: image}},
	})
	call := emitter.b.String()
	if !strings.Contains(call, "alloca %struct.Image, align 8") {
		t.Fatalf("struct argument was not materialized before the extern call: %s", call)
	}
	if !strings.Contains(call, "store %struct.Image %v1") {
		t.Fatalf("struct argument was not copied into the byval temporary: %s", call)
	}
	if !strings.Contains(call, "byval(%struct.Image) align 8") {
		t.Fatalf("extern call does not carry the byval ABI attribute: %s", call)
	}
}

func TestExternStructReturnUsesSRetABI(t *testing.T) {
	image := &sema.StructType{
		Name: "Image",
		Fields: []sema.Field{
			{Name: "width", Type: sema.TypeInt32},
			{Name: "height", Type: sema.TypeInt32},
			{Name: "channel", Type: sema.TypeInt32},
			{Name: "data", Type: &sema.PointerType{Base: sema.TypeByte}},
		},
	}
	extern := &hir.Function{
		Name:        "make_image",
		IsExtern:    true,
		ReturnTypes: []sema.Type{image},
	}
	program := &hir.Program{Functions: []*hir.Function{extern}}
	emitter := NewEmitter(program, nil, "x86_64-w64-windows-gnu", "", false)
	emitter.emitExternalDeclaration(extern)

	declaration := emitter.b.String()
	want := "declare void @make_image(%struct.Image* sret(%struct.Image) align 8)"
	if !strings.Contains(declaration, want) {
		t.Fatalf("extern declaration does not use the sret ABI form: %s", declaration)
	}

	emitter.b.Reset()
	emitter.emitCallStatic(&hir.InstrCallStatic{
		CalleeName: "make_image",
		Dst:        &hir.Reg{ID: 2, Typ: image},
	})
	call := emitter.b.String()
	if !strings.Contains(call, "alloca %struct.Image, align 8") {
		t.Fatalf("struct return did not allocate an sret destination: %s", call)
	}
	if !strings.Contains(call, "call void @make_image(%struct.Image* sret(%struct.Image) align 8") {
		t.Fatalf("struct return call does not pass the sret destination: %s", call)
	}
	if !strings.Contains(call, "load %struct.Image, %struct.Image*") {
		t.Fatalf("struct return was not loaded back into the Hike value: %s", call)
	}
}
