package wabt_test

import "testing"

func TestWabtStdMathOperations(t *testing.T) {
	wasm := buildWabt(t, `package main

import "std/math"

func near(got float64, want float64) int {
    if math.Abs(got-want) < 0.01 { return 1 }
    return 0
}

func main() int {
    return near(math.Sqrt(9.0), 3.0)*1000 +
        near(math.Pow(2.0, 3.0), 8.0)*100 +
        near(math.Sin(math.Pi/2.0), 1.0)*10 +
        near(math.Log2(8.0), 3.0)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1111\n"; got != want {
		t.Fatalf("Wabt std/math output = %q, want %q", got, want)
	}
}
