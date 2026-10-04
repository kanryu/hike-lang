package e2e_test

import "testing"

// 基本的な文字列操作を一つのHikeC実行にまとめる。各CASEマーカーにより
// 個別テストと同じく、どの操作が失敗したかを出力から特定できる。
func TestStrings_Primitive_Batch(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...)

func main() int {
    s1 := "Hello, "
    s2 := "Hike "
    s3 := "World!"
    printf("CASE=concat:%s\n", s1+s2+s3)

    a := "apple"
    b := "apple"
    c := "banana"
    printf("CASE=compare:%d,%d,%d\n", a == b, a == c, a != c)

    indexed := "Golang"
    printf("CASE=index:%d,%c,%c\n", len(indexed), indexed[0], indexed[3])

    sliced := "HikeCompiler"
    printf("CASE=subslice:%s,%s,%s,%s\n", sliced[0:4], sliced[4:12], sliced[:4], sliced[4:])

    escaped := "Line1\n\t\"Quoted\" \\ Backslash"
    printf("CASE=escape:%s\n", escaped)

    bytes := []byte{72, 73, 75, 69}
    converted := string(bytes)
    printf("CASE=bytes:%s,%d\n", converted, len(converted))
    return 0
}
`,
		ExpectedOut:  "CASE=concat:Hello, Hike World!\nCASE=compare:1,0,1\nCASE=index:6,G,a\nCASE=subslice:Hike,Compiler,Hike,Compiler\nCASE=escape:Line1\n\t\"Quoted\" \\ Backslash\nCASE=bytes:HIKE,4",
		ExpectedExit: 0,
	})
}

// Substring views may contain embedded NUL bytes. Concatenating two such
// views must preserve their explicit lengths instead of deriving the result
// length with strlen.
func TestStrings_Primitive_SubstringConcatPreservesLength(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...)

func main() int {
    bytes := []byte{'A', 0, 'B', 'C', 0, 'D'}
    source := string(bytes)
    left := source[0:3]
    right := source[3:6]
    joined := left + right
    printf("LEN=%d,B0=%d,B1=%d,B2=%d,B3=%d,B4=%d,B5=%d\n",
        len(joined), joined[0], joined[1], joined[2], joined[3], joined[4], joined[5])
    return 0
}
`,
		ExpectedOut:  "LEN=6,B0=65,B1=0,B2=66,B3=67,B4=0,B5=68",
		ExpectedExit: 0,
	})
}
