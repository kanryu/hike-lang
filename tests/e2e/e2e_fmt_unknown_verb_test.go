package e2e_test

import "testing"

// Sprintf must not crash when it receives an Errorf-only verb.
func TestFmt_Sprintf_UnknownVerbIsSafe(t *testing.T) {
	t.Parallel()
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/fmt"

func printf(format string, ...)

func main() int {
    value := fmt.Sprintf("VALUE=%w,NUMBER=%d", "ignored", 42)
    printf("%s\n", value)
    return 0
}
`,
		ExpectedOut:  "VALUE=%w,NUMBER=42",
		ExpectedExit: 0,
	})
}
