package hir

import "testing"

func TestInstrCallIfaceStringHandlesUntypedArgument(t *testing.T) {
	got := (&InstrCallIface{Args: []Value{nil}}).String()
	if got == "" {
		t.Fatal("InstrCallIface.String() returned an empty string")
	}
}
