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

// 5. Indexable 構造体を二重に適用した固定長行列アクセスのテスト。
// Matrix.Get は行ポインターを返し、Row.Get は列要素を返すため、
// matrix[row][column] が二段階の組み込みインターフェース解決になる。
func TestInterface_NestedIndexableMatrix(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func malloc(size int) *byte
func free(ptr *byte)
func printf(format string, ...) int

type Row struct {
    data *int
}


func (r *Row) Get(column int) int {
    return r.data[column]
}

func (r *Row) Set(column int, value int) {
    p := r.data + column
    *p = value
}

type Matrix struct {
    rows *Row
}

func (m *Matrix) Get(row int) *Row {
    return m.rows + row
}

func (m *Matrix) Set(row int, value *Row) {
    p := m.rows + row
    *p = *value
}

func main() int {
    var data *int = malloc(8 * 8)
    p := data + 0
    *p = 11
    p = data + 1
    *p = 12
    p = data + 2
    *p = 13
    p = data + 3
    *p = 21
    p = data + 4
    *p = 22
    p = data + 5
    *p = 23
    p = data + 6
    *p = 31
    p = data + 7
    *p = 32

    var rows *Row = malloc(3 * 8)
    row := rows + 0
    row.data = data
    row = rows + 1
    row.data = data + 3
    row = rows + 2
    row.data = data + 6

    var matrix *Matrix = malloc(8)
    matrix.rows = rows

    first := matrix[0][0]
    middle := matrix[1][2]
    last := matrix[2][1]
    replacement := rows + 1
    matrix[1] = replacement
    matrix[1][2] = 99
    middleAfterSet := matrix[1][2]
    printf("MATRIX=%d,%d,%d;SET=%d\n", first, middle, last, middleAfterSet)

    free((*byte)(data))
    free((*byte)(rows))
    free((*byte)(matrix))
    return 0
}
`,
		ExpectedOut:  "MATRIX=11,23,32;SET=99",
		ExpectedExit: 0,
	})
}

// 6. 型引数と末尾const引数を持つ Matrix[int, 8, 8] の二重Get/Setを検証する。
func TestInterface_ConstGenericMatrixInstantiation(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func malloc(size int) *byte
func free(ptr *byte)
func printf(format string, ...) int

type Row struct {
    data *int
}

func (r *Row) Get(column int) int {
    return r.data[column]
}

func (r *Row) Set(column int, value int) {
    p := r.data + column
    *p = value
}

type Matrix[T, Rows uint, Cols uint] struct {
    rows *Row
}

func (m *Matrix[T, Rows, Cols]) Get(row int) *Row {
    return m.rows + row
}

func (m *Matrix[T, Rows, Cols]) Set(row int, value *Row) {
    p := m.rows + row
    *p = *value
}

func main() int {
    var data *int = malloc(8 * 8 * 8)
    p := data + 0
    *p = 1
    p = data + 28
    *p = 29
    p = data + 63
    *p = 64

    var rows *Row = malloc(8 * 8)
    row := rows + 0
    row.data = data
    row = rows + 3
    row.data = data + 24
    row = rows + 7
    row.data = data + 56

    matrix := Matrix[int, 8, 8]{rows: rows}
    first := matrix[0][0]
    middle := matrix[3][4]
    last := matrix[7][7]
    replacement := rows + 3
    matrix[3] = replacement
    matrix[3][4] = 99
    middleAfterSet := matrix[3][4]
    printf("CONST_MATRIX=%d,%d,%d;SET=%d\n", first, middle, last, middleAfterSet)

    free((*byte)(data))
    free((*byte)(rows))
    return 0
}
`,
		ExpectedOut:  "CONST_MATRIX=1,29,64;SET=99",
		ExpectedExit: 0,
	})
}

// Go-compatible assertion semantics: a single-result assertion succeeds,
// while type switches support nil and multi-type cases.
func TestInterface_GoTypeAssertionAndSwitchSemantics(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func inspect(v any) int {
    switch v.(type) {
    case nil:
        return 1
    case int, string:
        return 2
    default:
        return 3
    }
    return 0
}

func main() int {
    var value any = 42
    exact := value.(int)
    var empty any = nil
    printf("EXACT=%d,NIL=%d,INT=%d\n", exact, inspect(empty), inspect(value))
    return 0
}
`,
		ExpectedOut:  "EXACT=42,NIL=1,INT=2",
		ExpectedExit: 0,
	})
}
