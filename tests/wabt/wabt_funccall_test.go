package wabt_test

import "testing"

// WABT counterpart of the integer part of
// tests/e2e/e2e_funccall_test.go's generic-call coverage.
func TestWabtGenericFunctionCallThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func Max[T](a T, b T) T {
    if a > b {
        return a
    }
    return b
}

func main() int {
    return Max[int](10, 20)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=20\n"; got != want {
		t.Fatalf("Wabt generic call output = %q, want %q", got, want)
	}
}

func TestWabtDefaultArgumentsThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func Compute(base int, mult int = 2, add int = 5) int {
    return base * mult + add
}

func main() int {
    r1 := Compute(10)
    r2 := Compute(10, 3)
    r3 := Compute(10, 3, 1)
    return r1 + r2 + r3
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=91\n"; got != want {
		t.Fatalf("Wabt default-argument output = %q, want %q", got, want)
	}
}

func TestWabtNumericCastsThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    valueA := int('A')
    valueZ := int64('Z')
    return valueA + int(valueZ)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=155\n"; got != want {
		t.Fatalf("Wabt numeric-cast output = %q, want %q", got, want)
	}
}

func TestWabtSliceMakeCapAppendThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    s := make([]int, 2, 4)
    s[0] = 10
    s[1] = 20
    len1 := len(s)
    cap1 := cap(s)
    s = append(s, 30, 40)
    len2 := len(s)
    cap2 := cap(s)
    s = append(s, 50)
    len3 := len(s)
    cap3 := cap(s)
    grown := 0
    if cap3 > cap2 {
        grown = 1
    }
    return s[4] + grown*100 + len1 + cap1 + len2 + cap2 + len3
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=169\n"; got != want {
		t.Fatalf("Wabt slice make/cap/append output = %q, want %q", got, want)
	}
}
