package e2e_test

import (
	"runtime"
	"testing"
)

// TestE2EStdMemoryMapMappedConcurrentSerializeDeserialize exercises the
// compiler intrinsic for a mapped concurrent variable. This is intentionally
// separate from the ordinary Mapping API test: the first argument of
// MappedSerialize/MappedDeserialize is the mapped variable group, not a
// *memorymap.Mapping value.
func TestE2EStdMemoryMapMappedConcurrentSerializeDeserialize(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("mapped-variable memorymap A2A test requires Windows Win32 APIs")
	}

	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/memorymap"

func printf(format string, ...) int

type Snapshot struct {
    ID int
    Rank int
    Name string
}

var concurrent(64, mapped(state)) {
    count int
    ratio float64
    text string
    payload []byte
    snapshot Snapshot
}

func main() int {
    path := "std-memorymap-mapped-concurrent.hmb"

    count = 125
    ratio = 3.5
    text = "hike"
    payload = []byte{9, 8, 7, 6}
    snapshot = Snapshot{ID: 41, Rank: 82, Name: "checkpoint"}
    serializeErr := memorymap.MappedSerialize(state, path)

    count = -1
    ratio = -1.0
    text = "destroyed"
    payload = []byte{0, 0, 0, 0}
    snapshot = Snapshot{ID: -1, Rank: -1, Name: "destroyed"}
    deserializeErr := memorymap.MappedDeserialize(state, path)

    printf("SERIALIZE=%d,DESERIALIZE=%d,COUNT=%d,RATIO=%.2f,TEXT=%s,BYTES=%d,%d,%d,%d,STRUCT=%d,%d,%s\n",
        serializeErr == nil, deserializeErr == nil, count, ratio, text,
        payload[0], payload[1], payload[2], payload[3],
        snapshot.ID, snapshot.Rank, snapshot.Name)
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,DESERIALIZE=1,COUNT=125,RATIO=3.50,TEXT=hike,BYTES=9,8,7,6,STRUCT=41,82,checkpoint",
		ExpectedExit: 0,
	})
}

// TestE2EStdMemoryMapMappedHandleRejectsOrdinaryUse ensures that the name in
// mapped(name) is not a value that can be printed, assigned, passed to an
// arbitrary function, or otherwise evaluated by the program.
func TestE2EStdMemoryMapMappedHandleRejectsOrdinaryUse(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("mapped-variable memorymap A2A test requires Windows Win32 APIs")
	}

	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

func printf(format string, ...) int

var concurrent(64, mapped(state)) {
    count int
}

func main() int {
    printf("STATE=%d\n", state)
    return 0
}
`,
		ExpectedOut:  "",
		ExpectedExit: 1,
	})
}
