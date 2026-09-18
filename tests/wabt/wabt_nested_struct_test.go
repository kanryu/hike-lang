package wabt_test

import "testing"

func TestWabtNestedStructMethodsThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main
type Engine struct { Power int }
func (e Engine) Output() int { return e.Power * 10 }
type Car struct { Eng Engine }
func (c Car) TotalPower() int { return c.Eng.Output() + 50 }
func main() int { eng:=Engine{Power:15}; car:=Car{Eng:eng}; return car.TotalPower() }
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=200\n"; got != want {
		t.Fatalf("Wabt nested-struct output = %q, want %q", got, want)
	}
}

func TestWabtStructMethodNameCollisionThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main
type Circle struct { Radius int }
func (c Circle) Area() int { return c.Radius * c.Radius * 3 }
type Square struct { Side int }
func (s Square) Area() int { return s.Side * s.Side }
func main() int { c:=Circle{Radius:5}; s:=Square{Side:8}; return c.Area()+s.Area() }
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=139\n"; got != want {
		t.Fatalf("Wabt method-collision output = %q, want %q", got, want)
	}
}

func TestWabtNestedStructPointerReceiverThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main
type Leaf struct { Val int }
func (l *Leaf) Add(d int) { l.Val=l.Val+d }
type Node struct { Weight int; Item Leaf }
func (n *Node) Process() int { n.Item.Add(10); return n.Item.Val*n.Weight }
func main() int { leaf:=Leaf{Val:5}; node:=Node{Weight:4,Item:leaf}; return node.Process() }
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=60\n"; got != want {
		t.Fatalf("Wabt nested pointer-receiver output = %q, want %q", got, want)
	}
}

func TestWabtNestedStructForwardReferenceThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main
func (p Parent) Eval() int { return p.Child.Score() * 2 }
type Parent struct { Child SubUnit }
func main() int { sub:=SubUnit{Base:42}; p:=Parent{Child:sub}; return p.Eval() }
type SubUnit struct { Base int }
func (s SubUnit) Score() int { return s.Base+8 }
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=100\n"; got != want {
		t.Fatalf("Wabt forward-reference output = %q, want %q", got, want)
	}
}

func TestWabtStructMethodClosureThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main
type Calculator struct { Factor int }
func (c Calculator) Compute(base int) int {
    fn:=func(delta int) int { return (base+delta)*c.Factor }
    return fn(5)
}
func main() int { calc:=Calculator{Factor:3}; return calc.Compute(15) }
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=60\n"; got != want {
		t.Fatalf("Wabt method-closure output = %q, want %q", got, want)
	}
}
