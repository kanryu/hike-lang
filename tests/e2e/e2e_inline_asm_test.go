package e2e_test

import "testing"

func TestE2EInlineAsm(t *testing.T) {
	t.Parallel()
	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    **asm**("nop", "", "")
    buf := []byte{0}
    ptr := &buf[0]
    **asm**("", "", "r", ptr)
    printf("ASM=1\n")
    return 0
}
`,
		ExpectedOut:  "ASM=1",
		ExpectedExit: 0,
	})
}
