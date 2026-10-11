package wabt_test

import "testing"

// TestWabtCBufferFromHikeSlice verifies the compiler intrinsic using an
// ordinary Hike slice as the source allocation. Slicing the cbuffer must copy
// into a normal Hike slice, while closing the cbuffer must not invalidate the
// original slice.
func TestWabtCBufferFromHikeSlice(t *testing.T) {
	wasm := buildWabt(t, `package main

func release() {}

func main() int {
    source := make([]byte, 4)
    source[0] = 7
    source[1] = 8
    source[2] = 9
    source[3] = 10

    buffer := cbuffer(&source[0], len(source), release)
    if buffer.Len() != 4 { return 1 }
    if buffer[0] != 7 || buffer[3] != 10 { return 2 }

    copied := buffer[1:3]
    if len(copied) != 2 || copied[0] != 8 || copied[1] != 9 { return 3 }

    buffer.Close()
    if buffer.Len() != 0 { return 4 }
    if source[0] != 7 || source[3] != 10 { return 5 }
    return 42
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt cbuffer output = %q, want %q", got, want)
	}
}
