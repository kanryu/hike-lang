package e2e_test

import "testing"

// -------------------------------------------------------------
// FuncCallテスト: lower_call.go網羅的E2Eテスト
// -------------------------------------------------------------

// 1. ジェネリクス関数の明示的型引数適用呼び出し (Monomorphization)
func TestFuncCall_Generics_Monomorphization(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Max[T](a T, b T) T {
    if a > b {
        return a
    }
    return b
}

func main() int {
    i := Max[int](10, 20)
    f := Max[float64](3.14, 1.41)

    printf("INT_MAX=%d,FLOAT_MAX=%.2f\n", i, f)
    return 0
}
`,
		ExpectedOut:  "INT_MAX=20,FLOAT_MAX=3.14",
		ExpectedExit: 0,
	})
}

// 2. デフォルト引数 (Default Arguments) の自動補完検証
func TestFuncCall_DefaultArguments(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Compute(base int, mult int = 2, add int = 5) int {
    return base * mult + add
}

func main() int {
    // 1. 全てのデフォルト引数を省略
    r1 := Compute(10) // 10 * 2 + 5 = 25

    // 2. 末尾のデフォルト引数のみ省略
    r2 := Compute(10, 3) // 10 * 3 + 5 = 35

    // 3. 全引数を明示指定
    r3 := Compute(10, 3, 1) // 10 * 3 + 1 = 31

    printf("R1=%d,R2=%d,R3=%d\n", r1, r2, r3)
    return 0
}
`,
		ExpectedOut:  "R1=25,R2=35,R3=31",
		ExpectedExit: 0,
	})
}

// 3. 型キャスト特殊パス (string <-> cstring, []byte -> string, 文字リテラルキャスト)
func TestFuncCall_TypeCasts_StringAndNumeric(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // 1. string -> cstring -> string 相互変換
    orig := "HelloHike"
    cs := cstring(orig)
    back := string(cs)

    // 2. []byte -> string 変換 (__hike_slice_to_str)
    bytes := []byte{65, 66, 67}
    sFromBytes := string(bytes)

    // 3. 文字リテラルの数値型キャスト
    valA := int('A')
    valZ := int64('Z')

    printf("BACK=%s,BYTES=%s,A=%d,Z=%d\n", back, sFromBytes, valA, valZ)
    return 0
}
`,
		ExpectedOut:  "BACK=HelloHike,BYTES=ABC,A=65,Z=90",
		ExpectedExit: 0,
	})
}

// 4. 組み込み関数 make(3引数), cap, append (grow / nogrow / 複数要素)
func TestFuncCall_Builtins_SliceMakeCapAppend(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // 1. make with len and cap
    s := make([]int, 2, 4)
    s[0] = 10
    s[1] = 20
    len1 := len(s)
    cap1 := cap(s)

    // 2. append within cap (nogrow: ポインタ再利用)
    s = append(s, 30, 40)
    len2 := len(s)
    cap2 := cap(s)

    // 3. append exceeding cap (grow: バッファ再確保)
    s = append(s, 50)
    len3 := len(s)
    cap3 := cap(s)
    hasGrown := 0
    if cap3 > cap2 {
        hasGrown = 1
    }

    printf("L1=%d,C1=%d,L2=%d,C2=%d,L3=%d,GROWN=%d,LAST=%d\n", len1, cap1, len2, cap2, len3, hasGrown, s[4])
    return 0
}
`,
		ExpectedOut:  "L1=2,C1=4,L2=4,C2=4,L3=5,GROWN=1,LAST=50",
		ExpectedExit: 0,
	})
}

