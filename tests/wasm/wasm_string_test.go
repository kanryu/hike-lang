package wasm_test

import "testing"

func TestWasm32StringExecution(t *testing.T) {
	wasm := buildWasm(t, `package main

// The WASM boundary uses a byte pointer and an explicit UTF-8 length. Hike
// constructs string instances directly from that pair before processing it.
cfunc TransformString(input *byte, length int) cstring {
    value := string(input, length)
    return cstring(value + " [string]")
}

cfunc TransformCString(input *byte, length int) cstring {
    raw := cstring(input, length)
    return cstring(string(raw) + " [cstring]")
}

jfunc JSStringLength(input string) int {
    const data = new Uint8Array(wasmMemory.buffer, Number(input), 128)
    let end = 0
    while (data[end] != 0) { end++ }
    return new TextDecoder("utf-8").decode(data.subarray(0, end)).length
}

func main() int {
    return JSStringLength("Wasmからこんにちは")
}
	`, "normal")
	if got, want := runWasm(t, wasm, "wasm_string.js"), "WASM_STRING=Wasmからこんにちは [string]\nWASM_CSTRING=Wasmからこんにちは [cstring]\nWASM_JFUNC=11\n"; got != want {
		t.Fatalf("WASM output = %q, want %q", got, want)
	}
}
