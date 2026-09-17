package e2e_test

import "testing"

func TestParser_Const_SingleIota(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

const First = iota

func main() int {
    printf("FIRST=%d\n", First)
    return 0
}
`,
		ExpectedOut:  "FIRST=0",
		ExpectedExit: 0,
	})
}
