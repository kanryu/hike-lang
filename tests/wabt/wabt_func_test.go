package wabt_test

import "testing"

// WABT counterpart of the implicit-return portion of
// tests/e2e/e2e_func_test.go.
func TestWabtImplicitIntegerReturnThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func GetZeroInt() int {
}

func main() int {
    return GetZeroInt() + 42
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt implicit-return output = %q, want %q", got, want)
	}
}
