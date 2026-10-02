package e2e_test

import "testing"

func TestE2EClosureCapturesReceiverAndParameterAfterReturn(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Counter struct {
    value int
}

// Both the receiver slot and the method parameter outlive this activation
// record because the returned closure is invoked after the method returns.
func (c *Counter) MakeReader(offset int) func() int {
    // Keep the string temporaries in the same frame as the captured receiver
    // and parameter. The returned closure must outlive all of these locals.
    scratch := "stack"
    scratch += "-reuse"
    scratch += "-one"
    scratch += "-two"
    scratch += "-three"
    scratchLen := len(scratch)
    _ = scratchLen
    return func() int {
        return c.value + offset
    }
}

func main() int {
    counter := &Counter{value: 40}
    first := counter.MakeReader(2)
    second := (&Counter{value: 100}).MakeReader(3)
    printf("CAPTURE=%d,%d\n", first(), second())
    return 0
}
`,
		ExpectedOut:  "CAPTURE=42,103",
		ExpectedExit: 0,
	})
}
