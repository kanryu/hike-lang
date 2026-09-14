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