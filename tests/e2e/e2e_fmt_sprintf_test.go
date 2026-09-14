package e2e_test

import "testing"

// 1. 基本型 (%d, %s, %c, %t, %%) のフォーマット検証
func TestFmt_Sprintf_BasicTypes(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/fmt"

func printf(format string, ...) int

func main() int {
    s := fmt.Sprintf("INT=%d,NEG=%d,STR=%s,CHAR=%c,BOOL_T=%t,BOOL_F=%t,PERCENT=%%", 42, -17, "hello", 65, true, false)
    printf("%s\n", s)
    return 0
}
`,
		ExpectedOut:  "INT=42,NEG=-17,STR=hello,CHAR=A,BOOL_T=true,BOOL_F=false,PERCENT=%",
		ExpectedExit: 0,
	})
}

// 2. 基数変換とフラグ (%b, %o, %x, %X, #, 0パディング, +/スペースフラグ) の検証
func TestFmt_Sprintf_RadixAndFlags(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/fmt"

func printf(format string, ...) int

func main() int {
    // 2進数、8進数、16進数（#プレフィックス付き）
    r1 := fmt.Sprintf("BIN=%#b,OCT=%#o,HEX=%#x,HEX_U=%#X", 5, 64, 255, 255)

    // 符号フラグ(+)、ゼロ埋め(0)、幅指定
    r2 := fmt.Sprintf("POS=%+d,ZERO=%05d", 42, 7)

    printf("%s | %s\n", r1, r2)
    return 0
}
`,
		ExpectedOut:  "BIN=0b101,OCT=0o100,HEX=0xff,HEX_U=0XFF | POS=+42,ZERO=00007",
		ExpectedExit: 0,
	})
}

// 3. アライメント (左右パディング)、クォート (%q)、浮動小数精度 (%.2f) の検証
func TestFmt_Sprintf_PrecisionAndPadding(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/fmt"

func printf(format string, ...) int

func main() int {
    // 左右寄せ幅指定
    align := fmt.Sprintf("L=[%-6s],R=[%6s]", "go", "hike")

    // エスケープ付きクォートと浮動小数精度
    quoted := fmt.Sprintf("Q=%q,PI=%.2f", "A\nB", 3.141592)

    printf("%s | %s\n", align, quoted)
    return 0
}
`,
		ExpectedOut:  `L=[go    ],R=[  hike] | Q="A\nB",PI=3.14`,
		ExpectedExit: 0,
	})
}

// 4. 型ダンプ (%T) と型自動推論 (%v) の検証
func TestFmt_Sprintf_TypeAndAutoValue(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/fmt"

func printf(format string, ...) int

func main() int {
    t1 := fmt.Sprintf("T_INT=%T,T_STR=%T,T_BOOL=%T", 100, "text", true)
    v1 := fmt.Sprintf("V_INT=%v,V_STR=%v,V_BOOL=%v", 999, "hike", false)

    printf("%s | %s\n", t1, v1)
    return 0
}
`,
		ExpectedOut:  "T_INT=int,T_STR=string,T_BOOL=bool | V_INT=999,V_STR=hike,V_BOOL=false",
		ExpectedExit: 0,
	})
}

// 5. Cのprintf非依存を確認するための std/fmt/sprintf 直接インポート検証
func TestFmt_Sprintf_DirectModuleImport(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/fmt/sprintf"

func printf(format string, ...) int

func main() int {
    // std/fmt を介さず、純粋な sprintf.Sprintf を直接呼び出し
    res := sprintf.Sprintf("VAL=%v,HEX=%#x,PAD=%04d", 123, 16, 8)
    printf("DIRECT=%s\n", res)
    return 0
}
`,
		ExpectedOut:  "DIRECT=VAL=123,HEX=0x10,PAD=0008",
		ExpectedExit: 0,
	})
}
