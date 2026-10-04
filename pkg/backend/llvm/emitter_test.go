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

func TestEmitBinaryComparesBothInterfaceFields(t *testing.T) {
	interfaceType := &sema.InterfaceType{
		Name:    "Reader",
		Methods: []sema.Method{{Name: "Read"}},
	}
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitBinary(&hir.InstrBinary{
		Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
		Op:  hir.OpEq,
		L:   &hir.Reg{ID: 2, Typ: interfaceType},
		R:   &hir.Reg{ID: 3, Typ: interfaceType},
	})

	ir := emitter.b.String()
	if strings.Count(ir, "extractvalue { i8*, i8* }") != 4 {
		t.Fatalf("interface comparison must extract both fields from both operands: %s", ir)
	}
	if !strings.Contains(ir, "and i1") {
		t.Fatalf("interface comparison must combine data and itab comparisons: %s", ir)
	}
	if !strings.Contains(ir, "icmp eq i8*") {
		t.Fatalf("interface comparison must compare pointer fields: %s", ir)
	}
}

func TestEmitBinaryComparesAllFatPointerLayouts(t *testing.T) {
	tests := []struct {
		name       string
		typ        sema.Type
		fields     int
		fieldTypes []string
	}{
		{
			name:       "any",
			typ:        &sema.InterfaceType{Name: "any"},
			fields:     2,
			fieldTypes: []string{"i32", "i8*"},
		},
		{
			name:       "function",
			typ:        &sema.FuncType{},
			fields:     2,
			fieldTypes: []string{"i8*", "i8*"},
		},
		{
			name:       "slice",
			typ:        &sema.SliceType{Elem: sema.TypeByte},
			fields:     3,
			fieldTypes: []string{"i8*", "i32", "i32"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
				Op:  hir.OpEq,
				L:   &hir.Reg{ID: 2, Typ: tt.typ},
				R:   &hir.Reg{ID: 3, Typ: tt.typ},
			})

			ir := emitter.b.String()
			if got := strings.Count(ir, "extractvalue "+tt.typ.LLVMType()); got != tt.fields*2 {
				t.Fatalf("expected %d field extractions, got %d: %s", tt.fields*2, got, ir)
			}
			for _, fieldType := range tt.fieldTypes {
				if !strings.Contains(ir, "icmp eq "+fieldType) {
					t.Fatalf("missing comparison for %s: %s", fieldType, ir)
				}
			}
		})
	}
}

func TestEmitBinaryUsesUnsignedInstructionsOnlyForUnsignedOperands(t *testing.T) {
	tests := []struct {
		name  string
		op    hir.Opcode
		left  sema.Type
		right sema.Type
		want  string
	}{
		{name: "unsigned division", op: hir.OpDiv, left: sema.TypeUint32, right: sema.TypeUint32, want: "udiv"},
		{name: "mixed division", op: hir.OpDiv, left: sema.TypeUint32, right: sema.TypeInt32, want: "sdiv"},
		{name: "unsigned remainder", op: hir.OpRem, left: sema.TypeUint32, right: sema.TypeUint32, want: "urem"},
		{name: "mixed remainder", op: hir.OpRem, left: sema.TypeInt32, right: sema.TypeUint32, want: "srem"},
		{name: "unsigned less-than", op: hir.OpLt, left: sema.TypeUint32, right: sema.TypeUint32, want: "icmp ult"},
		{name: "mixed less-than", op: hir.OpLt, left: sema.TypeUint32, right: sema.TypeInt32, want: "icmp slt"},
		{name: "unsigned right-shift", op: hir.OpShr, left: sema.TypeUint32, right: sema.TypeUint32, want: "lshr"},
		{name: "mixed right-shift", op: hir.OpShr, left: sema.TypeUint32, right: sema.TypeInt32, want: "ashr"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeInt32},
				Op:  tt.op,
				L:   &hir.Reg{ID: 2, Typ: tt.left},
				R:   &hir.Reg{ID: 3, Typ: tt.right},
			})
			if ir := emitter.b.String(); !strings.Contains(ir, tt.want) {
				t.Fatalf("expected %q in IR: %s", tt.want, ir)
			}
		})
	}
}

