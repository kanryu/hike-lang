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

// 8. 多値アンパック代入とブランク識別子（_）の検証
func TestGrammar_MultiAssignAndBlankIdentifier(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Coords struct {
    x int
    y int
}

func getPair() (int, int) {
    return 10, 20
}

func main() int {
    // 1. 定義代入 (:=) でのアンパックとブランク識別子
    a, b := getPair()
    x, _ := getPair()
    _, y := getPair()

    // 2. 構造体フィールドおよびスライス要素への通常代入 (=)
    var c Coords
    c.x, c.y = getPair()

    arr := []int{0, 0}
    arr[0], arr[1] = getPair()

    printf("A=%d,B=%d,X=%d,Y=%d,C=(%d,%d),ARR=(%d,%d)\n", a, b, x, y, c.x, c.y, arr[0], arr[1])
    return 0
}
`,
		ExpectedOut:  "A=10,B=20,X=10,Y=20,C=(10,20),ARR=(10,20)",
		ExpectedExit: 0,
	})
}

// 9. 複合代入演算子 (+=, -=, *=, ++, --) と多様な左辺値 (構造体、スライス、ポインタ) の検証
func TestGrammar_CompoundAssignComplexLValue(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Stats struct {
    count int
}

func main() int {
    // 構造体フィールドへの複合代入
    var st Stats
    st.count = 10
    st.count += 5
    st.count++

    // スライス要素への複合代入
    arr := []int{20, 30}
    arr[1] -= 5
    arr[1]--
    arr[0] *= 2

    // ポインタデリファレンスへの複合代入
    val := 4
    p := &val
    *p *= 3

    printf("ST=%d,ARR=(%d,%d),VAL=%d\n", st.count, arr[0], arr[1], val)
    return 0
}
`,
		ExpectedOut:  "ST=16,ARR=(40,24),VAL=12",
		ExpectedExit: 0,
	})
}

// 10. 初期化文付き if 文 (if init; cond) のスコープ検証
func TestGrammar_IfWithInitStatement(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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

    printf("R1=%d,R2=%d\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "R1=30,R2=109",
		ExpectedExit: 0,
	})
}

// 11. 条件のみ for ループおよび二重ネストループ内の break / continue 検証
func TestGrammar_ForLoopNestedControlFlow(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // 1. 条件のみループ (while 相当)
    n := 0
    whileSum := 0
    for n < 5 {
        whileSum = whileSum + n
        n = n + 1
    }

    // 2. 二重ネストループにおける内側 break / continue の動作検証
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

    printf("WHILE=%d,HITS=%d\n", whileSum, hits)
    return 0
}
`,
		ExpectedOut:  "WHILE=10,HITS=6",
		ExpectedExit: 0,
	})
}

// 12. for range ループによるスライス、マップ、文字列の走査検証
func TestGrammar_ForRange_SliceMapString(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `package main

import "std/maps"

func printf(format string, ...) int

func main() int {
    // 1. スライスの値のみ走査
    items := []int{10, 20, 30}
    valSum := 0
    for _, v := range items {
        valSum = valSum + v
    }

    // 2. マップのキー・値走査
    m := make(map[string]int)
    m["a"] = 100
    m["b"] = 200
    mapKeyCount := 0
    mapValSum := 0
    for k, v := range m {
        if len(k) > 0 {
            mapKeyCount = mapKeyCount + 1
        }
        mapValSum = mapValSum + v
    }

    // 3. 文字列のインデックス・byte 走査
    str := "Hike"
    byteSum := 0
    for i, b := range str {
        byteSum = byteSum + int(b) + i
    }

    printf("SLICE_VAL=%d,MAP_CNT=%d,MAP_VAL=%d,BYTE_SUM=%d\n", valSum, mapKeyCount, mapValSum, byteSum)
    return 0
}
`,
		ExpectedOut:  "SLICE_VAL=60,MAP_CNT=2,MAP_VAL=300,BYTE_SUM=391",
		ExpectedExit: 0,
	})
}

// 13. switch 文の高度な分岐 (初期化文、カンマ区切り複数条件、文字列比較、switch 内 break) 検証
func TestGrammar_SwitchAdvanced(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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

    // ループ内の switch からの break（外側ループは継続する）
    loopCount := 0
    for i := 0; i < 3; i = i + 1 {
        switch i {
        case 1:
            break
        }
        loopCount = loopCount + 1
    }

    printf("M=(%d,%d,%d),S=(%d,%d,%d),LOOP=%d\n", m1, m2, m3, s1, s2, s3, loopCount)
    return 0
}
`,
		ExpectedOut:  "M=(10,20,99),S=(1,2,0),LOOP=3",
		ExpectedExit: 0,
	})
}

// 14. type switch 文 (型スイッチ) によるインターフェース型判定検証
func TestGrammar_TypeSwitch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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

    printf("R1=%d,R2=%d,R3=%d\n", r1, r2, r3)
    return 0
}
`,
		ExpectedOut:  "R1=60,R2=8,R3=-1",
		ExpectedExit: 0,
	})
}

// 15. 複数戻り値のタプル一括返却 (return fn()) 検証
func TestGrammar_ReturnTupleForwarding(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Origin() (int, string) {
    return 42, "ReturnedTuple"
}

func Forward() (int, string) {
    return Origin()
}

func main() int {
    num, text := Forward()
    printf("NUM=%d,TEXT=%s\n", num, text)
    return 0
}
`,
		ExpectedOut:  "NUM=42,TEXT=ReturnedTuple",
		ExpectedExit: 0,
	})
}

// 16. チャネル送信文 (SendStmt: ch <- val) と受信の検証
func TestGrammar_ChannelSendReceive(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    ch := make(chan int, 2)
    ch <- 100
    ch <- 200

    r1 := <-ch
    r2 := <-ch

    printf("R1=%d,R2=%d\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "R1=100,R2=200",
		ExpectedExit: 0,
	})
}
