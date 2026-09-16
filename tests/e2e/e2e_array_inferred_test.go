package e2e_test

import "testing"

// 初期化リストから固定長配列の要素数を推論する構文を検証する。
func TestE2E_Array_InferredLength(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    math_lut := [...]byte{
        0x01, 0x02, 0x04, 0x08,
        0x10, 0x20, 0x40, 0x80,
        0x1d, 0x3a, 0x74, 0xe8,
        0xcd, 0x87, 0x13, 0x26,
    }
    printf("FIRST=%d,MIDDLE=%d,LAST=%d\n",
        math_lut[0], math_lut[8], math_lut[15])
    return 0
}
`,
		ExpectedOut:  "FIRST=1,MIDDLE=29,LAST=38",
		ExpectedExit: 0,
	})
}