func TestEmitBinaryStringOrderingUsesLengthAwareRuntime(t *testing.T) {
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitBinary(&hir.InstrBinary{
		Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
		Op:  hir.OpLt,
		L:   &hir.Reg{ID: 2, Typ: sema.TypeString},
		R:   &hir.Reg{ID: 3, Typ: sema.TypeString},
	})

	ir := emitter.b.String()
	if !strings.Contains(ir, "call i32 @hike_strcmp_len") {
		t.Fatalf("string ordering must use the length-aware comparison runtime: %s", ir)
	}
	if strings.Contains(ir, "icmp slt { i8*, i32, i32 }") {
		t.Fatalf("string ordering must not compare aggregate values with icmp: %s", ir)
	}
}

func TestEmitBinaryDispatchesEveryFatBinaryType(t *testing.T) {
	tests := []struct {
		name string
		typ  sema.Type
	}{
		{name: "interface", typ: &sema.InterfaceType{Name: "Reader", Methods: []sema.Method{{Name: "Read"}}}},
		{name: "function", typ: &sema.FuncType{}},
		{name: "slice", typ: &sema.SliceType{Elem: sema.TypeByte}},
		{name: "array", typ: &sema.ArrayType{Len: 2, Elem: sema.TypeByte}},
		{name: "struct", typ: &sema.StructType{Name: "Pair", Fields: []sema.Field{{Name: "value", Type: sema.TypeInt32}}}},
		{name: "tuple", typ: &sema.TupleType{Types: []sema.Type{sema.TypeInt32, sema.TypeByte}}},
		{name: "string", typ: sema.TypeString},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
				Op:  hir.OpEq,
				L:   &hir.Reg{ID: 2, Typ: tt.typ},
				R:   &hir.Reg{ID: 3, Typ: tt.typ},
			})

			ir := emitter.b.String()
			if strings.Contains(ir, "icmp eq "+tt.typ.LLVMType()) {
				t.Fatalf("%s comparison must not use icmp directly on a fat type: %s", tt.name, ir)
			}
			if tt.name == "string" {
				if !strings.Contains(ir, "call i1 @hike_streq_len") {
					t.Fatalf("string comparison must use the length-aware runtime: %s", ir)
				}
				return
			}
			if !strings.Contains(ir, "extractvalue "+tt.typ.LLVMType()) {
				t.Fatalf("%s comparison did not dispatch to its fat-value implementation: %s", tt.name, ir)
			}
		})
	}
}

func TestEmitBinaryStringOrderingUses32BitRuntime(t *testing.T) {
	emitter := &Emitter{
		pointerBits:   32,
		targetTriple:  "wasm32-unknown-unknown",
		renderedTypes: make(map[string]string),
	}
	emitter.emitBinary(&hir.InstrBinary{
		Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
		Op:  hir.OpLt,
		L:   &hir.Reg{ID: 2, Typ: sema.TypeString},
		R:   &hir.Reg{ID: 3, Typ: sema.TypeString},
	})

	if ir := emitter.b.String(); !strings.Contains(ir, "call i32 @hike_strcmp_len32") {
		t.Fatalf("32-bit string comparison used the wrong runtime: %s", ir)
	}
}

func TestEmitBinaryComparesNamedAndNestedAggregates(t *testing.T) {
	nested := &sema.StructType{
		Name: "NestedValue",
		Fields: []sema.Field{
			{Name: "x", Type: sema.TypeInt32},
			{Name: "y", Type: &sema.ArrayType{Len: 2, Elem: sema.TypeUint8}},
		},
	}
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitBinary(&hir.InstrBinary{
		Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
		Op:  hir.OpEq,
		L:   &hir.Reg{ID: 2, Typ: nested},
		R:   &hir.Reg{ID: 3, Typ: nested},
	})

	ir := emitter.b.String()
	if !strings.Contains(ir, "extractvalue %struct.NestedValue") {
		t.Fatalf("named struct fields were not extracted: %s", ir)
	}
	if !strings.Contains(ir, "extractvalue [2 x i8]") {
		t.Fatalf("nested array fields were not recursively compared: %s", ir)
	}
	if strings.Count(ir, "icmp eq") != 3 {
		t.Fatalf("expected one scalar and two nested element comparisons: %s", ir)
	}
}

