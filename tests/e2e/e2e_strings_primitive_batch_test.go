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
