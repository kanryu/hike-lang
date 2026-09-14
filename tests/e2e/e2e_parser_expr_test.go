package e2e_test

import "testing"

// -------------------------------------------------------------
// ParserExprテスト: parser_expr.go網羅的E2Eテスト
// -------------------------------------------------------------

// 1. 算術演算子とビット演算子の優先順位（SUM vs PRODUCT）および括弧による結合順序制御の検証
func TestParserExpr_Precedence_ArithmeticAndBitwise(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // & (PRODUCT) は | (SUM) より優先順位が高い
    r1 := 1 | 2 & 2     // 1 | (2 & 2) = 1 | 2 = 3
    r2 := (1 | 2) & 2   // (1 | 2) & 2 = 3 & 2 = 2

    // >> (PRODUCT) は + (SUM) より優先順位が高い
    r3 := 20 >> 1 + 1   // (20 >> 1) + 1 = 10 + 1 = 11
    r4 := 20 >> (1 + 1) // 20 >> 2 = 5

    // * (PRODUCT) は + (SUM) より優先順位が高い
    r5 := 2 + 3 * 4     // 2 + 12 = 14
    r6 := (2 + 3) * 4   // 5 * 4 = 20

    printf("R1=%d,R2=%d,R3=%d,R4=%d,R5=%d,R6=%d\n", r1, r2, r3, r4, r5, r6)
    return 0
}
`,
		ExpectedOut:  "R1=3,R2=2,R3=11,R4=5,R5=14,R6=20",
		ExpectedExit: 0,
	})
}

// 2. 比較演算子と論理演算子の優先順位（EQUALS/LESSGREATER vs LAND vs LOR）の検証
func TestParserExpr_Precedence_LogicalAndComparison(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // && (LAND) は || (LOR) より優先順位が高い
    b1 := 0
    if true || false && false {
        b1 = 1 // true || (false && false) = true
    }

    b2 := 0
    if (true || false) && false {
        b2 = 1 // (true) && false = false
    }

    // 比較演算子は && より優先順位が高い
    b3 := 0
    if 5 > 3 && 10 <= 20 {
        b3 = 1 // (5 > 3) && (10 <= 20) = true
    }

    printf("B1=%d,B2=%d,B3=%d\n", b1, b2, b3)
    return 0
}
`,
		ExpectedOut:  "B1=1,B2=0,B3=1",
		ExpectedExit: 0,
	})
}

// 3. 文字リテラル (CharLiteral) における各種エスケープシーケンスとASCIIコードポイントのパース検証
func TestParserExpr_CharLiteral_EscapeSequences(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    cTab := '\t'   // 9
    cNL := '\n'    // 10
    cCR := '\r'    // 13
    cZero := '\0'  // 0
    cQuote := '\'' // 39
    cSlash := '\\' // 92
    cA := 'A'      // 65

    printf("TAB=%d,NL=%d,CR=%d,ZERO=%d,Q=%d,S=%d,A=%d\n", int(cTab), int(cNL), int(cCR), int(cZero), int(cQuote), int(cSlash), int(cA))
    return 0
}
`,
		ExpectedOut:  "TAB=9,NL=10,CR=13,ZERO=0,Q=39,S=92,A=65",
		ExpectedExit: 0,
	})
}

// 4. 文字列に対するスライス式 (SliceExpr) の省略構文（開始省略、終了省略、両方省略）の検証
func TestParserExpr_SliceExpr_OmissionSyntax_String(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    str := "HikeLang"

    // 1. 開始インデックス省略 [:high]
    p1 := str[:4] // "Hike"

    // 2. 終了インデックス省略 [low:]
    p2 := str[4:] // "Lang"

    // 3. 両方省略 [:]
    p3 := str[:]  // "HikeLang"

    printf("P1=%s,P2=%s,P3=%s\n", p1, p2, p3)
    return 0
}
`,
		ExpectedOut:  "P1=Hike,P2=Lang,P3=HikeLang",
		ExpectedExit: 0,
	})
}

