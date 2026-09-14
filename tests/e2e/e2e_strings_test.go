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

// -------------------------------------------------------------
// 4. string と cstring の相互変換 & FFI 連携テスト
// -------------------------------------------------------------

// string <-> cstring の相互キャスト検証
func TestStrings_CString_Conversion(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    orig := "Hello FFI"
    // string -> cstring
    cs := cstring(orig)
    // cstring -> string
    back := string(cs)

    lOrig := len(orig)
    lBack := len(back)
    eq := (orig == back)

    printf("LEN1=%d,LEN2=%d,EQ=%d,STR=%s\n", lOrig, lBack, eq, back)
    return 0
}
`,
		ExpectedOut:  "LEN1=9,LEN2=9,EQ=1,STR=Hello FFI",
		ExpectedExit: 0,
	})
}

// 外部 C 関数 (extern) への cstring の受け渡し検証
func TestStrings_CString_PassToExtern(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func puts(s cstring) int
func printf(format string, ...) int

func main() int {
    msg := "Printed via puts"
    cs := cstring(msg)
    puts(cs)

    // cstring を printf に渡す検証
    fmtStr := "CS_PRINT=%s\n"
    printf(fmtStr, cs)
    return 0
}
`,
		ExpectedOut:  "Printed via puts\nCS_PRINT=Printed via puts",
		ExpectedExit: 0,
	})
}

// cstring に対するインデックスアクセス (cs[i]) およびスライス操作 (cs[low:high]) の検証
func TestStrings_CString_IndexAndSubslice(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    cs := cstring("HikeCString")

    // 1. インデックスアクセス (byte 取得)
    c0 := cs[0]
    c4 := cs[4]

    // 2. 部分文字列スライス切り出し
    sub := cs[4:11]

    printf("C0=%c,C4=%c,SUB=%s\n", c0, c4, sub)
    return 0
}
`,
		ExpectedOut:  "C0=H,C4=C,SUB=CString",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 5. 境界値・ゼロ値・特殊文字列テスト
// -------------------------------------------------------------

// 空文字列 ("") の各種操作 (len、連結、スライス、cstring 変換) 検証
func TestStrings_Primitive_EmptyString(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    empty := ""
    l := len(empty)

    // 空文字列との連結
    concat1 := empty + "hike"
    concat2 := "hike" + empty

    // 空文字列への cstring 変換と復元
    cs := cstring(empty)
    back := string(cs)

    // 同一比較
    eq := (empty == "")
    eqBack := (back == "")

    printf("LEN=%d,C1=%s,C2=%s,EQ=%d,EQB=%d\n", l, concat1, concat2, eq, eqBack)
    return 0
}
`,
		ExpectedOut:  "LEN=0,C1=hike,C2=hike,EQ=1,EQB=1",
		ExpectedExit: 0,
	})
}

// var 宣言による string の初期値 (zero value) の検証
func TestStrings_Primitive_ZeroValue(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    var s string
    l := len(s)
    eq := (s == "")
    s = s + "initialized"

    printf("LEN=%d,EQ=%d,S=%s\n", l, eq, s)
    return 0
}
`,
		ExpectedOut:  "LEN=0,EQ=1,S=initialized",
		ExpectedExit: 0,
	})
}

// UTF-8 マルチバイト文字列のバイト長およびインデックスアクセス検証
func TestStrings_Primitive_UTF8Bytes(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    // "こんにちは" は UTF-8 で各文字 3 バイト x 5 = 15 バイト
    s := "こんにちは"
    l := len(s)
    b0 := s[0]
    b1 := s[1]
    b2 := s[2]

    // 部分スライス (最初の 1 文字 = 先頭 3 バイト)
    firstChar := s[0:3]

    printf("LEN=%d,BYTES=(%d,%d,%d),CHAR=%s\n", l, b0, b1, b2, firstChar)
    return 0
}
`,
		ExpectedOut:  "LEN=15,BYTES=(227,129,147),CHAR=こ",
		ExpectedExit: 0,
	})
}

