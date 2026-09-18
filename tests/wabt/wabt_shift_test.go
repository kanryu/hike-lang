package wabt_test

import "testing"

func TestWabtSignedAndUnsignedShiftThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    signed := -8 >> 1
    highBit := uint32(1) << 31
    unsigned := highBit >> 1
    return signed + unsigned
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1073741820\n"; got != want {
		t.Fatalf("Wabt signed/unsigned shift output = %q, want %q", got, want)
	}
}

func TestWabtLeftShiftThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    signed := 3 << 3
    unsigned := uint(3) << 3
    return signed + unsigned
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=48\n"; got != want {
		t.Fatalf("Wabt left shift output = %q, want %q", got, want)
	}
}

func TestWabtShiftCarryThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    left, leftCarry := uint32(0x80000000) << 1
    right, rightCarry := uint32(0x80000001) >> 1
    signedRight, signedCarry := -5 >> 1
    return left + leftCarry + right + rightCarry + signedRight + signedCarry
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1073741824\n"; got != want {
		t.Fatalf("Wabt shift-carry output = %q, want %q", got, want)
	}
}
