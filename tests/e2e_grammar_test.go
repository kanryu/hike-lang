package e2e_test

import "testing"

// 1. 変数宣言、ポインタ、構造体のフィールドアクセス検証
func TestGrammar_VariablesAndStructPointers(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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

    printf("A=%d,PT=(%d,%d)\n", a, pt.x, pt.y)
    return 0
}
`,
		ExpectedOut:  "A=250,PT=(30,20)",
		ExpectedExit: 0,
	})
}

// 2. 固定長配列の初期化（PR #4 再発防止）・インデックス読み書き検証
func TestGrammar_ArraysAndZeroInit(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // 初期値なし配列（LLVM zeroinitializer の検証）
    var arr [4]int
    z0 := arr[0]
    z3 := arr[3]

    // インデックスへの代入と加算
    arr[1] = 42
    arr[2] = 58
    sum := arr[0] + arr[1] + arr[2] + arr[3]

    printf("Z0=%d,Z3=%d,SUM=%d\n", z0, z3, sum)
    return 0
}
`,
		ExpectedOut:  "Z0=0,Z3=0,SUM=100",
		ExpectedExit: 0,
	})
}

// 3. 関数ポインタ / コールバック呼び出し（間接関数呼び出し）検証
func TestGrammar_FunctionPointers(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Add(a int, b int) int {
    return a + b
}

func Sub(a int, b int) int {
    return a - b
}

func ExecOp(op func(int, int) int, x int, y int) int {
    return op(x, y)
}

func main() int {
    var fn func(int, int) int = Add
    r1 := fn(30, 20)
    r2 := ExecOp(Sub, 30, 20)
    printf("ADD=%d,SUB=%d\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "ADD=50,SUB=10",
		ExpectedExit: 0,
	})
}

// 4. 複合論理演算子と if-else if-else 判定検証
func TestGrammar_ComplexIfConditions(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Check(a int, b int, flag bool) int {
    if (a > 10 && b < 5) || (!flag && a == 0) {
        return 1
    } else if a == 10 && (b >= 5 || flag) {
        return 2
    } else {
        return 3
    }
}

func main() int {
    c1 := Check(15, 3, false)  // branch 1
    c2 := Check(0, 10, false)  // branch 1
    c3 := Check(10, 5, false)  // branch 2
    c4 := Check(2, 2, true)    // branch 3
    printf("C1=%d,C2=%d,C3=%d,C4=%d\n", c1, c2, c3, c4)
    return 0
}
`,
		ExpectedOut:  "C1=1,C2=1,C3=2,C4=3",
		ExpectedExit: 0,
	})
}

// 5. switch-case 文（値マッチング・default 節）検証
func TestGrammar_SwitchStatement(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Eval(x int) int {
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
    v1 := Eval(1)
    v2 := Eval(3)
    vDef := Eval(99)
    printf("V1=%d,V2=%d,DEF=%d\n", v1, v2, vDef)
    return 0
}
`,
		ExpectedOut:  "V1=100,V2=300,DEF=-1",
		ExpectedExit: 0,
	})
}

// 6. for ループ（3節形式、break、continue）検証
func TestGrammar_ForLoopControlFlow(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    sum := 0
    for i := 0; i < 10; i = i + 1 {
        if i % 2 != 0 {
            continue
        }
        if i > 6 {
            break
        }
        sum = sum + i
    }
    printf("SUM=%d\n", sum)
    return 0
}
`,
		ExpectedOut:  "SUM=12", // 0 + 2 + 4 + 6
		ExpectedExit: 0,
	})
}

// 7. for range ループ（配列要素の反復処理）検証
func TestGrammar_ForRangeLoop(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    var nums [5]int
    nums[0] = 10
    nums[1] = 20
    nums[2] = 30
    nums[3] = 40
    nums[4] = 50

    sumIdx := 0
    sumVal := 0
    for idx, val := range nums {
        sumIdx = sumIdx + idx
        sumVal = sumVal + val
    }
    printf("INDEX_SUM=%d,VAL_SUM=%d\n", sumIdx, sumVal)
    return 0
}
`,
		ExpectedOut:  "INDEX_SUM=10,VAL_SUM=150",
		ExpectedExit: 0,
	})
}
