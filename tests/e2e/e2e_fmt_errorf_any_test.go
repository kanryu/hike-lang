package e2e_test

import "testing"

// An error boxed into []any and passed through the %w Errorf path must still
// produce a valid string result.
func TestFmt_Errorf_WrapsErrorFromAnySlice(t *testing.T) {
	t.Parallel()
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/fmt"
import "std/os"

func printf(format string, ...)

func main() int {
    _, statErr := os.Stat("missing-fmt-errorf-any-slice.hike")
    args := []any{statErr}
    wrapped := fmt.Errorf("stat failed: %w", args...)
    printf("ERROR=%s\n", wrapped.Error())
    return 0
}
`,
		ExpectedOut:  "ERROR=stat failed:",
		ExpectedExit: 0,
	})
}
