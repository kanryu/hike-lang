package wabt_test

import "testing"

func TestWabtHTTPFuncDeferLIFOAndReturn(t *testing.T) {
	wasm := buildWabt(t, `package main

var trail int

func mark(n int) {
    trail = trail*10 + n
}

func testLifo(earlyRet bool) int {
    defer mark(1)
    defer mark(2)
    defer mark(3)
    mark(0)
    if earlyRet {
        return 42
    }
    return 0
}

func main() int {
    r1 := testLifo(false)
    t1 := trail
    trail = 0
    r2 := testLifo(true)
    t2 := trail
    return r2*1000 + r1*100 + t1 + t2
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42642\n"; got != want {
		t.Fatalf("Wabt defer-return result = %q, want %q", got, want)
	}
}

func TestWabtPanicRecoverInSameFunction(t *testing.T) {
	wasm := buildWabt(t, `package main

var recovered int

func localRecover() {
    defer func() {
        if recover() != nil {
            recovered = 1
        }
    }()
    panic("boom")
}

func main() int {
    localRecover()
    return recovered
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1\n"; got != want {
		t.Fatalf("Wabt same-function panic recovery result = %q, want %q", got, want)
	}
}

func TestWabtAreaStatement(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    area(64) {
        values := make([]int, 4)
        values[0] = 7
    }
    area() {
        values := make([]int, 2)
        values[0] = 3
        area() {
            nested := make([]int, 1)
            nested[0] = 5
        }
    }
    return 9
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=9\n"; got != want {
		t.Fatalf("Wabt area statement result = %q, want %q", got, want)
	}
}

func TestWabtAreaStringDeepCopy(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    first := ""
    second := ""
    area(64) {
        value := string([]byte{'f', 'i', 'r', 's', 't'})
        first = deepcopy(value)
    }
    area(64) {
        value := string([]byte{'s', 'e', 'c', 'o', 'n', 'd'})
        second = deepcopy(value)
    }
    if first == "first" && second == "second" {
        return 1
    }
    return 0
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1\n"; got != want {
		t.Fatalf("Wabt area string deepcopy result = %q, want %q", got, want)
	}
}

func TestWabtDeepCopySlice(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    area(64) {
        source := []int{1, 2, 3}
        copied := deepcopy(source)
        source[0] = 9
        if copied[0] == 1 && len(copied) == 3 && cap(copied) == 3 {
            return 1
        }
    }
    return 0
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1\n"; got != want {
		t.Fatalf("Wabt slice deepcopy result = %q, want %q", got, want)
	}
}

func TestWabtDeepCopyPointerStruct(t *testing.T) {
	wasm := buildWabt(t, `package main

type Item struct {
    value int
    text string
}

func main() int {
    var result *Item
    area(64) {
        item := Item{value: 7, text: string([]byte{'a', 'r', 'e', 'a'})}
        result = deepcopy(&item)
        item.value = 9
    }
    if result.value == 7 {
        return 1
    }
    return 0
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1\n"; got != want {
		t.Fatalf("Wabt pointer struct deepcopy result = %q, want %q", got, want)
	}
}

func TestWabtHTTPFuncEscapedParamsAndReceiver(t *testing.T) {
	wasm := buildWabt(t, `package main

func makeAdder(base int) func(int) int {
    return func(n int) int {
        base = base + n
        return base
    }
}

type Counter struct {
    total int
}

func (c *Counter) MakeStepClosure(step int) func() int {
    return func() int {
        c.total = c.total + step
        return c.total
    }
}

func main() int {
    add := makeAdder(10)
    a1 := add(5)
    a2 := add(5)
    cnt := &Counter{total: 100}
    stepFn := cnt.MakeStepClosure(20)
    s1 := stepFn()
    s2 := stepFn()
    return a1 + a2*10 + s1*100 + s2*1000 + cnt.total*10000
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1552215\n"; got != want {
		t.Fatalf("Wabt escaped-closure result = %q, want %q", got, want)
	}
}

func TestWabtHTTPFuncTypedVariadic(t *testing.T) {
	wasm := buildWabt(t, `package main

func sumAll(prefix int, nums ...int) int {
    total := prefix
    for i := 0; i < len(nums); i = i + 1 {
        total = total + nums[i]
    }
    return total
}

func main() int {
    r0 := sumAll(100)
    r1 := sumAll(10, 1, 2, 3, 4)
    list := []int{10, 20, 30}
    r2 := sumAll(5, list...)
    return r0 + r1*10 + r2*100
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=6800\n"; got != want {
		t.Fatalf("Wabt typed-variadic result = %q, want %q", got, want)
	}
}
