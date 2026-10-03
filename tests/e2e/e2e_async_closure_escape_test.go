package e2e_test

import "testing"

// This is the smallest end-to-end shape of the compile-fork failure: a
// method returns while an async closure still holds its receiver, parameter,
// and a local pointer. The closure then reads and combines heap-backed data.
func TestE2EAsyncClosureKeepsReceiverParameterAndLocalAlive(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int
func Sleep(ms int)

type Worker struct {
    prefix string
}

func (w *Worker) Start(out chan string, suffix string) int {
    scratch := "temporary"
    scratch += "-one"
    scratch += "-two"
    scratch += "-three"
    Async(func() int {
        Sleep(10)
        out <- w.prefix + ":" + scratch + ":" + suffix
        return 0
    })
    return 0
}

func churnStack() int {
    value := "clobber"
    value += "-one"
    value += "-two"
    value += "-three"
    return len(value)
}

func main() int {
    out := make(chan string, 1)
    worker := &Worker{prefix: "left"}
    worker.Start(out, "right")
    _ = churnStack()
    value := <-out
    printf("VALUE=%s\n", value)
    if value != "left:temporary-one-two-three:right" {
        return 1
    }
    return 0
}
`,
		ExpectedOut:  "VALUE=left:temporary-one-two-three:right",
		ExpectedExit: 0,
	})
}
