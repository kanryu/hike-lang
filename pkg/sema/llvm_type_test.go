package sema

import "testing"

func TestLLVMTypeOfStructUsesConcreteDispatch(t *testing.T) {
	typ := &StructType{
		Fields: []Field{{Name: "value", Type: TypeInt}},
	}
	if got := LLVMTypeOf(typ); got != "{ i64 }" {
		t.Fatalf("LLVMTypeOf returned %q, want { i64 }", got)
	}

	named := &StructType{Name: "Value", Fields: typ.Fields}
	if got := LLVMTypeOf(named); got != "%struct.Value" {
		t.Fatalf("LLVMTypeOf returned %q for named struct, want %%struct.Value", got)
	}
	if got := SizeOf(typ); got != 8 {
		t.Fatalf("SizeOf returned %d, want 8", got)
	}
}
