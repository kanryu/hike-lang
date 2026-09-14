package e2e_test

import "testing"

// -------------------------------------------------------------
// HTTPFuncテスト: LowerFunc網羅的E2Eテスト
// -------------------------------------------------------------

// 1. main関数のargc/argvからos_Argsスライスへの変換およびアクセス検証
func TestHTTPFunc_OsArgs(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

var os_Args []string

func main() int {
    argc := len(os_Args)
    hasArgs := 0
    if argc >= 1 {
        hasArgs = 1
    }
    // 実行ファイル名 (argv[0]) が空でないことを確認
    progLen := 0
    if hasArgs == 1 {
        progLen = len(os_Args[0])
    }
    hasProg := 0
    if progLen > 0 {
        hasProg = 1
    }

    printf("ARGC_GE_1=%d,HAS_PROG=%d\n", hasArgs, hasProg)
    return 0
}
`,
		ExpectedOut:  "ARGC_GE_1=1,HAS_PROG=1",
		ExpectedExit: 0,
	})
}

// 2. defer文の複数登録時におけるLIFO逆順実行と早期リターン時の動作検証
func TestHTTPFunc_Defer_LIFOAndReturn(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

var trail string = ""

func appendTrail(s string) {
    trail = trail + s
}

func testLifo(earlyRet bool) int {
    defer appendTrail("D1-")
    defer appendTrail("D2-")
    defer appendTrail("D3-")

    appendTrail("BODY-")
    if earlyRet {
        return 42
    }
    appendTrail("END-")
    return 0
}

func main() int {
    r1 := testLifo(false)
    t1 := trail
    trail = ""

    r2 := testLifo(true)
    t2 := trail

    printf("R1=%d,T1=%s\nR2=%d,T2=%s\n", r1, t1, r2, t2)
    return 0
}
`,
		ExpectedOut:  "R1=0,T1=BODY-END-D3-D2-D1-\nR2=42,T2=BODY-D3-D2-D1-",
		ExpectedExit: 0,
	})
}

// 3. 無名関数 (FuncLit) スコープ内でのdefer即時実行の検証
func TestHTTPFunc_Defer_InFuncLit(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

var log string = ""

func addLog(s string) {
    log = log + s
}

func printLog() {
    printf("%s\n", log)
}

func main() int {
    defer printLog()
    defer addLog("OUTER_DEF-")
    addLog("START-")

    fn := func() {
        defer addLog("INNER_DEF-")
        addLog("ANON_BODY-")
    }

    fn()
    addLog("AFTER_ANON-")
    return 0
}
`,
		ExpectedOut:  "START-ANON_BODY-INNER_DEF-AFTER_ANON-OUTER_DEF-",
		ExpectedExit: 0,
	})
}

// 4. エスケープ解析による引数およびレシーバのヒープ昇格 (InstrHeapAlloc) 検証
func TestHTTPFunc_EscapedParamsAndReceiver(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

// 引数がクロージャにキャプチャされてヒープ昇格するケース
func makeAdder(base int) func(int) int {
    return func(n int) int {
        base = base + n
        return base
    }
}

type Counter struct {
    total int
}

// レシーバ自身がクロージャにキャプチャされてヒープ昇格するケース
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

    printf("A1=%d,A2=%d,S1=%d,S2=%d,TOTAL=%d\n", a1, a2, s1, s2, cnt.total)
    return 0
}
`,
		ExpectedOut:  "A1=15,A2=20,S1=120,S2=140,TOTAL=140",
		ExpectedExit: 0,
	})
}

// 5. Dual ABI を持つ具象 cfunc の実体呼び出しおよびトランポリン解決の検証
func TestHTTPFunc_CFunc_Trampoline(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

// 手書きブロックを持つ具象 cfunc (__hike_impl_CCompute と CCompute トランポリンを生成)
cfunc CCompute(x int, y int) int {
    diff := x - y
    if diff < 0 {
        return -diff
    }
    return diff
}

func main() int {
    r1 := CCompute(10, 25)
    r2 := CCompute(50, 20)

    printf("R1=%d,R2=%d\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "R1=15,R2=30",
		ExpectedExit: 0,
	})
}

