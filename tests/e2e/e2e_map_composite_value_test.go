package e2e_test

import "testing"

const mapSliceValueE2ESource = `package main

import "std/maps"

func printf(format string, ...)

func main() int {
    values := make([]int, 0)
    values = append(values, 10, 20)
    grouped := make(map[string][]int)
    grouped["numbers"] = values
    loaded := grouped["numbers"]
    if len(loaded) != 2 || loaded[0] != 10 || loaded[1] != 20 { return 1 }
    printf("MAP_SLICE=%d,%d\n", loaded[0], loaded[1])
    return 0
}
`

const mapStringValueE2ESource = `package main

import "std/maps"

func printf(format string, ...)

func main() int {
    values := make(map[string]string)
    values["language"] = "hike"
    loaded := values["language"]
    if loaded != "hike" { return 1 }
    printf("MAP_STRING=%s\n", loaded)
    return 0
}
`

const mapMissingSliceAppendE2ESource = `package main

import "std/maps"

func printf(format string, ...)

func main() int {
    values := make(map[string][]int)
    values["numbers"] = append(values["numbers"], 10)
    values["numbers"] = append(values["numbers"], 20)
    loaded := values["numbers"]
    if len(loaded) != 2 || loaded[0] != 10 || loaded[1] != 20 { return 1 }
    printf("MAP_MISSING_SLICE_APPEND=%d,%d\n", loaded[0], loaded[1])
    return 0
}
`

const mapMissingSliceGetE2ESource = `package main

import "std/maps"

func printf(format string, ...)

func main() int {
    values := make(map[string][]int)
    loaded := values["missing"]
    if len(loaded) != 0 || cap(loaded) != 0 { return 1 }
    printf("MAP_MISSING_SLICE_GET=%d,%d\n", len(loaded), cap(loaded))
    return 0
}
`

const mapMissingStringGetE2ESource = `package main

import "std/maps"

func printf(format string, ...)

func main() int {
    values := make(map[string]string)
    loaded := values["missing"]
    if loaded != "" { return 1 }
    printf("MAP_MISSING_STRING_GET=%d\n", len(loaded))
    return 0
}
`

const mapMissingStructGetE2ESource = `package main

import "std/maps"

type Config struct { Bits int; Name string }

func printf(format string, ...)

func main() int {
    values := make(map[string]Config)
    loaded := values["missing"]
    if loaded.Bits != 0 || loaded.Name != "" { return 1 }
    printf("MAP_MISSING_STRUCT_GET=%d,%d\n", loaded.Bits, len(loaded.Name))
    return 0
}
`

const mapMissingStructPointerGetE2ESource = `package main

import "std/maps"

type Config struct { Bits int; Name string }

func printf(format string, ...)

func main() int {
    values := make(map[string]*Config)
    loaded := values["missing"]
    if loaded != nil { return 1 }
    printf("MAP_MISSING_STRUCT_PTR_GET=0\n")
    return 0
}
`

const mapStructValueE2ESource = `package main

import "std/maps"

type Config struct { Bits int; Name string }

func printf(format string, ...)

func main() int {
    values := make(map[string]Config)
    values["default"] = Config{Bits: 64, Name: "native"}
    loaded := values["default"]
    if loaded.Bits != 64 || loaded.Name != "native" { return 1 }
    printf("MAP_STRUCT=%d,%s\n", loaded.Bits, loaded.Name)
    return 0
}
`

const mapStructPointerValueE2ESource = `package main

import "std/maps"

type Config struct { Bits int; Name string }

func printf(format string, ...)

func main() int {
    config := Config{Bits: 64, Name: "native"}
    values := make(map[string]*Config)
    values["default"] = &config
    loaded := values["default"]
    if loaded.Bits != 64 || loaded.Name != "native" { return 1 }
    printf("MAP_STRUCT_PTR=%d,%s\n", loaded.Bits, loaded.Name)
    return 0
}
`

const mapRangeStructPointerValueE2ESource = `package main

import "std/maps"

type Config struct { Bits int; Name string }

func printf(format string, ...)

func main() int {
    config := Config{Bits: 64, Name: "native"}
    values := make(map[string]*Config)
    values["default"] = &config
    for _, loaded := range values {
        if loaded == nil || loaded.Bits != 64 || loaded.Name != "native" { return 1 }
        printf("MAP_RANGE_STRUCT_PTR=%d,%s\n", loaded.Bits, loaded.Name)
    }
    return 0
}
`

func runMapValueCase(t *testing.T, source, output string) {
	t.Helper()
	for _, goHike := range []bool{false, true} {
		name := "go"
		if goHike {
			name = "go-hike"
		}
		t.Run(name, func(t *testing.T) {
			RunHikeCase(t, HikeTestCase{
				Source: source, GoHike: goHike,
				ExpectedOut: output, ExpectedExit: 0,
			})
		})
	}
}

func TestE2EMapCompositeSliceValue(t *testing.T) {
	runMapValueCase(t, mapSliceValueE2ESource, "MAP_SLICE=10,20")
}

func TestE2EMapCompositeStringValue(t *testing.T) {
	runMapValueCase(t, mapStringValueE2ESource, "MAP_STRING=hike")
}

func TestE2EMapMissingSliceAppend(t *testing.T) {
	runMapValueCase(t, mapMissingSliceAppendE2ESource, "MAP_MISSING_SLICE_APPEND=10,20")
}

func TestE2EMapMissingSliceGet(t *testing.T) {
	runMapValueCase(t, mapMissingSliceGetE2ESource, "MAP_MISSING_SLICE_GET=0,0")
}

func TestE2EMapMissingStringGet(t *testing.T) {
	runMapValueCase(t, mapMissingStringGetE2ESource, "MAP_MISSING_STRING_GET=0")
}

func TestE2EMapMissingStructGet(t *testing.T) {
	runMapValueCase(t, mapMissingStructGetE2ESource, "MAP_MISSING_STRUCT_GET=0,0")
}

func TestE2EMapMissingStructPointerGet(t *testing.T) {
	runMapValueCase(t, mapMissingStructPointerGetE2ESource, "MAP_MISSING_STRUCT_PTR_GET=0")
}

func TestE2EMapCompositeStructValue(t *testing.T) {
	runMapValueCase(t, mapStructValueE2ESource, "MAP_STRUCT=64,native")
}

func TestE2EMapCompositeStructPointerValue(t *testing.T) {
	runMapValueCase(t, mapStructPointerValueE2ESource, "MAP_STRUCT_PTR=64,native")
}

func TestE2EMapRangeCompositeStructPointerValue(t *testing.T) {
	runMapValueCase(t, mapRangeStructPointerValueE2ESource, "MAP_RANGE_STRUCT_PTR=64,native")
}
