package e2e_test

import "testing"

// -------------------------------------------------------------
// 1. 文字列プリミティブ操作 (Primitive Operations)
// -------------------------------------------------------------

// 文字列連結 (+) の検証
func TestStrings_Primitive_Concat(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    s1 := "Hello, "
    s2 := "Hike "
    s3 := "World!"
    res := s1 + s2 + s3
    printf("RES=%s\n", res)
    return 0
}
`,
		ExpectedOut:  "RES=Hello, Hike World!",
		ExpectedExit: 0,
	})
}

// 文字列比較 (==, !=) の検証
func TestStrings_Primitive_Comparison(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    a := "apple"
    b := "apple"
    c := "banana"

    eq1 := (a == b)
    eq2 := (a == c)
    neq := (a != c)

    printf("EQ1=%d,EQ2=%d,NEQ=%d\n", eq1, eq2, neq)
    return 0
}
`,
		ExpectedOut:  "EQ1=1,EQ2=0,NEQ=1",
		ExpectedExit: 0,
	})
}

// 文字列長 (len) とインデックスアクセス (byte取得) の検証
func TestStrings_Primitive_LenAndIndex(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    s := "Golang"
    l := len(s)
    c0 := s[0]
    c3 := s[3]
    printf("LEN=%d,C0=%c,C3=%c\n", l, c0, c3)
    return 0
}
`,
		ExpectedOut:  "LEN=6,C0=G,C3=a",
		ExpectedExit: 0,
	})
}

// 部分文字列の切り出し (s[low:high], s[:high], s[low:]) の検証
func TestStrings_Primitive_Subslice(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    s := "HikeCompiler"
    sub1 := s[0:4]
    sub2 := s[4:12]
    sub3 := s[:4]
    sub4 := s[4:]
    printf("SUB1=%s,SUB2=%s,SUB3=%s,SUB4=%s\n", sub1, sub2, sub3, sub4)
    return 0
}
`,
		ExpectedOut:  "SUB1=Hike,SUB2=Compiler,SUB3=Hike,SUB4=Compiler",
		ExpectedExit: 0,
	})
}

// エスケープシーケンス (\n, \t, \", \\) のパースと出力検証
func TestStrings_Primitive_EscapeSequences(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    s := "Line1\n\t\"Quoted\" \\ Backslash"
    printf("%s\n", s)
    return 0
}
`,
		ExpectedOut:  "Line1\n\t\"Quoted\" \\ Backslash",
		ExpectedExit: 0,
	})
}

// バイトスライス ([]byte) から文字列 (string) へのキャスト検証
func TestStrings_Primitive_ByteSliceToString(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    b := []byte{72, 73, 75, 69} // 'H', 'I', 'K', 'E'
    s := string(b)
    printf("STR=%s,LEN=%d\n", s, len(s))
    return 0
}
`,
		ExpectedOut:  "STR=HIKE,LEN=4",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 2. std/strings API テスト
// -------------------------------------------------------------

// strings.Contains: 部分文字列の含有判定
func TestStrings_Std_Contains(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    s := "fast and safe systems language"
    hasSafe := strings.Contains(s, "safe")
    hasSlow := strings.Contains(s, "slow")
    printf("SAFE=%d,SLOW=%d\n", hasSafe, hasSlow)
    return 0
}
`,
		ExpectedOut:  "SAFE=1,SLOW=0",
		ExpectedExit: 0,
	})
}

// strings.HasPrefix / strings.HasSuffix: 接頭辞・接尾辞判定
func TestStrings_Std_PrefixSuffix(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    filename := "main.hike"
    p := strings.HasPrefix(filename, "main.")
    s := strings.HasSuffix(filename, ".hike")
    wrong := strings.HasSuffix(filename, ".go")
    printf("P=%d,S=%d,W=%d\n", p, s, wrong)
    return 0
}
`,
		ExpectedOut:  "P=1,S=1,W=0",
		ExpectedExit: 0,
	})
}

// strings.Index: 部分文字列の出現位置インデックス特定
func TestStrings_Std_Index(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    src := "abcdefg_hijk"
    idx1 := strings.Index(src, "def")
    idx2 := strings.Index(src, "xyz")
    printf("IDX1=%d,IDX2=%d\n", idx1, idx2)
    return 0
}
`,
		ExpectedOut:  "IDX1=3,IDX2=-1",
		ExpectedExit: 0,
	})
}

// strings.ToUpper / strings.ToLower: 大文字・小文字変換
func TestStrings_Std_UpperLower(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    raw := "Hike Language 2026"
    up := strings.ToUpper(raw)
    low := strings.ToLower(raw)
    printf("UP=%s\nLOW=%s\n", up, low)
    return 0
}
`,
		ExpectedOut:  "UP=HIKE LANGUAGE 2026\nLOW=hike language 2026",
		ExpectedExit: 0,
	})
}

// strings.TrimSpace: 前後の空白文字除去
func TestStrings_Std_TrimSpace(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    dirty := "  \t Hello Hike! \n "
    clean := strings.TrimSpace(dirty)
    printf("CLEAN=[%s]\n", clean)
    return 0
}
`,
		ExpectedOut:  "CLEAN=[Hello Hike!]",
		ExpectedExit: 0,
	})
}

// strings.Join: スライスの文字列連結
func TestStrings_Std_Join(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    parts := []string{"usr", "local", "bin", "hikec"}
    path := strings.Join(parts, "/")
    printf("PATH=%s\n", path)
    return 0
}
`,
		ExpectedOut:  "PATH=usr/local/bin/hikec",
		ExpectedExit: 0,
	})
}

// strings.Split: 区切り文字による文字列分割
func TestStrings_Std_Split(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    line := "apple,orange,banana"
    fruits := strings.Split(line, ",")
    printf("LEN=%d: %s %s %s\n", len(fruits), fruits[0], fruits[1], fruits[2])
    return 0
}
`,
		ExpectedOut:  "LEN=3: apple orange banana",
		ExpectedExit: 0,
	})
}

// strings.ReplaceAll: 全一致箇所の文字列置換
func TestStrings_Std_ReplaceAll(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    src := "foo_bar_foo_baz"
    res := strings.ReplaceAll(src, "foo", "qux")
    printf("RES=%s\n", res)
    return 0
}
`,
		ExpectedOut:  "RES=qux_bar_qux_baz",
		ExpectedExit: 0,
	})
}

// strings.Repeat: 文字列の指定回数リピート生成
func TestStrings_Std_Repeat(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/strings"

func printf(format string, ...) int

func main() int {
    bar := strings.Repeat("-", 5)
    echo := strings.Repeat("Ha", 3)
    printf("BAR=%s,ECHO=%s\n", bar, echo)
    return 0
}
`,
		ExpectedOut:  "BAR=-----,ECHO=HaHaHa",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 3. std/fmt API テスト
// -------------------------------------------------------------

// fmt.Sprintf: 整形された文字列生成の検証
func TestStrings_Fmt_Sprintf(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/fmt"

func printf(format string, ...) int

func main() int {
    id := 42
    name := "Hike"
    formatted := fmt.Sprintf("ID:%d,NAME:%s", id, name)
    printf("RES=%s\n", formatted)
    return 0
}
`,
		ExpectedOut:  "RES=ID:42,NAME:Hike",
		ExpectedExit: 0,
	})
}

// fmt.Println: 標準出力への出力と改行付与の検証
func TestStrings_Fmt_Println(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/fmt"

func main() int {
    fmt.Println("Hello via fmt.Println")
    return 0
}
`,
		ExpectedOut:  "Hello via fmt.Println",
		ExpectedExit: 0,
	})
}
