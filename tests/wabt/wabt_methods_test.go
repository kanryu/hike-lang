package wabt_test

import "testing"

// WABT counterpart of the method auto-deref/auto-address test in
// tests/e2e/e2e_funccall_test.go.
func TestWabtMethodAutoDerefAndAddressThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

type Point struct {
    x int
    y int
}

func (p Point) Sum() int {
    return p.x + p.y
}

func (p *Point) Move(dx int, dy int) {
    p.x = p.x + dx
    p.y = p.y + dy
}

func main() int {
    ptr := &Point{x: 10, y: 20}
    s1 := ptr.Sum()

    var val Point
    val.x = 100
    val.y = 200
    val.Move(5, 10)
    s2 := val.Sum()

    return s1 + s2 + val.x + val.y
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=660\n"; got != want {
		t.Fatalf("Wabt method dispatch output = %q, want %q", got, want)
	}
}
