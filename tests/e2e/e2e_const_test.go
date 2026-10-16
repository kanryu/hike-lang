package e2e_test

import "testing"

func TestParser_Const_Batch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

const First = iota

const (
    Zero = iota
    One
    Offset = iota + 10
    Next
)

const (
    RepeatedFirst = iota
    RepeatedSecond = iota
    Double = iota + 10
    Inherited
)

const (
    OffsetZero = iota
    OffsetOne
    Eleven = iota + 9
    Twelve
)

func main() int {
    printf("FIRST=%d\n", First)
    printf("IOTA=%d,%d,%d,%d\n", Zero, One, Offset, Next)
    printf("IOTA_REPEAT=%d,%d,%d,%d\n", RepeatedFirst, RepeatedSecond, Double, Inherited)
    printf("IOTA_OFFSET=%d,%d,%d,%d\n", OffsetZero, OffsetOne, Eleven, Twelve)
    return 0
}
`,
		ExpectedOut:  "FIRST=0\nIOTA=0,1,12,13\nIOTA_REPEAT=0,1,12,13\nIOTA_OFFSET=0,1,11,12",
		ExpectedExit: 0,
	})
}
