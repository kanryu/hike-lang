package wabt_test

import "testing"

// WABT counterpart of tests/e2e/e2e_array_inferred_test.go.
func TestWabtArrayInferredLengthThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    math_lut := [...]byte{
        0x01, 0x02, 0x04, 0x08,
        0x10, 0x20, 0x40, 0x80,
        0x1d, 0x3a, 0x74, 0xe8,
        0xcd, 0x87, 0x13, 0x26,
    }
    return math_lut[0] + math_lut[8] + math_lut[15]
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=68\n"; got != want {
		t.Fatalf("Wabt inferred-array output = %q, want %q", got, want)
	}
}
