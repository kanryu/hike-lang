package e2e_test

import "testing"

// A parameter used only for indexing and len is borrowed. A parameter
// returned as a slice view escapes and must keep its backing buffer alive.
func TestFunctionManagedBufferLifetime(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func inspectBytes(buf []byte) int {
    return int(buf[0]) + len(buf)
}

func keepBytes(buf []byte) []byte {
    return buf
}

func makeBytes() []byte {
    return []byte{10, 20, 30}
}

func keepText(text string) string {
    return text + "!"
}

func main() int {
    payload := makeBytes()
    local := inspectBytes(payload)
    view := keepBytes(payload[1:])
    escaped := inspectBytes(view)
    text := keepText("hike")
    printf("LOCAL=%d,ESCAPED=%d,TEXT=%s,LEN=%d\n", local, escaped, text, len(view))
    return 0
}
`,
		ExpectedOut:  "LOCAL=13,ESCAPED=22,TEXT=hike!,LEN=2",
		ExpectedExit: 0,
	})
}
