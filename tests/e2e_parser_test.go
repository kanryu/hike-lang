package e2e_test

import "testing"

// -------------------------------------------------------------
// Parserテスト: parser.go網羅的E2Eテスト
// -------------------------------------------------------------

// 1. const (...) グループ宣言における前行式引き継ぎおよび iota 式展開のパース検証
func TestParser_Const_GroupAndIotaExprInheritance(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

const (
    Zero = iota
    One
    Two
    Step = 100
    Next
)

func main() int {
    printf("Z=%d,O=%d,T=%d,S=%d,N=%d\n", Zero, One, Two, Step, Next)
    return 0
}
`,
		ExpectedOut:  "Z=0,O=1,T=2,S=100,N=100",
		ExpectedExit: 0,
	})
}

// 2. import (...) 括弧グループ形式による複数モジュールインポートのパース検証
func TestParser_Import_GroupSyntax(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import (
    "std/maps"
)

func printf(format string, ...) int

func main() int {
    m := make(map[string]int)
    m["key"] = 42
    printf("MAP_VAL=%d\n", m["key"])
    return 0
}
`,
		ExpectedOut:  "MAP_VAL=42",
		ExpectedExit: 0,
	})
}

// 3. 多値 var 宣言（型指定初期化および型推論付き多値定義）のパース検証
func TestParser_Var_MultipleIdentifiers(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // 1. var a, b int（未初期化の複数変数同時宣言）
    var a, b int
    a = 10
    b = 20

    // 2. var x, y = 30, 40（型推論を伴う多値変数定義宣言）
    var x, y = 30, 40

    printf("A=%d,B=%d,X=%d,Y=%d\n", a, b, x, y)
    return 0
}
`,
		ExpectedOut:  "A=10,B=20,X=30,Y=40",
		ExpectedExit: 0,
	})
}

// 4. 3節 for ループの初期化節・後置節省略および while 風条件節のみループのパース検証
func TestParser_For_OmittedClauses(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // 1. 初期化節省略: for ; i < 3; i++
    i := 0
    sum1 := 0
    for ; i < 3; i++ {
        sum1 = sum1 + i
    }

    // 2. 後置節省略: for j := 0; j < 3;
    sum2 := 0
    for j := 0; j < 3; {
        sum2 = sum2 + j
        j++
    }

    // 3. 条件節のみ (whileスタイル): for k < 3
    k := 0
    sum3 := 0
    for k < 3 {
        sum3 = sum3 + k
        k++
    }

    printf("SUM1=%d,SUM2=%d,SUM3=%d\n", sum1, sum2, sum3)
    return 0
}
`,
		ExpectedOut:  "SUM1=3,SUM2=3,SUM3=3",
		ExpectedExit: 0,
	})
}

// 5. 受領変数を一切受け取らない純粋な回数走査 for range collection のパース検証
func TestParser_ForRange_BareIteration(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    items := []int{10, 20, 30, 40}
    count := 0

    // キー・値を省略した走査: for range items
    for range items {
        count = count + 1
    }

    printf("COUNT=%d\n", count)
    return 0
}
`,
		ExpectedOut:  "COUNT=4",
		ExpectedExit: 0,
	})
}

// 6. 初期化文付きスイッチ構文および単一 case 節での複数値マッチのパース検証
func TestParser_Switch_InitStatementAndMultiCases(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func evalScore(score int) int {
    res := 0
    // 初期化文付き switch & 複数値マッチ case
    switch adjusted := score + 5; adjusted {
    case 15, 25:
        res = 1
    case 35, 45, 55:
        res = 2
    default:
        res = -1
    }
    return res
}

func main() int {
    r1 := evalScore(10) // adjusted = 15 -> 1
    r2 := evalScore(40) // adjusted = 45 -> 2
    r3 := evalScore(90) // adjusted = 95 -> -1

    printf("R1=%d,R2=%d,R3=%d\n", r1, r2, r3)
    return 0
}
`,
		ExpectedOut:  "R1=1,R2=2,R3=-1",
		ExpectedExit: 0,
	})
}

// 7. 変数束縛なしの型スイッチ構文（switch a.(type)）のパース検証
func TestParser_TypeSwitch_BareAssertion(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func identify(v any) int {
    res := 0
    // 変数束縛を行わない型スイッチ
    switch v.(type) {
    case int:
        res = 10
    case string:
        res = 20
    default:
        res = -1
    }
    return res
}

func main() int {
    r1 := identify(100)
    r2 := identify("Hike")
    r3 := identify(true)

    printf("R1=%d,R2=%d,R3=%d\n", r1, r2, r3)
    return 0
}
`,
		ExpectedOut:  "R1=10,R2=20,R3=-1",
		ExpectedExit: 0,
	})
}

// 8. 構造体におけるフィールド名省略の埋め込みフィールド（値型・ポインタ型）のパース検証
func TestParser_Struct_EmbeddedFields(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Header struct {
    tag int
}

type Footer struct {
    checksum int
}

type Packet struct {
    Header        // 値型埋め込み
    *Footer       // ポインタ型埋め込み
    payload int
}

func main() int {
    ft := &Footer{checksum: 999}
    pkt := &Packet{payload: 42}
    pkt.tag = 101
    pkt.Footer = ft

    printf("TAG=%d,PAYLOAD=%d,CHECK=%d\n", pkt.tag, pkt.payload, pkt.checksum)
    return 0
}
`,
		ExpectedOut:  "TAG=101,PAYLOAD=42,CHECK=999",
		ExpectedExit: 0,
	})
}

// 9. インターフェース定義における引数名省略形式（Method(T1, T2) R）のパース検証
func TestParser_Interface_UnnamedParams(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Transformer interface {
    // 引数名を持たない型名のみのメソッド定義
    Transform(int, int) int
}

type Multiplier struct {
    factor int
}

func (m *Multiplier) Transform(a int, b int) int {
    return (a + b) * m.factor
}

func runTransform(t Transformer, x int, y int) int {
    return t.Transform(x, y)
}

func main() int {
    m := &Multiplier{factor: 3}
    res := runTransform(m, 4, 6) // (4 + 6) * 3 = 30

    printf("TRANS_RES=%d\n", res)
    return 0
}
`,
		ExpectedOut:  "TRANS_RES=30",
		ExpectedExit: 0,
	})
}

// 10. cfunc における外部 C ライブラリシンボル明示バインド構文（= target）のパース検証
func TestParser_CFunc_TargetBinding(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

// ターゲット C 関数名を明示指定した cfunc
cfunc C_Abs(n int) int = abs

func main() int {
    r1 := C_Abs(-42)
    r2 := C_Abs(100)

    printf("R1=%d,R2=%d\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "R1=42,R2=100",
		ExpectedExit: 0,
	})
}
