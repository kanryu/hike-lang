package wabt_test

import "testing"

func TestWabtGrammarArithmeticPrecedence(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    return 10 + 20 * 3 - 5 / 2 % 3
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=68\n"; got != want {
		t.Fatalf("Wabt arithmetic result = %q, want %q", got, want)
	}
}

func TestWabtGrammarBitwiseOperations(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    a := 5
    b := 3
    andVal := a & b
    orVal := a | b
    xorVal := a ^ b
    shlVal := a << 2
    shrVal := a >> 1
    return andVal + orVal + xorVal + shlVal + shrVal
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=36\n"; got != want {
		t.Fatalf("Wabt bitwise result = %q, want %q", got, want)
	}
}

func TestWabtGrammarCompoundAssignment(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    x := 10
    x += 5
    x -= 2
    x *= 3
    x++
    x--
    return x
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=39\n"; got != want {
		t.Fatalf("Wabt compound assignment result = %q, want %q", got, want)
	}
}

func TestWabtGrammarLogicalAndComparison(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    tVal := !(true && false) || (10 > 20)
    fVal := (5 >= 10) && (100 == 100)
    neq := 10 != 20
    result := 0
    if tVal {
        result += 100
    }
    if fVal {
        result += 10
    }
    if neq {
        result++
    }
    return result
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=101\n"; got != want {
		t.Fatalf("Wabt logical/comparison result = %q, want %q", got, want)
	}
}

func TestWabtGrammarMultipleAssignment(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    var a, b int = 10, 20
    a, b = b, a
    x, y := 30, 40
    return a + b + x + y
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=100\n"; got != want {
		t.Fatalf("Wabt multiple-assignment result = %q, want %q", got, want)
	}
}

func TestWabtGrammarIfElseWithInitializer(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    result := 0
    if v := 15; v > 20 {
        result = 1
    } else if v > 10 {
        result = v
    } else {
        result = 3
    }
    return result
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=15\n"; got != want {
		t.Fatalf("Wabt if initializer result = %q, want %q", got, want)
	}
}

func TestWabtGrammarForLoopControl(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    sum := 0
    for i := 0; i < 10; i = i + 1 {
        if i == 3 {
            continue
        }
        if i == 7 {
            break
        }
        sum = sum + i
    }
    return sum
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=18\n"; got != want {
		t.Fatalf("Wabt for-loop result = %q, want %q", got, want)
	}
}

func TestWabtGrammarWhileStyleForLoop(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    n := 1
    for n < 16 {
        n = n * 2
    }
    return n
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=16\n"; got != want {
		t.Fatalf("Wabt while-style loop result = %q, want %q", got, want)
	}
}

func TestWabtGrammarSwitchMultipleCases(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    val := 2
    result := 0
    switch val {
    case 1:
        result = 1
    case 2, 3:
        result = 23
    default:
        result = 4
    }
    return result
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=23\n"; got != want {
		t.Fatalf("Wabt switch result = %q, want %q", got, want)
	}
}

func TestWabtGrammarConditionlessSwitch(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    score := 85
    result := 0
    switch {
    case score >= 90:
        result = 1
    case score >= 80:
        result = 2
    default:
        result = 3
    }
    return result
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=2\n"; got != want {
		t.Fatalf("Wabt conditionless switch result = %q, want %q", got, want)
	}
}

