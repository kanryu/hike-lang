package wabt_test

import "testing"

func TestWabtStringOperations(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    base := "HikeCompiler"
    prefix := base[:4]
    suffix := base[4:]
    same := 0
    if prefix + suffix == base { same = 1 }
    return len(prefix)*1000 + len(suffix)*100 + int(base[0]) + same
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=4873\n"; got != want {
		t.Fatalf("Wabt string output = %q, want %q", got, want)
	}
}