// 構造体フィールドに格納された string / cstring の読み書き検証
func TestStrings_Struct_StringFields(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Config struct {
    name    string
    rawPtr  cstring
    version int
}

func main() int {
    var cfg Config
    cfg.name = "CompilerConfig"
    cfg.rawPtr = cstring("RawBuffer")
    cfg.version = 1

    strFromRaw := string(cfg.rawPtr)
    printf("NAME=%s,VER=%d,RAW=%s\n", cfg.name, cfg.version, strFromRaw)
    return 0
}
`,
		ExpectedOut:  "NAME=CompilerConfig,VER=1,RAW=RawBuffer",
		ExpectedExit: 0,
	})
}

// -------------------------------------------------------------
// 6. std/regexp API テスト (Submatch, Index, Quantifier, UTF-8, Errors)
// -------------------------------------------------------------

// キャプチャグループ (FindStringSubmatch) の検証
func TestStrings_Regexp_Submatch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/regexp"

func printf(format string, ...) int

func main() int {
    re := regexp.MustCompile("([a-z]+)=([0-9]+)")
    matches := re.FindStringSubmatch("user=100")
    printf("LEN=%d,M0=%s,M1=%s,M2=%s\n", len(matches), matches[0], matches[1], matches[2])
    return 0
}
`,
		ExpectedOut:  "LEN=3,M0=user=100,M1=user,M2=100",
		ExpectedExit: 0,
	})
}

// 一致位置のインデックス特定 (FindStringIndex) の検証
func TestStrings_Regexp_FindIndex(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/regexp"

func printf(format string, ...) int

func main() int {
    re := regexp.MustCompile("[0-9]+")
    loc := re.FindStringIndex("abc12345def")
    printf("START=%d,END=%d\n", loc[0], loc[1])
    return 0
}
`,
		ExpectedOut:  "START=3,END=8",
		ExpectedExit: 0,
	})
}

// 量指定子 (非貪欲マッチ) と境界アサーション (^, $) の検証
func TestStrings_Regexp_QuantifiersAndAnchors(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/regexp"

func printf(format string, ...) int

func main() int {
    reLazy := regexp.MustCompile("<.*?>")
    res1 := reLazy.ReplaceAllString("<div>hello</div>", "[TAG]")

    reAnchor := regexp.MustCompile("^Hike.*2026$")
    m1 := reAnchor.MatchString("Hike Lang 2026")
    m2 := reAnchor.MatchString("Other Hike Lang 2026")

    printf("RES1=%s,M1=%d,M2=%d\n", res1, m1, m2)
    return 0
}
`,
		ExpectedOut:  "RES1=[TAG]hello[TAG],M1=1,M2=0",
		ExpectedExit: 0,
	})
}

// UTF-8 マルチバイト文字列に対する正規表現マッチおよびキャプチャの検証
func TestStrings_Regexp_UTF8(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/regexp"

func printf(format string, ...) int

func main() int {
    re := regexp.MustCompile("世界")
    matched := re.MatchString("こんにちは世界！")

    reSub := regexp.MustCompile("こんにちは(.*)")
    parts := reSub.FindStringSubmatch("こんにちは世界")

    printf("MATCH=%d,SUB=%s\n", matched, parts[1])
    return 0
}
`,
		ExpectedOut:  "MATCH=1,SUB=世界",
		ExpectedExit: 0,
	})
}

// 空文字列に対するマッチおよび不一致時のインデックス取得境界値の検証
func TestStrings_Regexp_EdgeCases(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/regexp"

func printf(format string, ...) int

func main() int {
    reEmpty := regexp.MustCompile("^$")
    m1 := reEmpty.MatchString("")
    m2 := reEmpty.MatchString("non-empty")

    reNum := regexp.MustCompile("[0-9]+")
    noLoc := reNum.FindStringIndex("abcdef")
    noMatchLen := len(noLoc)

    printf("EMPTY_MATCH=%d,NON_EMPTY=%d,NOLOC_LEN=%d\n", m1, m2, noMatchLen)
    return 0
}
`,
		ExpectedOut:  "EMPTY_MATCH=1,NON_EMPTY=0,NOLOC_LEN=0",
		ExpectedExit: 0,
	})
}
