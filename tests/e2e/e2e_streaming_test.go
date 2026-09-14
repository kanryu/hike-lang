package e2e_test

import "testing"

// 擬似URLから段階的に届くデータを、非同期producerとAsyncIterableで消費する。
func TestE2EStreaming_DownloadPipeline(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/collections"

func printf(format string, ...) int
func Sleep(ms int)

type Download struct {
    url string
    Blocks chan int
}

type Chunk[T] struct {
    Data T
}

func (d *Download) InitIterator(buf *byte) int {
    return 0
}

func (d *Download) NextChannel(buf *byte) (chan int, bool) {
    return d.Blocks, true
}

func download(url string) *Download {
    result := &Download{url: url, Blocks: make(chan int, 3)}

    Async(func() int {
        Sleep(5)
        result.Blocks <- 10
        Sleep(5)
        result.Blocks <- 20
        Sleep(10)
        result.Blocks <- 30
        return 0
    })

    return result
}

func main() int {
    url := "https://example.test/data.bin"
    stream := download(url)
    total := 0
    blocks := 0

    for value := range <-stream {
        chunk := Chunk[int]{Data: value}
        printf("BLOCK=%d ", chunk.Data)
        total = total + chunk.Data
        blocks = blocks + 1
        if blocks == 3 {
            break
        }
    }

    printf("URL=%s,BLOCKS=%d,TOTAL=%d\n", url, blocks, total)
    return 0
}
`,
		ExpectedOut:  "BLOCK=10 BLOCK=20 BLOCK=30 URL=https://example.test/data.bin,BLOCKS=3,TOTAL=60",
		ExpectedExit: 0,
	})
}

// ジェネリック構造体のメソッド特殊化と、非同期for-rangeの組み合わせを検証する。
func TestE2EStreaming_GenericDownloadPipeline(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Download[T] struct {
    Blocks chan T
}

func (d *Download[T]) InitIterator(buf *byte) int {
    return 0
}

func (d *Download[T]) NextChannel(buf *byte) (chan T, bool) {
    return d.Blocks, true
}

func download() *Download[int] {
    result := &Download[int]{Blocks: make(chan int, 3)}
    Async(func() int {
        result.Blocks <- 11
        result.Blocks <- 22
        result.Blocks <- 33
        return 0
    })
    return result
}

func main() int {
    total := 0
    blocks := 0
    stream := download()

    for block := range <-stream {
        total = total + block
        blocks = blocks + 1
        if blocks == 3 {
            break
        }
    }

    printf("GENERIC_BLOCKS=%d,GENERIC_TOTAL=%d\n", blocks, total)
    return 0
}
`,
		ExpectedOut:  "GENERIC_BLOCKS=3,GENERIC_TOTAL=66",
		ExpectedExit: 0,
	})
}

// ジェネリック構造体を同期 for-range (InitIterator + Next) で走査する。
func TestE2EStreaming_GenericSynchronousPipeline(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Sequence[T] struct {
}

func (s *Sequence[T]) InitIterator(buf *byte) int {
    return 0
}

func (s *Sequence[T]) Next(buf *byte) (T, bool) {
    var value T
    return value, true
}

func main() int {
    sequence := &Sequence[int]{}
    total := 0
    count := 0
    for value := range sequence {
        total = total + 5
        count = count + 1
        if count == 3 {
            break
        }
    }
    printf("GENERIC_SYNC_COUNT=%d,GENERIC_SYNC_TOTAL=%d\n", count, total)
    return 0
}
`,
		ExpectedOut:  "GENERIC_SYNC_COUNT=3,GENERIC_SYNC_TOTAL=15",
		ExpectedExit: 0,
	})
}
