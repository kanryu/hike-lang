package e2e_test

import "testing"

// The LLVM emitter must compare aggregate values by their semantic fields,
// including arrays and aggregates nested inside named structs. This is an
// execution-level regression test for the emitter's recursive equality path.
func TestEmitter_NestedStructAndArrayEquality(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Inner struct {
    value int
}

type Outer struct {
    inner Inner
    values [2]int
}

func main() int {
    left := Outer{}
    left.inner.value = 7
    left.values[0] = 10
    left.values[1] = 20

    same := Outer{}
    same.inner.value = 7
    same.values[0] = 10
    same.values[1] = 20

    different := Outer{}
    different.inner.value = 7
    different.values[0] = 10
    different.values[1] = 21

    printf("EQ=%d,NEQ=%d\n", left == same, left != different)
    return 0
}
`,
		ExpectedOut:  "EQ=1,NEQ=1",
		ExpectedExit: 0,
	})
}
