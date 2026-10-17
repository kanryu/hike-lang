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

// TestE2EStdMemoryMapMappedConcurrentSerializeDeserializeInsideLock verifies
// that mapped persistence is available while the concurrent block is locked.
// Ordinary function calls remain rejected inside lock bodies.
func TestE2EStdMemoryMapMappedConcurrentSerializeDeserializeInsideLock(t *testing.T) {
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

var concurrent(64, mapped(state)) {
    count int
}

func main() int {
    path := "std-memorymap-mapped-concurrent-lock.hmb"
    serializeOK := false
    deserializeOK := false

    count = 42
    lock {
        serializeErr := memorymap.MappedSerialize(state, path)
        serializeOK = serializeErr == nil
        count = -1
        deserializeErr := memorymap.MappedDeserialize(state, path)
        deserializeOK = deserializeErr == nil
    }

    printf("IN-LOCK-SERIALIZE=%d,IN-LOCK-DESERIALIZE=%d\n",
        serializeOK, deserializeOK)
    printf("VALUE=%d\n", count)
    return 0
}
`,
		ExpectedOut:  "IN-LOCK-SERIALIZE=1,IN-LOCK-DESERIALIZE=1\nVALUE=42",
		ExpectedExit: 0,
	})
}

func TestE2EStdMemoryMapMappedConcurrentComplexValuesInsideLock(t *testing.T) {
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

type Entry struct {
    Code int
    Label string
}

type Snapshot struct {
    ID int
    Entries []Entry
}

var concurrent(128, mapped(state)) {
    values map[string]int
    snapshots []Snapshot
}

func main() int {
    path := "std-memorymap-mapped-concurrent-complex-lock.hmb"
    serializeOK := false
    deserializeOK := false

    values = make(map[string]int)
    values["answer"] = 42
    snapshots = []Snapshot{
        Snapshot{ID: 10, Entries: []Entry{
            Entry{Code: 101, Label: "first"},
            Entry{Code: 102, Label: "second"},
        }},
    }

    lock {
        serializeErr := memorymap.MappedSerialize(state, path)
        serializeOK = serializeErr == nil
        values["answer"] = -1
        snapshots = []Snapshot{
            Snapshot{ID: -1, Entries: []Entry{Entry{Code: -1, Label: "destroyed"}}},
        }
        deserializeErr := memorymap.MappedDeserialize(state, path)
        deserializeOK = deserializeErr == nil
    }

    printf("SERIALIZE=%d,DESERIALIZE=%d,ANSWER=%d,ID=%d,COUNT=%d,CODE0=%d,LABEL0=%s,CODE1=%d,LABEL1=%s\n",
        serializeOK, deserializeOK, values["answer"], snapshots[0].ID,
        len(snapshots[0].Entries), snapshots[0].Entries[0].Code,
        snapshots[0].Entries[0].Label, snapshots[0].Entries[1].Code,
        snapshots[0].Entries[1].Label)
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,DESERIALIZE=1,ANSWER=42,ID=10,COUNT=2,CODE0=101,LABEL0=first,CODE1=102,LABEL1=second",
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

// TestE2EStdMemoryMapMappedDeepCompositeValues exercises several dynamic
// buffers at once: maps with composite values, maps whose values are slices,
// nested struct slices, nested slices, and strings at multiple levels.
func TestE2EStdMemoryMapMappedDeepCompositeValues(t *testing.T) {
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

type Leaf struct {
    Name string
    Scores []int
}

type Branch struct {
    Title string
    Tags []string
    Leaves []Leaf
    Rows [][]int
}

var concurrent(256, mapped(state)) {
    catalog map[string]Branch
    groups map[string][]Branch
    labels []string
    matrix [][]int
}

func main() int {
    path := "std-memorymap-mapped-deep-composite.hmb"

    catalog = make(map[string]Branch)
    catalog["alpha"] = Branch{
        Title: "first",
        Tags: []string{"red", "green"},
        Leaves: []Leaf{
            Leaf{Name: "a-1", Scores: []int{11, 12, 13}},
            Leaf{Name: "a-2", Scores: []int{21, 22}},
        },
        Rows: [][]int{[]int{101, 102}, []int{201, 202, 203}},
    }
    catalog["beta"] = Branch{
        Title: "second",
        Tags: []string{"blue"},
        Leaves: []Leaf{Leaf{Name: "b-1", Scores: []int{31, 32, 33}}},
        Rows: [][]int{[]int{301, 302}},
    }
    groups = make(map[string][]Branch)
    groups["main"] = []Branch{catalog["alpha"], catalog["beta"]}
    labels = []string{"one", "two", "three"}
    matrix = [][]int{[]int{7, 8, 9}, []int{17, 18, 19}}
    serializeErr := memorymap.MappedSerialize(state, path)

    catalog = make(map[string]Branch)
    catalog["alpha"] = Branch{Title: "destroyed", Tags: []string{"x"}, Leaves: []Leaf{Leaf{Name: "bad", Scores: []int{0}}}, Rows: [][]int{[]int{0}}}
    groups = make(map[string][]Branch)
    groups["main"] = []Branch{Branch{Title: "destroyed"}}
    labels = []string{"destroyed"}
    matrix = [][]int{[]int{0}}
    deserializeErr := memorymap.MappedDeserialize(state, path)

    alpha := catalog["alpha"]
    mainGroup := groups["main"]
    printf("SERIALIZE=%d,DESERIALIZE=%d,CATALOG=%d,TITLE=%s,TAGS=%d,%s,%s,LEAVES=%d,%s,%d,%d,ROWS=%d,%d,%d,GROUPS=%d,%s,%s,LABELS=%d,%s,%s,%s,MATRIX=%d,%d,%d,%d,%d,%d\n",
        serializeErr == nil, deserializeErr == nil, len(catalog), alpha.Title,
        len(alpha.Tags), alpha.Tags[0], alpha.Tags[1], len(alpha.Leaves),
        alpha.Leaves[0].Name, alpha.Leaves[0].Scores[0], alpha.Leaves[1].Scores[1],
        len(alpha.Rows), alpha.Rows[0][1], alpha.Rows[1][2], len(mainGroup),
        mainGroup[0].Title, mainGroup[1].Leaves[0].Name, len(labels), labels[0],
        labels[1], labels[2], len(matrix), matrix[0][2], matrix[1][0],
        matrix[1][2], matrix[0][0], matrix[0][1])
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,DESERIALIZE=1,CATALOG=2,TITLE=first,TAGS=2,red,green,LEAVES=2,a-1,11,22,ROWS=2,102,203,GROUPS=2,first,b-1,LABELS=3,one,two,three,MATRIX=2,9,17,19,7,8",
		ExpectedExit: 0,
	})
}

func TestE2EStdMemoryMapMappedSliceWriteAndAppend(t *testing.T) {
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

var concurrent(64, mapped(state)) {
    values []int
}

func main() int {
    path := "std-memorymap-mapped-slice-write-append.hmb"

    values = []int{1, 2}
    serializeErr := memorymap.MappedSerialize(state, path)

    values = []int{0}
    deserializeErr := memorymap.MappedDeserialize(state, path)
    values[0] = 10
    values = append(values, 3)

    printf("SERIALIZE=%d,DESERIALIZE=%d,LEN=%d,CAP=%d,VALUES=%d,%d,%d\n",
        serializeErr == nil, deserializeErr == nil, len(values), cap(values),
        values[0], values[1], values[2])
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,DESERIALIZE=1,LEN=3,CAP=7,VALUES=10,2,3",
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