// 5. スライスに対するスライス式 (SliceExpr) の省略構文の検証
func TestParserExpr_SliceExpr_OmissionSyntax_Slice(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    arr := []int{10, 20, 30, 40, 50}

    // 1. [:3]
    s1 := arr[:3]

    // 2. [2:]
    s2 := arr[2:]

    // 3. [:]
    s3 := arr[:]

    printf("S1_LEN=%d,S1_END=%d;S2_LEN=%d,S2_START=%d;S3_LEN=%d\n", len(s1), s1[2], len(s2), s2[0], len(s3))
    return 0
}
`,
		ExpectedOut:  "S1_LEN=3,S1_END=30;S2_LEN=3,S2_START=30;S3_LEN=5",
		ExpectedExit: 0,
	})
}

// 6. 即時実行無名関数（IIFE: Immediately Invoked Function Expression）の式パース検証
func TestParserExpr_FuncLit_IIFE(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // 無名関数を式として直接定義し即座に呼び出す (IIFE)
    res := func(a int, b int) int {
        return a*a + b*b
    }(3, 4)

    printf("IIFE_RES=%d\n", res)
    return 0
}
`,
		ExpectedOut:  "IIFE_RES=25",
		ExpectedExit: 0,
	})
}

// 7. メンバアクセス、インデックスアクセス、関数呼び出しの連続チェーン式のパース検証
func TestParserExpr_Chained_MemberAndIndex(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Layer2 struct {
    scores []int
}

type Layer1 struct {
    child *Layer2
}

func getLayer() *Layer1 {
    l2 := &Layer2{scores: []int{100, 200, 300}}
    return &Layer1{child: l2}
}

func main() int {
    // getLayer().child.scores[1] の連続チェーン評価
    val := getLayer().child.scores[1]

    printf("CHAIN_VAL=%d\n", val)
    return 0
}
`,
		ExpectedOut:  "CHAIN_VAL=200",
		ExpectedExit: 0,
	})
}

// 8. チャネル受信式 (<-ch) が二項演算子や条件分岐の式中に直接埋め込まれた場合のパース検証
func TestParserExpr_ReceiveExpr_InExpressions(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    ch := make(chan int, 2)
    ch <- 15
    ch <- 25

    // 式中に埋め込まれた <-ch の二項加算評価
    sum := (<-ch) + (<-ch)

    printf("CHAN_EXPR_SUM=%d\n", sum)
    return 0
}
`,
		ExpectedOut:  "CHAN_EXPR_SUM=40",
		ExpectedExit: 0,
	})
}

// 9. 型アサーション式 (TypeAssertExpr) a.(T) のパースと成否判定の検証
func TestParserExpr_TypeAssertExpr_CommaOk(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    var a any = 777

    // 成功する型アサーション
    v1, ok1 := a.(int)

    // 失敗する型アサーション
    v2, ok2 := a.(string)

    lenV2 := 0
    if ok2 {
        lenV2 = len(v2)
    }

    printf("V1=%d,OK1=%d,V2_LEN=%d,OK2=%d\n", v1, ok1, lenV2, ok2)
    return 0
}
`,
		ExpectedOut:  "V1=777,OK1=1,V2_LEN=0,OK2=0",
		ExpectedExit: 0,
	})
}

// 10. async(...) 式とフューチャ受信式 (<-fut) の式中評価検証
func TestParserExpr_AsyncExpr_Evaluation(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func taskWork(base int, mult int) int {
    return base * mult
}

func main() int {
    // async 式による非同期クロージャ実行
    fut := async(func() int {
        return taskWork(7, 6)
    })

    // フューチャからの同期待ち合わせと結果受領 (<-fut)
    res := <-fut

    printf("ASYNC_EVAL=%d\n", res)
    return 0
}
`,
		ExpectedOut:  "ASYNC_EVAL=42",
		ExpectedExit: 0,
	})
}