func TestWabtGrammarMultipleReturns(t *testing.T) {
	wasm := buildWabt(t, `package main

func swap(a int, b int) (int, int) {
    return b, a
}

func main() int {
    x, y := swap(100, 200)
    return x + y
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=300\n"; got != want {
		t.Fatalf("Wabt multiple-return result = %q, want %q", got, want)
	}
}

func TestWabtGrammarRecursion(t *testing.T) {
	wasm := buildWabt(t, `package main

func fib(n int) int {
    if n <= 0 {
        return 0
    }
    if n == 1 {
        return 1
    }
    return fib(n - 1) + fib(n - 2)
}

func main() int {
    return fib(10)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=55\n"; got != want {
		t.Fatalf("Wabt recursion result = %q, want %q", got, want)
	}
}

func TestWabtGrammarClosureState(t *testing.T) {
	wasm := buildWabt(t, `package main

func makeCounter(start int) func() int {
    c := start
    return func() int {
        c = c + 1
        return c
    }
}

func main() int {
    cnt := makeCounter(10)
    v1 := cnt()
    v2 := cnt()
    return v1*100 + v2
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1112\n"; got != want {
		t.Fatalf("Wabt closure result = %q, want %q", got, want)
	}
}

func TestWabtGrammarDeferLIFO(t *testing.T) {
	wasm := buildWabt(t, `package main

var state int

func mark(n int) {
    state = state*10 + n
}

func testDefer() {
    defer mark(1)
    defer mark(2)
    defer mark(3)
}

func main() int {
    testDefer()
    return state
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=321\n"; got != want {
		t.Fatalf("Wabt defer result = %q, want %q", got, want)
	}
}

func TestWabtGrammarPointerMutation(t *testing.T) {
	wasm := buildWabt(t, `package main

func mutate(p *int) {
    *p = *p + 50
}

func main() int {
    x := 10
    mutate(&x)
    return x
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=60\n"; got != want {
		t.Fatalf("Wabt pointer mutation result = %q, want %q", got, want)
	}
}

func TestWabtGrammarStructValueReceiver(t *testing.T) {
	wasm := buildWabt(t, `package main

type Point struct {
    X int
    Y int
}

func (p Point) Sum() int {
    return p.X + p.Y
}

func main() int {
    pt := Point{X: 12, Y: 34}
    return pt.Sum()
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=46\n"; got != want {
		t.Fatalf("Wabt value-receiver result = %q, want %q", got, want)
	}
}

func TestWabtGrammarStructPointerReceiver(t *testing.T) {
	wasm := buildWabt(t, `package main

type Counter struct {
    Val int
}

func (c *Counter) Inc(delta int) {
    c.Val = c.Val + delta
}

func (c *Counter) Get() int {
    return c.Val
}

func main() int {
    c := Counter{Val: 100}
    c.Inc(25)
    return c.Get()
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=125\n"; got != want {
		t.Fatalf("Wabt pointer-receiver result = %q, want %q", got, want)
	}
}

func TestWabtGrammarStructEmbeddingPromotion(t *testing.T) {
	wasm := buildWabt(t, `package main

type Base struct {
    Id int
}

type Item struct {
    *Base
    Price int
}

func main() int {
    b := &Base{Id: 777}
    item := Item{Base: b, Price: 500}
    return item.Id + item.Price
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1277\n"; got != want {
		t.Fatalf("Wabt embedding result = %q, want %q", got, want)
	}
}

func TestWabtGrammarFixedArrayAccess(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    arr := [3]int{100, 200, 300}
    arr[1] = 999
    return arr[0] + arr[1] + arr[2]
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1399\n"; got != want {
		t.Fatalf("Wabt fixed-array result = %q, want %q", got, want)
	}
}

func TestWabtGrammarSliceMakeAppend(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    s := make([]int, 0, 4)
    s = append(s, 10, 20, 30)
    return len(s)*100 + cap(s)*10 + s[1]
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=360\n"; got != want {
		t.Fatalf("Wabt slice result = %q, want %q", got, want)
	}
}

func TestWabtGrammarArraySubslice(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    arr := [5]int{10, 20, 30, 40, 50}
    sl := arr[1:4]
    return len(sl)*100 + sl[0] + sl[2]
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=360\n"; got != want {
		t.Fatalf("Wabt subslice result = %q, want %q", got, want)
	}
}

func TestWabtGrammarForRangeIteration(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    arr := [3]int{1, 2, 3}
    result := 0
    for i, v := range arr {
        result = result + i*10 + v
    }
    return result
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=36\n"; got != want {
		t.Fatalf("Wabt range result = %q, want %q", got, want)
	}
}

func TestWabtGrammarStringLengthAndIndex(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    str := "HikeLang"
    return len(str)*100 + int(str[0]) + int(str[4])
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=948\n"; got != want {
		t.Fatalf("Wabt string result = %q, want %q", got, want)
	}
}

func TestWabtGrammarNumericCasting(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    f := 12.85
    i := int(f)
    f2 := float64(i) + 0.5
    return i*100 + int(f2*10)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1325\n"; got != want {
		t.Fatalf("Wabt numeric-cast result = %q, want %q", got, want)
	}
}

func TestWabtGrammarInterfaceDynamicDispatch(t *testing.T) {
	wasm := buildWabt(t, `package main

type Greeter interface {
    Greet() int
}

type Robot struct {
    Model int
}

func (r *Robot) Greet() int {
    return r.Model
}

func main() int {
    var g Greeter = &Robot{Model: 78}
    return g.Greet()
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=78\n"; got != want {
		t.Fatalf("Wabt interface-dispatch result = %q, want %q", got, want)
	}
}

func TestWabtGrammarAnyTypeAssertion(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    var a any = 1234
    val, ok := a.(int)
    if ok {
        return val + 10000
    }
    return 0
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=11234\n"; got != want {
		t.Fatalf("Wabt any assertion result = %q, want %q", got, want)
	}
}

func TestWabtGrammarProcessExitCode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    return 42
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt process-exit result = %q, want %q", got, want)
	}
}
