package llvm

import (
	"strings"
	"testing"

	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

func TestEmitCastRebuildsFunctionValueFromCodePointer(t *testing.T) {
	functionType := &sema.FuncType{}
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitCast(&hir.InstrCast{
		Dst:    &hir.Reg{ID: 1, Typ: functionType},
		Val:    &hir.Reg{ID: 2, Typ: &sema.PointerType{Base: sema.TypeByte}},
		ToType: functionType,
	})

	ir := emitter.b.String()
	if !strings.Contains(ir, "insertvalue { i8*, i8* } zeroinitializer, i8* %v2, 0") {
		t.Fatalf("function code pointer was not inserted into the function value: %s", ir)
	}
	if !strings.Contains(ir, "insertvalue { i8*, i8* }") || !strings.Contains(ir, "i8* null, 1") {
		t.Fatalf("function environment pointer was not initialized: %s", ir)
	}
	if strings.Contains(ir, "select i1 true, { i8*, i8* } zeroinitializer") {
		t.Fatalf("function value was replaced with a zero aggregate: %s", ir)
	}
}

func TestEmitCastDoesNotTreatInterfaceLayoutAsFunctionValue(t *testing.T) {
	interfaceType := &sema.InterfaceType{
		Name:    "Reader",
		Methods: []sema.Method{{Name: "Read"}},
	}
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitCast(&hir.InstrCast{
		Dst:    &hir.Reg{ID: 1, Typ: interfaceType},
		Val:    &hir.Reg{ID: 2, Typ: &sema.PointerType{Base: sema.TypeByte}},
		ToType: interfaceType,
	})

	ir := emitter.b.String()
	if strings.Contains(ir, "insertvalue { i8*, i8* } zeroinitializer, i8* %v2, 0") {
		t.Fatalf("interface layout was incorrectly rebuilt as a function value: %s", ir)
	}
	if !strings.Contains(ir, "select i1 true, { i8*, i8* } zeroinitializer") {
		t.Fatalf("expected the generic aggregate fallback for a non-function destination: %s", ir)
	}
}

func TestHikeVariadicFunctionsUseTypedSliceABI(t *testing.T) {
	ctx := sema.NewContext()
	ctx.Functions["hikeVariadic"] = &sema.FuncType{
		Name:       "hikeVariadic",
		ParamTypes: []sema.Type{sema.TypeString, &sema.SliceType{Elem: sema.TypeInt}},
		IsVariadic: true,
		IsCFunc:    false,
	}
	ctx.Functions["cVariadic"] = &sema.FuncType{
		Name:       "cVariadic",
		ParamTypes: []sema.Type{sema.TypeString},
		IsVariadic: true,
		IsCFunc:    true,
	}
	ctx.Functions["externVariadic"] = &sema.FuncType{
		Name:       "externVariadic",
		ParamTypes: []sema.Type{sema.TypeString},
		IsVariadic: true,
		IsExtern:   true,
	}

	emitter := &Emitter{semaCtx: ctx, renderedTypes: make(map[string]string)}
	if isVar, _ := emitter.isVariadicFunc("hikeVariadic"); isVar {
		t.Fatal("ordinary Hike variadic functions must not use LLVM varargs")
	}
	if isVar, sig := emitter.isVariadicFunc("cVariadic"); !isVar || !strings.HasSuffix(sig, ", ...)") {
		t.Fatalf("C variadic signature was not preserved: isVar=%v sig=%q", isVar, sig)
	}
	if isVar, sig := emitter.isVariadicFunc("externVariadic"); !isVar || !strings.HasSuffix(sig, ", ...)") {
		t.Fatalf("external variadic signature was not preserved: isVar=%v sig=%q", isVar, sig)
	}
}
