package wabt_test

import "testing"

func TestWabtGenericFunctionsAndGenericStructs(t *testing.T) {
	wasm := buildWabt(t, `package main

func max[T](a T, b T) T {
    if a > b { return a }
    return b
}

func main() int {
    return max[int](37, 5) + max[int](2, 3)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=40\n"; got != want {
		t.Fatalf("Wabt generic output = %q, want %q", got, want)
	}
}