func TestEmitBinaryFatTypesUseTheirDeclaredLayouts(t *testing.T) {
	tests := []struct {
		name       string
		typ        sema.Type
		extracts   int
		comparison string
	}{
		{
			name:       "array",
			typ:        &sema.ArrayType{Len: 3, Elem: sema.TypeInt16},
			extracts:   6,
			comparison: "icmp eq i16",
		},
		{
			name: "struct",
			typ: &sema.StructType{
				Name: "Triple",
				Fields: []sema.Field{
					{Name: "first", Type: sema.TypeInt32},
					{Name: "second", Type: sema.TypeFloat64},
					{Name: "third", Type: sema.TypeByte},
				},
			},
			extracts:   6,
			comparison: "fcmp oeq double",
		},
		{
			name:       "tuple",
			typ:        &sema.TupleType{Types: []sema.Type{sema.TypeInt8, sema.TypeUint64}},
			extracts:   4,
			comparison: "icmp eq i64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
				Op:  hir.OpEq,
				L:   &hir.Reg{ID: 2, Typ: tt.typ},
				R:   &hir.Reg{ID: 3, Typ: tt.typ},
			})

			ir := emitter.b.String()
			if got := strings.Count(ir, "extractvalue "+tt.typ.LLVMType()); got != tt.extracts {
				t.Fatalf("expected %d extracts for %s, got %d: %s", tt.extracts, tt.name, got, ir)
			}
			if !strings.Contains(ir, tt.comparison) {
				t.Fatalf("missing declared-field comparison %q for %s: %s", tt.comparison, tt.name, ir)
			}
		})
	}
}

func TestEmitBinaryNestedStringFieldUsesStringRuntime(t *testing.T) {
	typ := &sema.StructType{
		Name: "StringPair",
		Fields: []sema.Field{
			{Name: "label", Type: sema.TypeString},
			{Name: "value", Type: sema.TypeInt32},
		},
	}
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitBinary(&hir.InstrBinary{
		Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
		Op:  hir.OpEq,
		L:   &hir.Reg{ID: 2, Typ: typ},
		R:   &hir.Reg{ID: 3, Typ: typ},
	})

	ir := emitter.b.String()
	if !strings.Contains(ir, "call i1 @hike_streq_len") {
		t.Fatalf("nested string field must use the length-aware string runtime: %s", ir)
	}
	if strings.Contains(ir, "icmp eq { i8*, i32, i32 }") {
		t.Fatalf("nested string field must not be compared as an aggregate: %s", ir)
	}
}

func TestEmitBinaryIntegerCoversEveryOpcode(t *testing.T) {
	tests := []struct {
		op   hir.Opcode
		want string
	}{
		{hir.OpAdd, "add i32"},
		{hir.OpSub, "sub i32"},
		{hir.OpMul, "mul i32"},
		{hir.OpDiv, "sdiv i32"},
		{hir.OpRem, "srem i32"},
		{hir.OpAnd, "and i32"},
		{hir.OpOr, "or i32"},
		{hir.OpXor, "xor i32"},
		{hir.OpShl, "shl i32"},
		{hir.OpShr, "ashr i32"},
		{hir.OpEq, "icmp eq i32"},
		{hir.OpNeq, "icmp ne i32"},
		{hir.OpLt, "icmp slt i32"},
		{hir.OpLe, "icmp sle i32"},
		{hir.OpGt, "icmp sgt i32"},
		{hir.OpGe, "icmp sge i32"},
	}

	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeInt32},
				Op:  tt.op,
				L:   &hir.Reg{ID: 2, Typ: sema.TypeInt32},
				R:   &hir.Reg{ID: 3, Typ: sema.TypeInt32},
			})
			if ir := emitter.b.String(); !strings.Contains(ir, tt.want) {
				t.Fatalf("expected %q for %s: %s", tt.want, tt.op, ir)
			}
		})
	}
}

