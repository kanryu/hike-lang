package e2e_test

import "testing"

func TestE2EInlineAsm(t *testing.T) {
	t.Parallel()
	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    __asm__{
        params:
        "nop", "", "", ""
    }
    buf := []byte{0}
    ptr := &buf[0]
    __asm__{
        params: ptr
        "", "", "r", "~{memory}"
    }
    printf("ASM=1\n")
    return 0
}
`,
		ExpectedOut:  "ASM=1",
		ExpectedExit: 0,
	})
}
