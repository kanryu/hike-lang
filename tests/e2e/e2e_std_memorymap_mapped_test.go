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

// TestE2EStdMemoryMapMappedMapSerializeDeserialize covers map-valued mapped
// state separately from the ordinary memorymap.Mapping API.
func TestE2EStdMemoryMapMappedMapSerializeDeserialize(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("mapped-variable memorymap A2A test requires Windows Win32 APIs")
	}

	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/maps"
import "std/memorymap"

func printf(format string, ...) int

var concurrent(64, mapped(state)) {
    values map[string]int
}

func main() int {
    path := "std-memorymap-mapped-map.hmb"

    values = make(map[string]int)
    values["answer"] = 42
    values["meaning"] = 7
    serializeErr := memorymap.MappedSerialize(state, path)

    values = make(map[string]int)
    values["answer"] = -1
    deserializeErr := memorymap.MappedDeserialize(state, path)

    printf("SERIALIZE=%d,DESERIALIZE=%d,LEN=%d,ANSWER=%d,MEANING=%d\n",
        serializeErr == nil, deserializeErr == nil, len(values),
        values["answer"], values["meaning"])
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,DESERIALIZE=1,LEN=2,ANSWER=42,MEANING=7",
		ExpectedExit: 0,
	})
}

func TestE2EStdMemoryMapMappedCompositeMapValues(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("mapped-variable memorymap A2A test requires Windows Win32 APIs")
	}

	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/maps"
import "std/memorymap"

func printf(format string, ...) int

type Snapshot struct {
    ID int
    Name string
}

var concurrent(64, mapped(state)) {
    texts map[string]string
    lists map[string][]int
    snapshots map[string]Snapshot
}

func main() int {
    path := "std-memorymap-mapped-composite-maps.hmb"

    texts = make(map[string]string)
    texts["language"] = "hike"
    lists = make(map[string][]int)
    lists["numbers"] = []int{3, 1, 4}
    snapshots = make(map[string]Snapshot)
    snapshots["checkpoint"] = Snapshot{ID: 41, Name: "saved"}
    serializeErr := memorymap.MappedSerialize(state, path)

    texts = make(map[string]string)
    texts["language"] = "destroyed"
    lists = make(map[string][]int)
    lists["numbers"] = []int{0}
    snapshots = make(map[string]Snapshot)
    snapshots["checkpoint"] = Snapshot{ID: -1, Name: "destroyed"}
    deserializeErr := memorymap.MappedDeserialize(state, path)

    value := snapshots["checkpoint"]
    printf("SERIALIZE=%d,DESERIALIZE=%d,TEXT=%s,LIST=%d,%d,%d,STRUCT=%d,%s\n",
        serializeErr == nil, deserializeErr == nil, texts["language"],
        lists["numbers"][0], lists["numbers"][1], lists["numbers"][2],
        value.ID, value.Name)
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,DESERIALIZE=1,TEXT=hike,LIST=3,1,4,STRUCT=41,saved",
		ExpectedExit: 0,
	})
}

func TestE2EStdMemoryMapMappedNestedStructSlices(t *testing.T) {
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

type Entry struct {
    Code int
    Label string
}

type Snapshot struct {
    ID int
    Entries []Entry
}

var concurrent(64, mapped(state)) {
    snapshots []Snapshot
}

func main() int {
    path := "std-memorymap-mapped-nested-struct-slices.hmb"

    snapshots = []Snapshot{
        Snapshot{ID: 10, Entries: []Entry{
            Entry{Code: 101, Label: "first"},
            Entry{Code: 102, Label: "second"},
        }},
        Snapshot{ID: 20, Entries: []Entry{
            Entry{Code: 201, Label: "third"},
        }},
    }
    serializeErr := memorymap.MappedSerialize(state, path)

    snapshots = []Snapshot{
        Snapshot{ID: -1, Entries: []Entry{Entry{Code: -1, Label: "destroyed"}}},
    }
    deserializeErr := memorymap.MappedDeserialize(state, path)

    printf("SERIALIZE=%d,DESERIALIZE=%d,OUTER=%d,ID0=%d,INNER0=%d,CODE0=%d,LABEL0=%s,CODE1=%d,LABEL1=%s,ID1=%d,INNER1=%d\n",
        serializeErr == nil, deserializeErr == nil, len(snapshots),
        snapshots[0].ID, len(snapshots[0].Entries), snapshots[0].Entries[0].Code,
        snapshots[0].Entries[0].Label, snapshots[0].Entries[1].Code,
        snapshots[0].Entries[1].Label, snapshots[1].ID, len(snapshots[1].Entries))
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,DESERIALIZE=1,OUTER=2,ID0=10,INNER0=2,CODE0=101,LABEL0=first,CODE1=102,LABEL1=second,ID1=20,INNER1=1",
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