// 5. 組み込み関数 delete(map, key) および len(map) 検証
func TestFuncCall_Builtins_MapDeleteLen(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/maps"

func printf(format string, ...) int

func main() int {
    m := make(map[string]int)
    m["alpha"] = 1
    m["beta"] = 2
    m["gamma"] = 3
    lenBefore := len(m)

    delete(m, "beta")
    lenAfter := len(m)

    vAlpha := m["alpha"]
    vBeta := m["beta"]
    vGamma := m["gamma"]

    printf("BEFORE=%d,AFTER=%d,A=%d,B=%d,G=%d\n", lenBefore, lenAfter, vAlpha, vBeta, vGamma)
    return 0
}
`,
		ExpectedOut:  "BEFORE=3,AFTER=2,A=1,B=0,G=3",
		ExpectedExit: 0,
	})
}

// 6. インターフェースの複数メソッド動的ディスパッチ (InstrCallIface / itab)
func TestFuncCall_Interface_DynamicDispatch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Calculator interface {
    Add(n int) int
    Multiply(n int) int
}

type Number struct {
    val int
}

func (n *Number) Add(x int) int {
    n.val = n.val + x
    return n.val
}

func (n *Number) Multiply(x int) int {
    n.val = n.val * x
    return n.val
}

func Operate(c Calculator) (int, int) {
    r1 := c.Add(10)
    r2 := c.Multiply(2)
    return r1, r2
}

func main() int {
    num := &Number{val: 5}
    r1, r2 := Operate(num)

    printf("R1=%d,R2=%d,FINAL=%d\n", r1, r2, num.val)
    return 0
}
`,
		ExpectedOut:  "R1=15,R2=30,FINAL=30",
		ExpectedExit: 0,
	})
}

// 7. メソッド呼び出しにおける値/ポインタの自動相互変換 (Auto-Deref / Auto-Address)
func TestFuncCall_Method_AutoDerefAndAddress(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Point struct {
    x int
    y int
}

// 値レシーバ
func (p Point) Sum() int {
    return p.x + p.y
}

// ポインタレシーバ
func (p *Point) Move(dx int, dy int) {
    p.x = p.x + dx
    p.y = p.y + dy
}

func main() int {
    // 1. ポインタ変数から値レシーバメソッドの呼び出し (Auto-Deref)
    ptr := &Point{x: 10, y: 20}
    s1 := ptr.Sum()

    // 2. 値変数からポインタレシーバメソッドの呼び出し (Auto-Address)
    var val Point
    val.x = 100
    val.y = 200
    val.Move(5, 10)
    s2 := val.Sum()

    printf("S1=%d,S2=%d,VAL=(%d,%d)\n", s1, s2, val.x, val.y)
    return 0
}
`,
		ExpectedOut:  "S1=30,S2=315,VAL=(105,210)",
		ExpectedExit: 0,
	})
}

// 8. 埋め込み構造体 (Embedded Struct) を経由したメソッド解決
func TestFuncCall_Method_EmbeddedStructDispatch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Base struct {
    score int
}

func (b *Base) AddScore(s int) int {
    b.score = b.score + s
    return b.score
}

type Player struct {
    Base
    name string
}

func main() int {
    p := &Player{name: "Hero"}
    p.score = 50
    r := p.AddScore(25)

    printf("NAME=%s,SCORE=%d,RES=%d\n", p.name, p.score, r)
    return 0
}
`,
		ExpectedOut:  "NAME=Hero,SCORE=75,RES=75",
		ExpectedExit: 0,
	})
}

// 9. 可変長引数のスライス展開渡し (args...) および親から子への転送
func TestFuncCall_Variadic_SpreadAndForwarding(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func ConcatAll(items ...string) string {
    res := ""
    for i := 0; i < len(items); i = i + 1 {
        res = res + items[i]
    }
    return res
}

func Forward(first string, rest ...string) string {
    return first + "-" + ConcatAll(rest...)
}

func main() int {
    // 1. スライス展開渡しの直接呼び出し
    words := []string{"foo", "bar", "baz"}
    r1 := ConcatAll(words...)

    // 2. 親関数の可変長引数を子関数へ転送 (rest...)
    r2 := Forward("START", words...)

    printf("R1=%s,R2=%s\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "R1=foobarbaz,R2=START-foobarbaz",
		ExpectedExit: 0,
	})
}

// 10. Cスタイル可変長引数呼び出しにおけるプリミティブ型の自動昇格検証
func TestFuncCall_CVariadic_TypePromotion(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    var b bool = true
    var ch byte = 65 // 'A'
    var f32 float32 = 3.5

    // printf の可変長引数に渡された際、lowerArgs での型昇格キャストが動作する
    printf("BOOL=%d,BYTE=%d,CHAR=%c,F32=%.1f\n", b, ch, ch, f32)
    return 0
}
`,
		ExpectedOut:  "BOOL=1,BYTE=65,CHAR=A,F32=3.5",
		ExpectedExit: 0,
	})
}