// 6. 型付き可変長引数 (Variadic) の境界動作 (0個、複数個、スライス展開渡し) 検証
func TestHTTPFunc_Variadic_TypedAndSliceSpread(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func SumAll(prefix int, nums ...int) int {
    total := prefix
    for i := 0; i < len(nums); i = i + 1 {
        total = total + nums[i]
    }
    return total
}

func main() int {
    // 1. 0個の可変長引数
    r0 := SumAll(100)

    // 2. 任意個数の実引数
    r1 := SumAll(10, 1, 2, 3, 4)

    // 3. スライスを展開して渡す (args...)
    list := []int{10, 20, 30}
    r2 := SumAll(5, list...)

    printf("R0=%d,R1=%d,R2=%d\n", r0, r1, r2)
    return 0
}
`,
		ExpectedOut:  "R0=100,R1=20,R2=65",
		ExpectedExit: 0,
	})
}

// 7. 無名関数リテラル (FuncLit) における可変長引数の受領検証
func TestHTTPFunc_Variadic_FuncLit(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    joiner := func(base int, mults ...int) int {
        acc := base
        for i := 0; i < len(mults); i = i + 1 {
            acc = acc * mults[i]
        }
        return acc
    }

    r1 := joiner(5)
    r2 := joiner(2, 3, 4)

    parts := []int{2, 5}
    r3 := joiner(10, parts...)

    printf("R1=%d,R2=%d,R3=%d\n", r1, r2, r3)
    return 0
}
`,
		ExpectedOut:  "R1=5,R2=24,R3=100",
		ExpectedExit: 0,
	})
}

// 8. 多段ネストクロージャにおける祖父スコープ変数のキャプチャチェイン検証
func TestHTTPFunc_NestedClosure_CaptureChain(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func BuildChain(a int) func(int) func(int) int {
    return func(b int) func(int) int {
        return func(c int) int {
            a = a + 1
            return a + b + c
        }
    }
}

func main() int {
    level1 := BuildChain(10)
    level2 := level1(20)

    r1 := level2(30) // a=11 -> 11 + 20 + 30 = 61
    r2 := level2(5)  // a=12 -> 12 + 20 + 5  = 37

    printf("R1=%d,R2=%d\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "R1=61,R2=37",
		ExpectedExit: 0,
	})
}

// 9. 構造体ポインタおよびスライスを環境キャプチャしたクロージャの変異反映検証
func TestHTTPFunc_Closure_CaptureComplexTypes(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Node struct {
    value int
}

func main() int {
    node := &Node{value: 42}
    vals := []int{1, 2, 3}

    mutator := func(extra int) {
        node.value = node.value + extra
        vals[0] = vals[0] + extra
    }

    mutator(10)
    printf("NODE=%d,VAL0=%d\n", node.value, vals[0])
    return 0
}
`,
		ExpectedOut:  "NODE=52,VAL0=11",
		ExpectedExit: 0,
	})
}

// 10. 明示的return省略（フォールスルー）時におけるゼロ値の自動補完検証
func TestHTTPFunc_ImplicitDefaultReturn(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type State struct {
    code int
    ok   bool
}

// return文なしでブロック末尾へ到達する関数
func GetZeroInt() int {
}

func GetZeroState() State {
}

func GetZeroSlice() []int {
}

func main() int {
    zInt := GetZeroInt()
    zState := GetZeroState()
    zSlice := GetZeroSlice()

    printf("INT=%d,ST=(%d,%d),SL_LEN=%d\n", zInt, zState.code, zState.ok, len(zSlice))
    return 0
}
`,
		ExpectedOut:  "INT=0,ST=(0,0),SL_LEN=0",
		ExpectedExit: 0,
	})
}
