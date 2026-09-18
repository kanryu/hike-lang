package wabt_test

import "testing"

func TestWabtTopLevelFunctionPointerThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func Add(a int, b int) int { return a + b }
func Multiply(a int, b int) int { return a * b }

func Apply(op func(int, int) int, x int, y int) int {
    return op(x, y)
}

func main() int {
    var fn func(int, int) int = Add
    r1 := fn(10, 20)
    fn = Multiply
    r2 := fn(10, 20)
    r3 := Apply(Add, 30, 40)
    r4 := Apply(Multiply, 5, 6)
    return r1 + r2 + r3 + r4
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=330\n"; got != want {
		t.Fatalf("Wabt function-pointer output = %q, want %q", got, want)
	}
}

func TestWabtAnonymousFunctionPointerThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func Exec(f func(int) int, v int) int {
    return f(v)
}

func main() int {
    square := func(x int) int { return x * x }
    return square(7) + Exec(func(n int) int { return n + 100 }, 25)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=174\n"; got != want {
		t.Fatalf("Wabt anonymous function-pointer output = %q, want %q", got, want)
	}
}

func TestWabtBoundMethodPointerThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

type Accumulator struct { total int }

func (a *Accumulator) Add(n int) int {
    a.total = a.total + n
    return a.total
}

func (a *Accumulator) Get() int { return a.total }

func RunOperation(op func(int) int, arg int) int { return op(arg) }

func main() int {
    acc := &Accumulator{total: 100}
    addMethod := acc.Add
    r1 := addMethod(50)
    r2 := addMethod(25)
    r3 := RunOperation(acc.Add, 10)
    getMethod := acc.Get
    current := getMethod()
    return r1 + r2 + r3 + current
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=695\n"; got != want {
		t.Fatalf("Wabt bound-method pointer output = %q, want %q", got, want)
	}
}

func TestWabtCapturedFunctionPointerThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func MakeCounter(start int) func() int {
    count := start
    return func() int {
        count = count + 1
        return count
    }
}

func main() int {
    base := 50
    multiplier := 3
    calc := func(x int) int { return base + x*multiplier }
    counter := MakeCounter(0)
    return calc(10) + counter() + counter() + counter()
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=86\n"; got != want {
		t.Fatalf("Wabt captured function-pointer output = %q, want %q", got, want)
	}
}

func TestWabtStructFunctionFieldCallbackThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

type Button struct {
    onClick func(int) int
}

func main() int {
    button := Button{}
    button.onClick = func(value int) int { return value * 2 }
    return button.onClick(21)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt struct callback output = %q, want %q", got, want)
	}
}

func TestWabtFunctionSliceDispatchThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func Step1(v int) int { return v + 10 }
func Step2(v int) int { return v * 2 }
func Step3(v int) int { return v - 5 }

func main() int {
    pipeline := []func(int) int{Step1, Step2, Step3}
    value := 5
    for i := 0; i < len(pipeline); i = i + 1 {
        fn := pipeline[i]
        value = fn(value)
    }
    return value
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=25\n"; got != want {
		t.Fatalf("Wabt function-slice dispatch output = %q, want %q", got, want)
	}
}
