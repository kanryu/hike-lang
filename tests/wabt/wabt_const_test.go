package wabt_test

import "testing"

// This is the WABT counterpart of tests/e2e/e2e_const_test.go.  It keeps the
// iota semantics while observing the result through the WASM export because
// the current WABT runner does not yet provide a variadic printf host import.
func TestWabtConstIotaThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

const First = iota

func main() int {
    return First + 42
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt const/iota output = %q, want %q", got, want)
	}
}
