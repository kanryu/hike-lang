package e2e_test

import "testing"

// TestE2ECBufferFromHikeSlice verifies the cbuffer compiler intrinsic with an
// ordinary Hike slice as the source allocation. The callback is intentionally
// a no-op because the original slice owns the storage.
func TestE2ECBufferFromHikeSlice(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func release() {}

func main() int {
    source := make([]byte, 4)
    source[0] = 7
    source[1] = 8
    source[2] = 9
    source[3] = 10

    buffer := cbuffer(&source[0], len(source), release)
    if buffer.Len() != 4 { return 1 }
    if buffer[0] != 7 || buffer[3] != 10 { return 2 }

    copied := buffer[1:3]
    if len(copied) != 2 || copied[0] != 8 || copied[1] != 9 { return 3 }

    buffer.Close()
    if buffer.Len() != 0 { return 4 }
    if source[0] != 7 || source[3] != 10 { return 5 }
    return 42
}
`,
		ExpectedExit: 42,
	})
}