func TestEmitBinaryFloatCoversEverySupportedOpcode(t *testing.T) {
	tests := []struct {
		op   hir.Opcode
		want string
	}{
		{hir.OpAdd, "fadd double"},
		{hir.OpSub, "fsub double"},
		{hir.OpMul, "fmul double"},
		{hir.OpDiv, "fdiv double"},
		{hir.OpEq, "fcmp oeq double"},
		{hir.OpNeq, "fcmp one double"},
		{hir.OpLt, "fcmp olt double"},
		{hir.OpLe, "fcmp ole double"},
		{hir.OpGt, "fcmp ogt double"},
		{hir.OpGe, "fcmp oge double"},
	}

	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeFloat64},
				Op:  tt.op,
				L:   &hir.Reg{ID: 2, Typ: sema.TypeFloat64},
				R:   &hir.Reg{ID: 3, Typ: sema.TypeFloat64},
			})
			if ir := emitter.b.String(); !strings.Contains(ir, tt.want) {
				t.Fatalf("expected %q for %s: %s", tt.want, tt.op, ir)
			}
		})
	}
}

func TestEmitBinaryStringCoversAllComparisons(t *testing.T) {
	for _, op := range []hir.Opcode{hir.OpEq, hir.OpNeq, hir.OpLt, hir.OpLe, hir.OpGt, hir.OpGe} {
		t.Run(op.String(), func(t *testing.T) {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
				Op:  op,
				L:   &hir.Reg{ID: 2, Typ: sema.TypeString},
				R:   &hir.Reg{ID: 3, Typ: sema.TypeString},
			})
			ir := emitter.b.String()
			if op == hir.OpEq || op == hir.OpNeq {
				if !strings.Contains(ir, "call i1 @hike_streq_len") {
					t.Fatalf("string equality must use hike_streq_len: %s", ir)
				}
			} else if !strings.Contains(ir, "call i32 @hike_strcmp_len") {
				t.Fatalf("string ordering must use hike_strcmp_len: %s", ir)
			}
		})
	}
}

func TestEmitBinaryFatTypesCoverEqualityAndInequality(t *testing.T) {
	types := []sema.Type{
		&sema.InterfaceType{Name: "Reader", Methods: []sema.Method{{Name: "Read"}}},
		&sema.FuncType{},
		&sema.SliceType{Elem: sema.TypeByte},
		&sema.ArrayType{Len: 2, Elem: sema.TypeInt32},
		&sema.StructType{Name: "Pair", Fields: []sema.Field{{Name: "value", Type: sema.TypeInt32}}},
		&sema.TupleType{Types: []sema.Type{sema.TypeInt32, sema.TypeByte}},
	}

	for _, op := range []hir.Opcode{hir.OpEq, hir.OpNeq} {
		for _, typ := range types {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
				Op:  op,
				L:   &hir.Reg{ID: 2, Typ: typ},
				R:   &hir.Reg{ID: 3, Typ: typ},
			})
			if ir := emitter.b.String(); !strings.Contains(ir, "select i1 true, i1") {
				t.Fatalf("%s on %s did not produce a boolean result: %s", op, typ.TypeName(), ir)
			}
		}
	}
}

func TestEmitBinaryCoversPointerEqualityWithoutIntegerFallback(t *testing.T) {
	pointerType := &sema.PointerType{
		Base: &sema.StructType{Name: "sema_BasicType"},
	}

	for _, op := range []hir.Opcode{hir.OpEq, hir.OpNeq} {
		t.Run(op.String(), func(t *testing.T) {
			emitter := &Emitter{renderedTypes: make(map[string]string)}
			emitter.emitBinary(&hir.InstrBinary{
				Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
				Op:  op,
				L:   &hir.Reg{ID: 2, Typ: pointerType},
				R:   &hir.Reg{ID: 3, Typ: pointerType},
			})

			ir := emitter.b.String()
			want := "icmp " + map[hir.Opcode]string{
				hir.OpEq:  "eq",
				hir.OpNeq: "ne",
			}[op] + " %struct.sema_BasicType* %v2, %v3"
			if !strings.Contains(ir, want) {
				t.Fatalf("pointer %s must use a pointer comparison, want %q: %s", op, want, ir)
			}
			if strings.Contains(ir, "icmp "+map[hir.Opcode]string{
				hir.OpEq:  "eq",
				hir.OpNeq: "ne",
			}[op]+" i64") {
				t.Fatalf("pointer %s was routed through an integer comparison: %s", op, ir)
			}
		})
	}
}

