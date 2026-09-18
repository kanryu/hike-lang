package wabt_test

import "testing"

func TestWabtGrammarVariablesAndStructPointers(t *testing.T) {
	wasm := buildWabt(t, `package main

type Point struct {
    x int
    y int
}

func main() int {
    var a int = 100
    p := &a
    *p = 250
    var pt Point
    pt.x = 10
    pt.y = 20
    ppt := &pt
    ppt.x = 30
    return a + pt.x*10 + pt.y
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=570\n"; got != want {
		t.Fatalf("Wabt struct-pointer result = %q, want %q", got, want)
	}
}

func TestWabtGrammarArraysAndZeroInit(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    var arr [4]int
    z0 := arr[0]
    z3 := arr[3]
    arr[1] = 42
    arr[2] = 58
    sum := arr[0] + arr[1] + arr[2] + arr[3]
    return z0 + z3 + sum
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=100\n"; got != want {
		t.Fatalf("Wabt zero-array result = %q, want %q", got, want)
	}
}

func TestWabtGrammarComplexIfConditions(t *testing.T) {
	wasm := buildWabt(t, `package main

func check(a int, b int, flag bool) int {
    if (a > 10 && b < 5) || (!flag && a == 0) {
        return 1
    } else if a == 10 && (b >= 5 || flag) {
        return 2
    }
    return 3
}

func main() int {
    c1 := check(15, 3, false)
    c2 := check(0, 10, false)
    c3 := check(10, 5, false)
    c4 := check(2, 2, true)
    return c1*1000 + c2*100 + c3*10 + c4
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=1123\n"; got != want {
		t.Fatalf("Wabt complex-if result = %q, want %q", got, want)
	}
}

func TestWabtGrammarSwitchStatement(t *testing.T) {
	wasm := buildWabt(t, `package main

func eval(x int) int {
    switch x {
    case 1:
        return 100
    case 2:
        return 200
    case 3:
        return 300
    default:
        return -1
    }
}

func main() int {
    return eval(1) + eval(3) + eval(99)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=399\n"; got != want {
		t.Fatalf("Wabt switch result = %q, want %q", got, want)
	}
}

func TestWabtGrammarMultiAssignAndBlankIdentifier(t *testing.T) {
	wasm := buildWabt(t, `package main

type Coords struct {
    x int
    y int
}

func getPair() (int, int) {
    return 10, 20
}

func main() int {
    a, b := getPair()
    x, _ := getPair()
    _, y := getPair()
    var c Coords
    c.x, c.y = getPair()
    arr := []int{0, 0}
    arr[0], arr[1] = getPair()
    return a + b + x + y + c.x + c.y + arr[0] + arr[1]
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=120\n"; got != want {
		t.Fatalf("Wabt multi-assignment result = %q, want %q", got, want)
	}
}

func TestWabtGrammarCompoundAssignComplexLValue(t *testing.T) {
	wasm := buildWabt(t, `package main

type Stats struct {
    count int
}

func main() int {
    var st Stats
    st.count = 10
    st.count += 5
    st.count++
    arr := []int{20, 30}
    arr[1] -= 5
    arr[1]--
    arr[0] *= 2
    val := 4
    p := &val
    *p *= 3
    return st.count*10000 + arr[0]*100 + arr[1]*10 + val
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=164252\n"; got != want {
		t.Fatalf("Wabt complex-lvalue result = %q, want %q", got, want)
	}
}

func TestWabtGrammarNestedLoopControlFlow(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    n := 0
    whileSum := 0
    for n < 5 {
        whileSum = whileSum + n
        n = n + 1
    }
    hits := 0
    for i := 0; i < 3; i = i + 1 {
        for j := 0; j < 5; j = j + 1 {
            if j == 1 {
                continue
            }
            if j == 3 {
                break
            }
            hits = hits + 1
        }
    }
    return whileSum*10 + hits
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=106\n"; got != want {
		t.Fatalf("Wabt nested-loop result = %q, want %q", got, want)
	}
}

func TestWabtGrammarReturnTupleForwarding(t *testing.T) {
	wasm := buildWabt(t, `package main

func origin() (int, int) {
    return 42, 7
}

func forward() (int, int) {
    return origin()
}

func main() int {
    num, extra := forward()
    return num*100 + extra
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=4207\n"; got != want {
		t.Fatalf("Wabt tuple-forwarding result = %q, want %q", got, want)
	}
}

func TestWabtGrammarIfWithInitStatement(t *testing.T) {
	wasm := buildWabt(t, `package main

func compute(n int) int {
    return n * 3
}

func main() int {
    r1 := 0
    if v := compute(10); v > 20 {
        r1 = v
    } else {
        r1 = -1
    }
    r2 := 0
    if v := compute(3); v > 20 {
        r2 = v
    } else {
        r2 = v + 100
    }
    return r1*1000 + r2
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=30109\n"; got != want {
		t.Fatalf("Wabt if-init result = %q, want %q", got, want)
	}
}

func TestWabtGrammarForRangeSlice(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    items := []int{10, 20, 30}
    valSum := 0
    for _, v := range items {
        valSum = valSum + v
    }
    return valSum
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=60\n"; got != want {
		t.Fatalf("Wabt slice-range result = %q, want %q", got, want)
	}
}

func TestWabtGrammarAdvancedSwitch(t *testing.T) {
	wasm := buildWabt(t, `package main

func evalMulti(x int) int {
    switch x {
    case 1, 3, 5:
        return 10
    case 2, 4, 6:
        return 20
    default:
        return 99
    }
}

func evalStr(s string) int {
    switch s {
    case "red", "crimson":
        return 1
    case "blue", "navy":
        return 2
    default:
        return 0
    }
}

func main() int {
    m1 := evalMulti(3)
    m2 := evalMulti(4)
    m3 := evalMulti(9)
    s1 := evalStr("crimson")
    s2 := evalStr("blue")
    s3 := evalStr("green")
    loopCount := 0
    for i := 0; i < 3; i = i + 1 {
        switch i {
        case 1:
            break
        }
        loopCount = loopCount + 1
    }
    return loopCount*1000000 + s2*100000 + s1*10000 + m3*100 + m2*10 + m1 + s3
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=3220110\n"; got != want {
		t.Fatalf("Wabt advanced-switch result = %q, want %q", got, want)
	}
}

func TestWabtGrammarTypeSwitch(t *testing.T) {
	wasm := buildWabt(t, `package main

func checkType(val any) int {
    switch v := val.(type) {
    case int:
        return v + 10
    case string:
        return len(v)
    default:
        return -1
    }
}

func main() int {
    var a any = 50
    var b any = "HikeLang"
    var c any = true
    r1 := checkType(a)
    r2 := checkType(b)
    r3 := checkType(c)
    return r1*1000 + r2*10 - r3
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=60081\n"; got != want {
		t.Fatalf("Wabt type-switch result = %q, want %q", got, want)
	}
}

func TestWabtGrammarChannelSendReceive(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    ch := make(chan int, 2)
    ch <- 100
    ch <- 200
    r1 := <-ch
    r2 := <-ch
    return r1 + r2
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=300\n"; got != want {
		t.Fatalf("Wabt channel result = %q, want %q", got, want)
	}
}
