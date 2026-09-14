package e2e_test

import "testing"

// -------------------------------------------------------------
// Lowerテスト: lower.go網羅的E2Eテスト
// -------------------------------------------------------------

// 1. パッケージレベルグローバル変数の初期化式（関数呼び出し・式評価）がmain先頭で先行実行されるかの検証
func TestLower_GlobalVarInitializers(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func computeInitialVal() int {
    return 100
}

var gCount int = computeInitialVal() + 23
var gMessage string = "HelloGlobal"

func main() int {
    printf("COUNT=%d,MSG=%s\n", gCount, gMessage)
    return 0
}
`,
		ExpectedOut:  "COUNT=123,MSG=HelloGlobal",
		ExpectedExit: 0,
	})
}

// 2. 複数のグローバル変数初期化における宣言順の先行評価検証
func TestLower_MultipleGlobalInitsOrder(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

var gA int = 10
var gB int = gA * 2
var gC int = gB + 5

func main() int {
    printf("GA=%d,GB=%d,GC=%d\n", gA, gB, gC)
    return 0
}
`,
		ExpectedOut:  "GA=10,GB=20,GC=25",
		ExpectedExit: 0,
	})
}

// 3. 非空インターフェースからanyへの再ボクシング（itabからtypeID抽出・再パッキング）検証
func TestLower_NonEmptyInterfaceToAny(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Greeter interface {
    Greet() string
}

type Person struct {
    name string
}

func (p *Person) Greet() string {
    return "Hi," + p.name
}

func checkAny(a any) int {
    switch v := a.(type) {
    case *Person:
        return len(v.name)
    default:
        return -1
    }
}

func main() int {
    p := &Person{name: "Alice"}
    var g Greeter = p
    var a any = g // 非空インターフェース -> any への再ボクシングパス
    r := checkAny(a)
    printf("LEN=%d\n", r)
    return 0
}
`,
		ExpectedOut:  "LEN=5",
		ExpectedExit: 0,
	})
}

// 4. 非ポインタ具象値（値型整数・構造体実体）のインターフェースボクシング（スタック一時退避）検証
func TestLower_ValueTypeBoxing(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Item struct {
    id int
}

func inspectAny(a any) int {
    switch v := a.(type) {
    case int:
        return v
    case Item:
        return v.id
    default:
        return 0
    }
}

func main() int {
    // 1. int 値実体のボクシング
    valInt := 42
    r1 := inspectAny(valInt)

    // 2. 構造体値実体のボクシング
    var it Item
    it.id = 99
    r2 := inspectAny(it)

    printf("R1=%d,R2=%d\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "R1=42,R2=99",
		ExpectedExit: 0,
	})
}

// 5. インターフェースに対する明示的nil代入およびnil判定検証
func TestLower_NilInterfaceCoercion(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Runner interface {
    Run() int
}

func main() int {
    var r Runner = nil
    var a any = nil

    isNilR := 0
    if r == nil {
        isNilR = 1
    }

    isNilA := 0
    if a == nil {
        isNilA = 1
    }

    printf("NIL_R=%d,NIL_A=%d\n", isNilR, isNilA)
    return 0
}
`,
		ExpectedOut:  "NIL_R=1,NIL_A=1",
		ExpectedExit: 0,
	})
}

// 6. 文字列定数プールの再利用およびデフォルト空文字列のnull終端性検証
func TestLower_StringConstantPoolAndEmptyDefault(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func getDefaultStr() string {
    var s string
    return s
}

func main() int {
    s1 := "shared_constant_pool"
    s2 := "shared_constant_pool"
    isSame := 0
    if s1 == s2 {
        isSame = 1
    }

    empty := getDefaultStr()
    emptyLen := len(empty)

    printf("SAME=%d,EMPTY_LEN=%d\n", isSame, emptyLen)
    return 0
}
`,
		ExpectedOut:  "SAME=1,EMPTY_LEN=0",
		ExpectedExit: 0,
	})
}

// 7. 複合型（配列・構造体・関数ポインタ）の未初期化変数におけるConstZeroゼロ値初期化検証
func TestLower_CompositeTypesZeroInit(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Config struct {
    timeout int
    enabled bool
}

func main() int {
    var arr [3]int
    var cfg Config
    var fn func(int) int

    isFnNil := 0
    if fn == nil {
        isFnNil = 1
    }

    printf("ARR=(%d,%d,%d),CFG=(%d,%d),FN_NIL=%d\n", arr[0], arr[1], arr[2], cfg.timeout, cfg.enabled, isFnNil)
    return 0
}
`,
		ExpectedOut:  "ARR=(0,0,0),CFG=(0,0),FN_NIL=1",
		ExpectedExit: 0,
	})
}
