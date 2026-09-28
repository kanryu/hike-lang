package e2e_test

import "testing"

// 文法・制御構文・型・構造体の独立した検証を一つのHikeC実行にまとめる。
func TestGrammar_Batch(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...)

func swap(a int, b int) (int, int) { return b, a }
func fib(n int) int { if n <= 0 { return 0 }; if n == 1 { return 1 }; return fib(n-1) + fib(n-2) }

func makeCounter(start int) func() int {
    c := start
    return func() int { c = c + 1; return c }
}

func printDeferNum(n int) { printf("%d ", n) }
func testDefer() { defer printDeferNum(1); defer printDeferNum(2); defer printDeferNum(3); printf("CASE=defer:START ") }

func mutate(p *int) { *p = *p + 50 }

type Point struct { X int; Y int }
func (p Point) Sum() int { return p.X + p.Y }

type Counter struct { Val int }
func (c *Counter) Inc(delta int) { c.Val = c.Val + delta }
func (c *Counter) Get() int { return c.Val }

type Base struct { Id int }
type Item struct { *Base; Price int }

type Engine struct { Power int }
func (e Engine) Output() int { return e.Power * 10 }
type Car struct { Eng Engine }
func (c Car) TotalPower() int { return c.Eng.Output() + 50 }

type Circle struct { Radius int }
func (c Circle) Area() int { return c.Radius * c.Radius * 3 }
type Square struct { Side int }
func (s Square) Area() int { return s.Side * s.Side }

type Parent struct { Child SubUnit }
type SubUnit struct { Base int }
func (p Parent) Eval() int { return p.Child.Score() * 2 }
func (s SubUnit) Score() int { return s.Base + 8 }

type Calculator struct { Factor int }
func (c Calculator) Compute(base int) int {
    fn := func(delta int) int { return (base + delta) * c.Factor }
    return fn(5)
}

type Leaf struct { Val int }
func (l *Leaf) Add(d int) { l.Val = l.Val + d }
type Node struct { Weight int; Item Leaf }
func (n *Node) Process() int { n.Item.Add(10); return n.Item.Val * n.Weight }

type Greeter interface { Greet() string }
type Robot struct { Model string }
func (r *Robot) Greet() string { return r.Model }

func main() int {
    printf("CASE=arithmetic:%d\n", 10+20*3-5/2%3)
    a := 5; b := 3
    printf("CASE=bitwise:%d,%d,%d,%d,%d\n", a&b, a|b, a^b, a<<2, a>>1)
    x := 10; x += 5; x -= 2; x *= 3; x++; x--
    printf("CASE=compound:%d\n", x)
    printf("CASE=logical:%d,%d,%d\n", !(true && false) || (10 > 20), (5 >= 10) && (100 == 100), 10 != 20)

    var va, vb int = 10, 20; va, vb = vb, va; vx, vy := 30, 40
    printf("CASE=multi:%d,%d,%d,%d\n", va, vb, vx, vy)
    if v := 15; v > 20 { printf("CASE=if:HIGH\n") } else if v > 10 { printf("CASE=if:MID=%d\n", v) } else { printf("CASE=if:LOW\n") }
    sum := 0
    for i := 0; i < 10; i = i + 1 { if i == 3 { continue }; if i == 7 { break }; sum += i }
    printf("CASE=for:%d\n", sum)
    n := 1; for n < 16 { n *= 2 }; printf("CASE=while:%d\n", n)
    val := 2; switch val { case 1: printf("CASE=switch:ONE\n"); case 2, 3: printf("CASE=switch:TWO_OR_THREE\n"); default: printf("CASE=switch:OTHER\n") }
    score := 85; switch { case score >= 90: printf("CASE=switchless:A\n"); case score >= 80: printf("CASE=switchless:B\n"); default: printf("CASE=switchless:C\n") }

    rx, ry := swap(100, 200); printf("CASE=returns:%d,%d\n", rx, ry)
    printf("CASE=recursion:%d\n", fib(10))
    cnt := makeCounter(10); printf("CASE=closure:%d,%d\n", cnt(), cnt())
    testDefer(); printf("END\n")
    px := 10; mutate(&px); printf("CASE=pointer:%d\n", px)

    pt := Point{X: 12, Y: 34}; printf("CASE=value-receiver:%d\n", pt.Sum())
    counter := Counter{Val: 100}; counter.Inc(25); printf("CASE=pointer-receiver:%d\n", counter.Get())
    item := Item{Base: &Base{Id: 777}, Price: 500}; printf("CASE=embedding:%d,%d\n", item.Id, item.Price)
    eng := Engine{Power: 15}; car := Car{Eng: eng}; printf("CASE=nested:%d\n", car.TotalPower())
    circle := Circle{Radius: 5}; square := Square{Side: 8}; printf("CASE=method-collision:%d,%d\n", circle.Area(), square.Area())
    parent := Parent{Child: SubUnit{Base: 42}}; printf("CASE=forward:%d\n", parent.Eval())
    calc := Calculator{Factor: 3}; printf("CASE=method-closure:%d\n", calc.Compute(15))
    node := Node{Weight: 4, Item: Leaf{Val: 5}}; printf("CASE=deep-pointer:%d\n", node.Process())

    arr := [3]int{100, 200, 300}; arr[1] = 999; printf("CASE=array:%d,%d,%d\n", arr[0], arr[1], arr[2])
    sl := make([]int, 0, 4); sl = append(sl, 10, 20, 30); printf("CASE=slice:%d,%d,%d\n", len(sl), cap(sl), sl[1])
    five := [5]int{10, 20, 30, 40, 50}; part := five[1:4]; printf("CASE=array-subslice:%d,%d,%d\n", len(part), part[0], part[2])
    words := [3]string{"A", "B", "C"}; printf("CASE=range:"); for i, word := range words { printf("%d:%s ", i, word) }; printf("\n")
    text := "HikeLang"; printf("CASE=string:%d,%c,%c\n", len(text), text[0], text[4])
    floating := 12.85; integer := int(floating); printf("CASE=cast:%d,%.1f\n", integer, float64(integer)+0.5)
    var greeter Greeter = &Robot{Model: "RX-78"}; printf("CASE=interface:%s\n", greeter.Greet())
    var anyValue any = 1234; asserted, ok := anyValue.(int); printf("CASE=assertion:%d,%d\n", asserted, ok)
    return 42
}
`,
		ExpectedOut:  "CASE=arithmetic:68\nCASE=bitwise:1,7,6,20,2\nCASE=compound:39\nCASE=logical:1,0,1\nCASE=multi:20,10,30,40\nCASE=if:MID=15\nCASE=for:18\nCASE=while:16\nCASE=switch:TWO_OR_THREE\nCASE=switchless:B\nCASE=returns:200,100\nCASE=recursion:55\nCASE=closure:11,12\nCASE=defer:START 3 2 1 END\nCASE=pointer:60\nCASE=value-receiver:46\nCASE=pointer-receiver:125\nCASE=embedding:777,500\nCASE=nested:200\nCASE=method-collision:75,64\nCASE=forward:100\nCASE=method-closure:60\nCASE=deep-pointer:60\nCASE=array:100,999,300\nCASE=slice:3,4,20\nCASE=array-subslice:3,20,40\nCASE=range:0:A 1:B 2:C \nCASE=string:8,H,L\nCASE=cast:12,12.5\nCASE=interface:RX-78\nCASE=assertion:1234,1",
		ExpectedExit: 42,
	})
}