func TestEmitBinaryCastsIntegerLiteralsForPointerEquality(t *testing.T) {
	pointerType := &sema.PointerType{
		Base: &sema.StructType{Name: "sema_BasicType"},
	}
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitBinary(&hir.InstrBinary{
		Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
		Op:  hir.OpNeq,
		L:   &hir.Reg{ID: 2, Typ: pointerType},
		R:   &hir.ConstInt{Val: 1, Typ: pointerType},
	})

	ir := emitter.b.String()
	if !strings.Contains(ir, "inttoptr i64 1 to %struct.sema_BasicType*") {
		t.Fatalf("pointer literal was not converted to a pointer: %s", ir)
	}
	if strings.Contains(ir, "icmp ne %struct.sema_BasicType* %v2, 1") {
		t.Fatalf("pointer comparison still emits an integer literal: %s", ir)
	}
}

func TestEmitBinaryComparesInterfaceAndConcretePointer(t *testing.T) {
	interfaceType := &sema.InterfaceType{
		Name:    "Type",
		Methods: []sema.Method{{Name: "Marker"}},
	}
	pointerType := &sema.PointerType{Base: &sema.StructType{Name: "sema_BasicType"}}

	for _, tt := range []struct {
		name          string
		left, right   hir.Value
		interfaceLeft bool
	}{
		{
			name:          "interface-left",
			left:          &hir.Reg{ID: 2, Typ: interfaceType},
			right:         &hir.Reg{ID: 3, Typ: pointerType},
			interfaceLeft: true,
		},
		{
			name:  "interface-right",
			left:  &hir.Reg{ID: 2, Typ: pointerType},
			right: &hir.Reg{ID: 3, Typ: interfaceType},
		},
	} {
		for _, op := range []hir.Opcode{hir.OpEq, hir.OpNeq} {
			t.Run(tt.name+"/"+op.String(), func(t *testing.T) {
				emitter := &Emitter{renderedTypes: make(map[string]string)}
				emitter.emitBinary(&hir.InstrBinary{
					Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
					Op:  op,
					L:   tt.left,
					R:   tt.right,
				})

				ir := emitter.b.String()
				if !strings.Contains(ir, "extractvalue { i8*, i8* }") {
					t.Fatalf("interface data field was not extracted: %s", ir)
				}
				if !strings.Contains(ir, "bitcast %struct.sema_BasicType* ") {
					t.Fatalf("concrete pointer was not normalized to i8*: %s", ir)
				}
				if !strings.Contains(ir, "icmp "+map[hir.Opcode]string{hir.OpEq: "eq", hir.OpNeq: "ne"}[op]+" i8*") {
					t.Fatalf("mixed comparison did not compare data pointers: %s", ir)
				}
				if strings.Contains(ir, "extractvalue %struct.sema_BasicType*") {
					t.Fatalf("concrete pointer was incorrectly treated as an aggregate: %s", ir)
				}
			})
		}
	}

	anyType := &sema.InterfaceType{Name: "any"}
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitBinary(&hir.InstrBinary{
		Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
		Op:  hir.OpEq,
		L:   &hir.Reg{ID: 2, Typ: anyType},
		R:   &hir.Reg{ID: 3, Typ: pointerType},
	})
	if ir := emitter.b.String(); !strings.Contains(ir, "extractvalue { i32, i8* }") {
		t.Fatalf("any interface data field was not extracted: %s", ir)
	}
}

func TestEmitBinaryComparesInterfaceWithPointerLiteral(t *testing.T) {
	interfaceType := &sema.InterfaceType{
		Name:    "Type",
		Methods: []sema.Method{{Name: "Marker"}},
	}
	emitter := &Emitter{renderedTypes: make(map[string]string)}
	emitter.emitBinary(&hir.InstrBinary{
		Dst: &hir.Reg{ID: 1, Typ: sema.TypeBool},
		Op:  hir.OpNeq,
		L:   &hir.Reg{ID: 2, Typ: interfaceType},
		R:   &hir.ConstInt{Val: 1, Typ: interfaceType},
	})

	ir := emitter.b.String()
	if !strings.Contains(ir, "inttoptr i64 1 to i8*") {
		t.Fatalf("interface pointer literal was not normalized: %s", ir)
	}
	if strings.Contains(ir, "extractvalue { i8*, i8* } 1") {
		t.Fatalf("pointer literal was incorrectly treated as an aggregate: %s", ir)
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
