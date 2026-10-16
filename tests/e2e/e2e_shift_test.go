package e2e_test

import "testing"

// シフト演算の符号、型、キャリーの動作を1回のコンパイルで検証する。
func TestE2EShift_Batch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    signedRight := -8 >> 1
    highBit := uint32(1) << 31
    unsignedRight := highBit >> 1

    signedLeft := 3 << 3
    unsignedLeft := uint(3) << 3

    left, leftCarry := uint32(0x80000000) << 1
    right, rightCarry := uint32(0x80000001) >> 1
    signedWithCarry, signedCarry := -5 >> 1

    printf("SIGNED=%d,UNSIGNED=%d\n", signedRight, unsignedRight)
    printf("SIGNED=%d,UNSIGNED=%d\n", signedLeft, unsignedLeft)
    printf("LEFT=%d,CARRY=%d,RIGHT=%d,CARRY=%d,SIGNED=%d,CARRY=%d\n", left, leftCarry, right, rightCarry, signedWithCarry, signedCarry)
    return 0
}
`,
		ExpectedOut:  "SIGNED=-4,UNSIGNED=1073741824\nSIGNED=24,UNSIGNED=24\nLEFT=0,CARRY=1,RIGHT=1073741824,CARRY=1,SIGNED=-3,CARRY=1",
		ExpectedExit: 0,
	})
}
