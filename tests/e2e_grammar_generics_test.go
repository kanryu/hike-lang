package e2e_test

import "testing"

// -------------------------------------------------------------
// 1. 演算子 (Operators & Expressions)
// -------------------------------------------------------------

// 四則演算・剰余・演算子優先順位の検証
func TestGrammar_Arithmetic_Precedence(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    a := 10 + 20 * 3 - 5 / 2 % 3
    printf("RES=%d\n", a)
    return 0
}
`,
		ExpectedOut:  "RES=68",
		ExpectedExit: 0,
	})
}

// ビット演算 (AND, OR, XOR, シフト演算) の検証
func TestGrammar_Bitwise_Operations(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    a := 5  // 0101
    b := 3  // 0011
    andVal := a & b
    orVal := a | b
    xorVal := a ^ b
    shlVal := a << 2
    shrVal := a >> 1
    printf("AND=%d,OR=%d,XOR=%d,SHL=%d,SHR=%d\n", andVal, orVal, xorVal, shlVal, shrVal)
    return 0
}
`,
		ExpectedOut:  "AND=1,OR=7,XOR=6,SHL=20,SHR=2",
		ExpectedExit: 0,
	})
}

// 複合代入演算子およびインクリメント・デクリメントの検証
func TestGrammar_Compound_Assignment(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    x := 10
    x += 5
    x -= 2
    x *= 3
    x++
    x--
    printf("X=%d\n", x)
    return 0
}
`,
		ExpectedOut:  "X=39",
		ExpectedExit: 0,
	})
}

// 比較演算および論理演算 (短絡評価含む) の検証
func TestGrammar_Logical_And_Comparison(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    tVal := !(true && false) || (10 > 20)
    fVal := (5 >= 10) && (100 == 100)
    neq := 10 != 20
    printf("T=%d,F=%d,NEQ=%d\n", tVal, fVal, neq)
    return 0
}
`,
		ExpectedOut:  "T=1,F=0,NEQ=1",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 2. 変数宣言と代入 (Variables & Declarations)
// -------------------------------------------------------------

// 多重宣言・多重代入・短縮代入の検証
func TestGrammar_Var_Multiple_Assignment(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    var a, b int = 10, 20
    a, b = b, a
    x, y := 30, 40
    printf("A=%d,B=%d,X=%d,Y=%d\n", a, b, x, y)
    return 0
}
`,
		ExpectedOut:  "A=20,B=10,X=30,Y=40",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 3. 制御構文 (Control Structures)
// -------------------------------------------------------------

// 初期化文付き if-else 構文の分岐検証
func TestGrammar_If_Else_Init(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    if v := 15; v > 20 {
        printf("HIGH\n")
    } else if v > 10 {
        printf("MID=%d\n", v)
    } else {
        printf("LOW\n")
    }
    return 0
}
`,
		ExpectedOut:  "MID=15",
		ExpectedExit: 0,
	})
}

// 伝統的な 3 節 for ループ (break / continue 含む) の検証
func TestGrammar_For_Loop_Control(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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
    printf("SUM=%d\n", sum)
    return 0
}
`,
		ExpectedOut:  "SUM=18",
		ExpectedExit: 0,
	})
}

// 条件のみの for ループ (while 形式) の検証
func TestGrammar_For_While_Style(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    n := 1
    for n < 16 {
        n = n * 2
    }
    printf("N=%d\n", n)
    return 0
}
`,
		ExpectedOut:  "N=16",
		ExpectedExit: 0,
	})
}

// 複数条件値および default を持つ switch 文の検証
func TestGrammar_Switch_Multiple_Cases(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    val := 2
    switch val {
    case 1:
        printf("ONE\n")
    case 2, 3:
        printf("TWO_OR_THREE\n")
    default:
        printf("OTHER\n")
    }
    return 0
}
`,
		ExpectedOut:  "TWO_OR_THREE",
		ExpectedExit: 0,
	})
}

// 条件式なし switch (if-else 代替形式) の検証
func TestGrammar_Switch_Conditionless(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    score := 85
    switch {
    case score >= 90:
        printf("GRADE=A\n")
    case score >= 80:
        printf("GRADE=B\n")
    default:
        printf("GRADE=C\n")
    }
    return 0
}
`,
		ExpectedOut:  "GRADE=B",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 4. 関数・再帰・クロージャ・defer (Functions & Flow)
// -------------------------------------------------------------

// 関数からの複数戻り値の返却とアンパックの検証
func TestGrammar_Func_Multiple_Returns(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func swap(a int, b int) (int, int) {
    return b, a
}

func main() int {
    x, y := swap(100, 200)
    printf("X=%d,Y=%d\n", x, y)
    return 0
}
`,
		ExpectedOut:  "X=200,Y=100",
		ExpectedExit: 0,
	})
}

// 再帰関数 (フィボナッチ数列計算) のスタック巻き戻し検証
func TestGrammar_Func_Recursion(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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
    printf("FIB(10)=%d\n", fib(10))
    return 0
}
`,
		ExpectedOut:  "FIB(10)=55",
		ExpectedExit: 0,
	})
}

// クロージャによる外部ローカルスコープ変数のキャプチャと状態維持の検証
func TestGrammar_Func_Closure_State(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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
    printf("CNT=%d,%d\n", v1, v2)
    return 0
}
`,
		ExpectedOut:  "CNT=11,12",
		ExpectedExit: 0,
	})
}

// defer 文の遅延評価および LIFO (後入れ先出し) 順序の実行検証
func TestGrammar_Defer_LIFO_Execution(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func printNum(n int) {
    printf("%d ", n)
}

func testDefer() {
    defer printNum(1)
    defer printNum(2)
    defer printNum(3)
    printf("START ")
}

func main() int {
    testDefer()
    printf("END\n")
    return 0
}
`,
		ExpectedOut:  "START 3 2 1 END",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 5. ポインタ操作 (Pointers)
