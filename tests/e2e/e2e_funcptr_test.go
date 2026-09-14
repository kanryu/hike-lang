package e2e_test

import "testing"

// -------------------------------------------------------------
// 関数ポインタ（Function Pointer）運用 E2E テスト
// -------------------------------------------------------------

// 1. 通常のトップレベル関数のポインタ代入および高階関数呼び出し
func TestFuncPtr_TopLevelFunction(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Add(a int, b int) int {
    return a + b
}

func Multiply(a int, b int) int {
    return a * b
}

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

    printf("R1=%d,R2=%d,R3=%d,R4=%d\n", r1, r2, r3, r4)
    return 0
}
`,
		ExpectedOut:  "R1=30,R2=200,R3=70,R4=30",
		ExpectedExit: 0,
	})
}

// 2. キャプチャを持たない純粋な無名関数ポインタの代入・呼び出し
func TestFuncPtr_AnonymousFunction(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Exec(f func(int) int, v int) int {
    return f(v)
}

func main() int {
    square := func(x int) int {
        return x * x
    }
    r1 := square(7)
    r2 := Exec(func(n int) int { return n + 100 }, 25)

    printf("SQUARE=%d,EXEC=%d\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "SQUARE=49,EXEC=125",
		ExpectedExit: 0,
	})
}

// 3. 外部環境（ローカル変数）をキャプチャするクロージャのポインタ呼び出し
func TestFuncPtr_ClosureWithCapture(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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
    calc := func(x int) int {
        return base + x * multiplier
    }
    r1 := calc(10) // 50 + 10 * 3 = 80

    c1 := MakeCounter(0)
    v1 := c1()
    v2 := c1()
    v3 := c1()

    printf("CALC=%d,CTR=(%d,%d,%d)\n", r1, v1, v2, v3)
    return 0
}
`,
		ExpectedOut:  "CALC=80,CTR=(1,2,3)",
		ExpectedExit: 0,
	})
}

// 4. 構造体のレシーバを束縛したバウンドメソッドポインタ呼び出し
func TestFuncPtr_BoundMethodReceiver(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Accumulator struct {
    total int
}

func (a *Accumulator) Add(n int) int {
    a.total = a.total + n
    return a.total
}

func (a *Accumulator) Get() int {
    return a.total
}

func RunOperation(op func(int) int, arg int) int {
    return op(arg)
}

func main() int {
    acc := &Accumulator{total: 100}

    // メソッド値（バウンドメソッド）の変数代入
    addMethod := acc.Add
    r1 := addMethod(50)
    r2 := addMethod(25)

    // 引数として渡すバウンドメソッド呼び出し
    r3 := RunOperation(acc.Add, 10)

    getMethod := acc.Get
    current := getMethod()

    printf("R1=%d,R2=%d,R3=%d,TOTAL=%d\n", r1, r2, r3, current)
    return 0
}
`,
		ExpectedOut:  "R1=150,R2=175,R3=185,TOTAL=185",
		ExpectedExit: 0,
	})
}

// 5. 構造体フィールドに関数ポインタを保持させたコールバックの実行
func TestFuncPtr_StructFieldCallback(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Button struct {
    name    string
    onClick func(string) int
}

func handleClick(btnName string) int {
    printf("CLICKED: %s\n", btnName)
    return 1
}

func main() int {
    var btn Button
    btn.name = "Submit"
    btn.onClick = handleClick

    res := btn.onClick(btn.name)
    printf("RES=%d\n", res)
    return 0
}
`,
		ExpectedOut:  "CLICKED: Submit\nRES=1",
		ExpectedExit: 0,
	})
}

// 6. 関数ポインタのスライス格納とループによる連続ディスパッチ
func TestFuncPtr_FunctionSliceDispatch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Step1(v int) int { return v + 10 }
func Step2(v int) int { return v * 2 }
func Step3(v int) int { return v - 5 }

func main() int {
    pipeline := []func(int) int{Step1, Step2, Step3}

    val := 5
    for i := 0; i < len(pipeline); i = i + 1 {
        fn := pipeline[i]
        val = fn(val)
    }
    printf("PIPELINE_RES=%d\n", val)
    return 0
}
`,
		ExpectedOut:  "PIPELINE_RES=25",
		ExpectedExit: 0,
	})
}

// 7. 正規表現置換 (ReplaceAllStringFunc) における無名関数ポインタ渡し
func TestFuncPtr_Regexp_AnonymousFunc(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/regexp"

func printf(format string, ...) int

func main() int {
    re := regexp.MustCompile("[0-9]+")
    src := "item10_count25"
    res := re.ReplaceAllStringFunc(src, func(s string) string {
        return "[" + s + "]"
    })
    printf("RES=%s\n", res)
    return 0
}
`,
		ExpectedOut:  "RES=item[10]_count[25]",
		ExpectedExit: 0,
	})
}

// 8. 正規表現置換 (ReplaceAllStringFunc) におけるクロージャ（キャプチャあり）渡し
func TestFuncPtr_Regexp_ClosureCapture(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/regexp"

func printf(format string, ...) int

func main() int {
    re := regexp.MustCompile("[a-z]")
    src := "a-b-c"
    count := 0
    res := re.ReplaceAllStringFunc(src, func(s string) string {
        count = count + 1
        if count == 1 {
            return "FIRST"
        }
        if count == 2 {
            return "SECOND"
        }
        return "THIRD"
    })
    printf("RES=%s,COUNT=%d\n", res, count)
    return 0
}
`,
		ExpectedOut:  "RES=FIRST-SECOND-THIRD,COUNT=3",
		ExpectedExit: 0,
	})
}

// 9. 正規表現置換 (ReplaceAllStringFunc) におけるトップレベル関数ポインタ渡し
func TestFuncPtr_Regexp_TopLevelFunc(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/regexp"
import "std/strings"

func printf(format string, ...) int

func Wrap(s string) string {
    return "<" + s + ">"
}

func main() int {
    re := regexp.MustCompile("[a-z]+")
    src := "hello world"

    r1 := re.ReplaceAllStringFunc(src, Wrap)
    r2 := re.ReplaceAllStringFunc(src, strings.ToUpper)

    printf("R1=%s,R2=%s\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "R1=<hello> <world>,R2=HELLO WORLD",
		ExpectedExit: 0,
	})
}
