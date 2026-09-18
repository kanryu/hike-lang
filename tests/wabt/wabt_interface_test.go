package wabt_test

import "testing"

func TestWabtInterfaceDynamicDispatchThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

type Runner interface { Run() int }
type Value struct { n int }

func (v *Value) Run() int { return v.n }
func call(r Runner) int { return r.Run() }

func main() int {
    v := &Value{n: 42}
    return call(v)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt interface dispatch output = %q, want %q", got, want)
	}
}

func TestWabtInterfaceMultiValueDispatchThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

type Calculator interface {
    Add(n int) int
    Multiply(n int) int
}

type Number struct { val int }

func (n *Number) Add(x int) int { n.val = n.val + x; return n.val }
func (n *Number) Multiply(x int) int { n.val = n.val * x; return n.val }

func operate(c Calculator) (int, int) {
    a := c.Add(10)
    b := c.Multiply(2)
    return a, b
}

func main() int {
    n := &Number{val: 5}
    a, b := operate(n)
    return a + b + n.val
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=75\n"; got != want {
		t.Fatalf("Wabt interface multi-value output = %q, want %q", got, want)
	}
}

func TestWabtInterfaceMultipleMethodsThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

type Calculator interface {
    Add(n int) int
    Multiply(n int) int
}

type Number struct { val int }

func (n *Number) Add(x int) int {
    n.val = n.val + x
    return n.val
}

func (n *Number) Multiply(x int) int {
    n.val = n.val * x
    return n.val
}

func main() int {
    n := &Number{val: 5}
    var c Calculator = n
    a := c.Add(10)
    b := c.Multiply(2)
    return a + b + n.val
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=75\n"; got != want {
		t.Fatalf("Wabt multiple interface methods output = %q, want %q", got, want)
	}
}
