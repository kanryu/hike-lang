package sema

import "testing"

func TestConstValueTypeLLVMTypeMatchesItsRuntimeLayout(t *testing.T) {
	typ := &ConstValueType{Value: 3}
	if got := LLVMTypeOf(typ); got != "i64" {
		t.Fatalf("LLVMTypeOf(ConstValueType) = %q, want i64", got)
	}
	if got := SizeOf(typ); got != 8 {
		t.Fatalf("SizeOf(ConstValueType) = %d, want 8", got)
	}
}
