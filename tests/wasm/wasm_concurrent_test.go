package wasm_test

import "testing"

func TestWasm32ConcurrentModeExecution(t *testing.T) {
	wasm := buildWasm(t, `package main

func main() int {
    task := Async(func() int {
        return 6 * 7
    })
    return <-task
}
`, "concurrent")
	if got, want := runWasm(t, wasm, "wasm_concurrent.js"), "WASM_RESULT=42\n"; got != want {
		t.Fatalf("WASM output = %q, want %q", got, want)
	}
}

func TestWasm32ConcurrentModeInterfaceAsyncIterable(t *testing.T) {
	wasm := buildWasm(t, `package main

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
    return total
}
`, "concurrent")
	if got, want := runWasm(t, wasm, "wasm_concurrent.js"), "WASM_RESULT=9\n"; got != want {
		t.Fatalf("WASM interface AsyncIterable output = %q, want %q", got, want)
	}
}
