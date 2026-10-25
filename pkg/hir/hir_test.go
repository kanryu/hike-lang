package hir

import "testing"

func TestInstrBinaryStringHandlesIncompleteInstruction(t *testing.T) {
	got := (&InstrBinary{}).String()
	if got != "  <nil> = add <nil>, <nil>" {
		t.Fatalf("InstrBinary.String() = %q", got)
	}
}
