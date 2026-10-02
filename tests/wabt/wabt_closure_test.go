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

func TestWabtClosureCapturesReceiverAndParameterAfterReturn(t *testing.T) {
	wasm := buildWabt(t, `package main

type Counter struct {
    value int
}

// The closure is invoked after MakeReader has returned, so both captured
// parameter slots must have been promoted out of the activation record.
func (c *Counter) MakeReader(offset int) func() int {
    scratch := "stack"
    scratch += "-reuse"
    scratch += "-one"
    scratch += "-two"
    scratch += "-three"
    scratchLen := len(scratch)
    _ = scratchLen
    return func() int {
        return c.value + offset
    }
}

func main() int {
    counter := &Counter{value: 40}
    first := counter.MakeReader(2)
    second := (&Counter{value: 100}).MakeReader(3)
    return first() + second()
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=145\n"; got != want {
		t.Fatalf("Wabt receiver/parameter closure output = %q, want %q", got, want)
	}
}
