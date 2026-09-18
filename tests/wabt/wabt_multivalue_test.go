package wabt_test

import "testing"

func TestWabtMultiValueReturnAndDestructureThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func pair() (int, int) {
    return 20, 22
}

func main() int {
    a, b := pair()
    return a + b
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt multi-value output = %q, want %q", got, want)
	}
}
