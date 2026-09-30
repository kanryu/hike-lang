package wabt_test

import "testing"

func TestWabtMapMissingSliceAppendThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

import "std/maps"

func main() int {
	values := make(map[string][]int)
	values["numbers"] = append(values["numbers"], 10)
	values["numbers"] = append(values["numbers"], 20)
	return values["numbers"][0] + values["numbers"][1]
}`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=30\n"; got != want {
		t.Fatalf("WABT output mismatch: got %q, want %q", got, want)
	}
}
