package e2e_test

import "testing"

// 構造体のネストとメソッドチェーンの呼び出し検証
func TestGrammar_Nested_Struct_Methods(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Engine struct {
    Power int
}

func (e Engine) Output() int {
    return e.Power * 10
}

type Car struct {
    Eng Engine
}

func (c Car) TotalPower() int {
    return c.Eng.Output() + 50
}

func main() int {
    eng := Engine{Power: 15}
    car := Car{Eng: eng}
    printf("POWER=%d\n", car.TotalPower())
    return 0
}
`,
		ExpectedOut:  "POWER=200",
		ExpectedExit: 0,
	})
}

// 複数構造体における同名メソッド（@StructName マングリング）の衝突回避検証
func TestGrammar_Struct_Method_Name_Collision(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Circle struct {
    Radius int
}

func (c Circle) Area() int {
    return c.Radius * c.Radius * 3
}

type Square struct {
    Side int
}

func (s Square) Area() int {
    return s.Side * s.Side
}

func main() int {
    c := Circle{Radius: 5}
    s := Square{Side: 8}
    printf("CIRCLE=%d,SQUARE=%d\n", c.Area(), s.Area())
    return 0
}
`,
		ExpectedOut:  "CIRCLE=75,SQUARE=64",
		ExpectedExit: 0,
	})
}

// 定義順序に依存しない構造体とメソッドの前方参照検証
func TestGrammar_Forward_Reference_Nested_Struct(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

// 下で定義されている Parent, SubUnit, Score() を先行参照
func (p Parent) Eval() int {
    return p.Child.Score() * 2
}

type Parent struct {
    Child SubUnit
}

func main() int {
    sub := SubUnit{Base: 42}
    p := Parent{Child: sub}
    printf("SCORE=%d\n", p.Eval())
    return 0
}

type SubUnit struct {
    Base int
}

func (s SubUnit) Score() int {
    return s.Base + 8
}
`,
		ExpectedOut:  "SCORE=100",
		ExpectedExit: 0,
	})
}

// 構造体メソッド内でのクロージャ（関数内関数）の生成とフィールド参照検証
func TestGrammar_Struct_Method_With_Closure(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Calculator struct {
    Factor int
}

func (c Calculator) Compute(base int) int {
    fn := func(delta int) int {
        return (base + delta) * c.Factor
    }
    return fn(5)
}

func main() int {
    calc := Calculator{Factor: 3}
    ans := calc.Compute(15)
    printf("ANS=%d\n", ans)
    return 0
}
`,
		ExpectedOut:  "ANS=60",
		ExpectedExit: 0,
	})
}

// ネストされた構造体に対するポインタレシーバでの状態更新検証
func TestGrammar_Deeply_Nested_Pointer_Receiver(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Leaf struct {
    Val int
}

func (l *Leaf) Add(d int) {
    l.Val = l.Val + d
}

type Node struct {
    Weight int
    Item   Leaf
}

func (n *Node) Process() int {
    n.Item.Add(10)
    return n.Item.Val * n.Weight
}

func main() int {
    leaf := Leaf{Val: 5}
    node := Node{Weight: 4, Item: leaf}
    res := node.Process()
    printf("RES=%d\n", res)
    return 0
}
`,
		ExpectedOut:  "RES=60",
		ExpectedExit: 0,
	})
}
