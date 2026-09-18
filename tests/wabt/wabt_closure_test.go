package wabt_test

import "testing"

func TestWabtClosureCaptureThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func makeAdder(base int) func(int) int {
    return func(n int) int {
        base = base + n
        return base
    }
}

func main() int {
    add := makeAdder(10)
    first := add(5)
    second := add(5)
    return first + second
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=35\n"; got != want {
		t.Fatalf("Wabt closure output = %q, want %q", got, want)
	}
}
