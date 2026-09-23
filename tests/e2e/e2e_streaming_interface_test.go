package e2e_test

import "testing"

// Interface-valued AsyncIterable must dispatch InitIterator and NextChannel
// through the interface table rather than falling back to a normal range.
func TestE2EStreaming_InterfaceAsyncIterable(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type AsyncStream interface {
    InitIterator(buf *byte) int
    NextChannel(buf *byte) (chan int, bool)
}

type Download struct {
    Blocks chan int
}

func (d *Download) InitIterator(buf *byte) int {
    return 0
}

func (d *Download) NextChannel(buf *byte) (chan int, bool) {
    return d.Blocks, true
}

func main() int {
    concrete := &Download{Blocks: make(chan int, 2)}
    concrete.Blocks <- 4
    concrete.Blocks <- 5
    var stream AsyncStream = concrete
    total := 0
    count := 0
    for value := range <-stream {
        total = total + value
        count = count + 1
        if count == 2 {
            break
        }
    }
    printf("INTERFACE_COUNT=%d,INTERFACE_TOTAL=%d\n", count, total)
    return 0
}
`,
		ExpectedOut:  "INTERFACE_COUNT=2,INTERFACE_TOTAL=9",
		ExpectedExit: 0,
	})
}
