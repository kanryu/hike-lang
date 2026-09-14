package e2e_test

import "testing"

// 符号付き整数は算術右シフト、符号なし整数は論理右シフトになることを検証する。
func TestE2EShift_SignedAndUnsignedRightShift(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    signed := -8 >> 1
    highBit := uint32(1) << 31
    unsigned := highBit >> 1

    printf("SIGNED=%d,UNSIGNED=%d\n", signed, unsigned)
    return 0
}
`,
		ExpectedOut:  "SIGNED=-4,UNSIGNED=1073741824",
		ExpectedExit: 0,
	})
}

// 左シフトは符号付き・符号なし整数のどちらでも正常に生成されることを検証する。
func TestE2EShift_LeftShift(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    signed := 3 << 3
    unsigned := uint(3) << 3

    printf("SIGNED=%d,UNSIGNED=%d\n", signed, unsigned)
    return 0
}
`,
		ExpectedOut:  "SIGNED=24,UNSIGNED=24",
		ExpectedExit: 0,
	})
}

// 2値代入時だけシフトのキャリーを返し、左・右シフトで捨てられた
// ビットを抽出できることを検証する。
func TestE2EShift_WithCarry(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    left, leftCarry := uint32(0x80000000) << 1
    right, rightCarry := uint32(0x80000001) >> 1
    signedRight, signedCarry := -5 >> 1

    printf("LEFT=%d,CARRY=%d,RIGHT=%d,CARRY=%d,SIGNED=%d,CARRY=%d\n", left, leftCarry, right, rightCarry, signedRight, signedCarry)
    return 0
}
`,
		ExpectedOut:  "LEFT=0,CARRY=1,RIGHT=1073741824,CARRY=1,SIGNED=-3,CARRY=1",
		ExpectedExit: 0,
	})
}
