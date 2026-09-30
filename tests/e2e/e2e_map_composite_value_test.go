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

func TestE2EMapCompositeStructValue(t *testing.T) {
	runMapValueCase(t, mapStructValueE2ESource, "MAP_STRUCT=64,native")
}

func TestE2EMapCompositeStructPointerValue(t *testing.T) {
	runMapValueCase(t, mapStructPointerValueE2ESource, "MAP_STRUCT_PTR=64,native")
}