// -------------------------------------------------------------

// ポインタ参照・デリファレンスと直接値書き換えの検証
func TestGrammar_Pointer_Mutation(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func mutate(p *int) {
    *p = *p + 50
}

func main() int {
    x := 10
    mutate(&x)
    printf("X=%d\n", x)
    return 0
}
`,
		ExpectedOut:  "X=60",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 6. 構造体・メソッド・埋め込み (Structs, Methods & Embedding)
// -------------------------------------------------------------

// 構造体の値レシーバメソッドの呼び出し検証
func TestGrammar_Struct_Value_Receiver(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Point struct {
    X int
    Y int
}

func (p Point) Sum() int {
    return p.X + p.Y
}

func main() int {
    pt := Point{X: 12, Y: 34}
    printf("SUM=%d\n", pt.Sum())
    return 0
}
`,
		ExpectedOut:  "SUM=46",
		ExpectedExit: 0,
	})
}

// 構造体のポインタレシーバメソッドによる内部ミューテーションの検証
func TestGrammar_Struct_Pointer_Receiver(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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
    printf("VAL=%d\n", c.Get())
    return 0
}
`,
		ExpectedOut:  "VAL=125",
		ExpectedExit: 0,
	})
}

// 構造体埋め込みによるフィールドの昇格 (Field Promotion) の検証
func TestGrammar_Struct_Embedding_Promotion(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

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
    printf("ID=%d,PRICE=%d\n", item.Id, item.Price)
    return 0
}
`,
		ExpectedOut:  "ID=777,PRICE=500",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 7. 配列・スライス・for-range (Arrays, Slices & Ranges)
// -------------------------------------------------------------

// 固定長配列の宣言・初期化およびインデックスアクセスの検証
func TestGrammar_Array_Fixed_Access(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    arr := [3]int{100, 200, 300}
    arr[1] = 999
    printf("A0=%d,A1=%d,A2=%d\n", arr[0], arr[1], arr[2])
    return 0
}
`,
		ExpectedOut:  "A0=100,A1=999,A2=300",
		ExpectedExit: 0,
	})
}

// スライスの make、append による容量拡張、len/cap の検証
func TestGrammar_Slice_Make_Append(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    s := make([]int, 0, 4)
    s = append(s, 10, 20, 30)
    printf("LEN=%d,CAP=%d,AT1=%d\n", len(s), cap(s), s[1])
    return 0
}
`,
		ExpectedOut:  "LEN=3,CAP=4,AT1=20",
		ExpectedExit: 0,
	})
}

// 配列からの部分スライス切り出し操作の検証
func TestGrammar_Array_Subslice(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    arr := [5]int{10, 20, 30, 40, 50}
    sl := arr[1:4]
    printf("LEN=%d,V0=%d,V2=%d\n", len(sl), sl[0], sl[2])
    return 0
}
`,
		ExpectedOut:  "LEN=3,V0=20,V2=40",
		ExpectedExit: 0,
	})
}

// for-range 構文による配列走査 (キー・バリュー取得) の検証
func TestGrammar_For_Range_Iteration(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    arr := [3]string{"A", "B", "C"}
    for i, v := range arr {
        printf("%d:%s ", i, v)
    }
    printf("\n")
    return 0
}
`,
		ExpectedOut:  "0:A 1:B 2:C",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 8. 文字列 (Strings)
// -------------------------------------------------------------

// 文字列の len 取得および文字インデックスアクセスの検証
func TestGrammar_String_Len_And_Index(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    str := "HikeLang"
    printf("LEN=%d,C0=%c,C4=%c\n", len(str), str[0], str[4])
    return 0
}
`,
		ExpectedOut:  "LEN=8,C0=H,C4=L",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 9. 型キャスト (Type Casting)
// -------------------------------------------------------------

// 整数型と浮動小数点数型の相互キャスト検証
func TestGrammar_Type_Casting_Numeric(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    f := 12.85
    i := int(f)
    f2 := float64(i) + 0.5
    printf("INT=%d,FLOAT=%.1f\n", i, f2)
    return 0
}
`,
		ExpectedOut:  "INT=12,FLOAT=12.5",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 10. インターフェース・型アサーション (Interfaces & Type Assertion)
// -------------------------------------------------------------

// インターフェース経由の動的ディスパッチ呼び出しの検証
func TestGrammar_Interface_Dynamic_Dispatch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Greeter interface {
    Greet() string
}

type Robot struct {
    Model string
}

func (r *Robot) Greet() string {
    return r.Model
}

func main() int {
    var g Greeter = &Robot{Model: "RX-78"}
    printf("GREET=%s\n", g.Greet())
    return 0
}
`,
		ExpectedOut:  "GREET=RX-78",
		ExpectedExit: 0,
	})
}

// any 型からの型アサーション (値および ok フラグ) の検証
func TestGrammar_Type_Assertion_Any(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    var a any = 1234
    val, ok := a.(int)
    printf("VAL=%d,OK=%d\n", val, ok)
    return 0
}
`,
		ExpectedOut:  "VAL=1234,OK=1",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 11. プロセス終了ステータス (Process Exit Code)
// -------------------------------------------------------------

// main 関数の戻り値による終了コード伝播の検証
func TestGrammar_Process_Exit_Code(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func main() int {
    return 42
}
`,
		ExpectedOut:  "",
		ExpectedExit: 42,
	})
}
