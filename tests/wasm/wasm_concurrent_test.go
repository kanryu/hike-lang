package wasm_test

import "testing"

func TestWasm32ConcurrentModeExecution(t *testing.T) {
	wasm := buildWasm(t, `package main

func main() int {
    task := Async(func() int {
        return 6 * 7
    })
    return <-task
}
`, "concurrent")
	if got, want := runWasm(t, wasm, "wasm_concurrent.js"), "WASM_RESULT=42\n"; got != want {
		t.Fatalf("WASM output = %q, want %q", got, want)
	}
}
