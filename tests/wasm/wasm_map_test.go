package wasm_test

import "testing"

func TestWasm32MapBuildAndNodeExecution(t *testing.T) {
	wasm := buildWasm(t, `package main

import "std/maps"

func main() int {
    values := make(map[string]int)
    values["answer"] = 42
    return values["answer"]
}
`, "normal")
	if got, want := runWasm(t, wasm, "wasm_map.js"), "WASM_RESULT=42\n"; got != want {
		t.Fatalf("WASM output = %q, want %q", got, want)
	}
}
