package e2e_test

import "testing"

// 1. 静的直接呼び出しと、インターフェース変数を介した動的ディスパッチ（多態性）のテスト
func TestInterface_StaticAndDynamicDispatch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Calculator interface {
    Compute(a int, b int) int
}

type Adder struct {
    bias int
}

func (add *Adder) Compute(a int, b int) int {
    return add.bias + a + b
}

type Multiplier struct {
    factor int
}

func (mul *Multiplier) Compute(a int, b int) int {
    return (a * b) * mul.factor
}

func main() int {
    add := Adder{bias: 5}
    mul := Multiplier{factor: 2}

    // 静的直接ディスパッチ (コンパイル時直接ジャンプ)
    staticAdd := add.Compute(10, 20)
    staticMul := mul.Compute(3, 4)

    // 動的インターフェースディスパッチ (Fat Pointer + itab 経由)
    var calc Calculator = &add
    dynAdd := calc.Compute(10, 20)

    calc = &mul
    dynMul := calc.Compute(3, 4)

    printf("STATIC=%d,%d;DYNAMIC=%d,%d\n", staticAdd, staticMul, dynAdd, dynMul)
    return 0
}
`,
		ExpectedOut:  "STATIC=35,24;DYNAMIC=35,24",
		ExpectedExit: 0,
	})
}

// 2. インターフェース経由での多値戻り値のアンパック代入テスト (Encoding.Decode 準拠)
func TestInterface_MultiReturnDispatch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Codec interface {
    Process(tag string, count int) (string, int, int)
}

type MockCodec struct {
    version int
}

func (m *MockCodec) Process(tag string, count int) (string, int, int) {
    return tag, m.version, count * 10
}

func main() int {
    mock := MockCodec{version: 2}
    var c Codec = &mock

    // インターフェース経由での多値アンパック代入
    tag, ver, total := c.Process("Hike", 5)

    printf("TAG=%s,VER=%d,TOTAL=%d\n", tag, ver, total)
    return 0
}
`,
		ExpectedOut:  "TAG=Hike,VER=2,TOTAL=50",
		ExpectedExit: 0,
	})
}

// 3. インターフェース値と nil の比較、およびインターフェース変数同士の同値性比較テスト
func TestInterface_ComparisonsAndNil(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Reader interface {
    Read() int
}

type StringReader struct {
    id int
}

func (r *StringReader) Read() int {
    return r.id
}

func main() int {
    var r1 Reader = nil
    isNil := (r1 == nil)
    isNotNil := (r1 != nil)

    srA := StringReader{id: 1}
    srB := StringReader{id: 2}

    var r2 Reader = &srA
    var r3 Reader = &srA
    var r4 Reader = &srB

    assignedNotNil := (r2 != nil)
    sameEqual := (r2 == r3)
    diffNotEqual := (r2 != r4)

    printf("NIL=%d,%d;EQ=%d,%d,%d\n", isNil, isNotNil, assignedNotNil, sameEqual, diffNotEqual)
    return 0
}
`,
		ExpectedOut:  "NIL=1,0;EQ=1,1,1",
		ExpectedExit: 0,
	})
}

// 4. 動的型アサーション (.(T)) による具象型復元テスト
func TestInterface_TypeAssertion(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Greeter interface {
    Greet() string
}

type JapaneseGreeter struct {
    id int
}

func (j *JapaneseGreeter) Greet() string {
    return "こんにちは"
}

type EnglishGreeter struct {
    id int
}

func (e *EnglishGreeter) Greet() string {
    return "Hello"
}

func main() int {
    jg := JapaneseGreeter{id: 777}
    var g Greeter = &jg

    // 正しい型へのアサーション
    unpacked, ok := g.(*JapaneseGreeter)

    // 不一致な型へのアサーション
    _, badOk := g.(*EnglishGreeter)

    printf("OK=%d,ID=%d;BAD_OK=%d\n", ok, unpacked.id, badOk)
    return 0
}
`,
		ExpectedOut:  "OK=1,ID=777;BAD_OK=0",
		ExpectedExit: 0,
	})
}
