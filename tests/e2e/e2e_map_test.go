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

const mapStringKeyViewE2ESource = `package main

import "std/maps"

func printf(format string, ...)

func main() int {
    values := make(map[string]int)
    left := "compiler:left"
    right := "compiler:right"
    values[left[:8]] = 42
    // Equal substring views must compare by visible bytes and length, not by
    // backing pointer or by bytes after the view.
    if values[right[:8]] != 42 { return 1 }
    printf("MAP_STRING_KEY_VIEW=%d\n", values[right[:8]])
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

func TestE2EMapCompositeValues(t *testing.T) {
	const source = `package main

import "std/maps"

type Config struct { Bits int; Name string }

func printf(format string, ...)

func main() int {
    values := make(map[string][]int)
    values["numbers"] = append(values["numbers"], 10)
    values["numbers"] = append(values["numbers"], 20)
    loaded := values["numbers"]
    if len(loaded) != 2 || loaded[0] != 10 || loaded[1] != 20 { return 1 }
    printf("MAP_SLICE=%d,%d\n", loaded[0], loaded[1])
    for key, item := range values {
        if key != "numbers" || len(item) != 2 || item[0] != 10 || item[1] != 20 { return 1 }
        printf("MAP_RANGE_SLICE=%s,%d,%d\n", key, item[0], item[1])
    }

    strings := make(map[string]string)
    strings["language"] = "hike"
    if strings["language"] != "hike" || strings["missing"] != "" { return 1 }
    printf("MAP_STRING=%s\n", strings["language"])
    for key, item := range strings {
        if key != "language" || item != "hike" { return 1 }
        printf("MAP_RANGE_STRING=%s,%s\n", key, item)
    }

    missing := make(map[string][]int)["missing"]
    if len(missing) != 0 || cap(missing) != 0 { return 1 }
    printf("MAP_MISSING_SLICE_GET=%d,%d\n", len(missing), cap(missing))

    config := Config{Bits: 64, Name: "native"}
    configs := make(map[string]Config)
    configs["default"] = config
    if configs["default"].Bits != 64 || configs["missing"].Bits != 0 { return 1 }
    printf("MAP_STRUCT=%d,%s\n", configs["default"].Bits, configs["default"].Name)
    for key, item := range configs {
        if key != "default" || item.Bits != 64 || item.Name != "native" { return 1 }
        printf("MAP_RANGE_STRUCT=%s,%d,%s\n", key, item.Bits, item.Name)
    }

    pointers := make(map[string]*Config)
    pointers["default"] = &config
    if pointers["default"].Bits != 64 || pointers["missing"] != nil { return 1 }
    found, ok := pointers["default"]
    if !ok || found == nil || found.Bits != 64 { return 1 }
    missingPtr, missingOK := pointers["missing"]
    if missingOK || missingPtr != nil { return 1 }
    printf("MAP_STRUCT_PTR=%d,%s\n", pointers["default"].Bits, pointers["default"].Name)
    for _, item := range pointers {
        if item == nil || item.Bits != 64 || item.Name != "native" { return 1 }
        printf("MAP_RANGE_STRUCT_PTR=%d,%s\n", item.Bits, item.Name)
    }
    return 0
}

`
	const output = "MAP_SLICE=10,20\nMAP_RANGE_SLICE=numbers,10,20\nMAP_STRING=hike\nMAP_RANGE_STRING=language,hike\nMAP_MISSING_SLICE_GET=0,0\nMAP_STRUCT=64,native\nMAP_RANGE_STRUCT=default,64,native\nMAP_STRUCT_PTR=64,native\nMAP_RANGE_STRUCT_PTR=64,native"
	for _, goHike := range []bool{false, true} {
		name := "go"
		if goHike {
			name = "go-hike"
		}
		t.Run(name, func(t *testing.T) {
			RunHikeCase(t, HikeTestCase{Source: source, GoHike: goHike, ExpectedOut: output, ExpectedExit: 0})
		})
	}
}

func TestE2EMapStringKeyViews(t *testing.T) {
	const output = "MAP_STRING_KEY_VIEW=42\n"
	for _, goHike := range []bool{false, true} {
		name := "go"
		if goHike {
			name = "go-hike"
		}
		t.Run(name, func(t *testing.T) {
			RunHikeCase(t, HikeTestCase{
				Source: mapStringKeyViewE2ESource, GoHike: goHike,
				ExpectedOut: output, ExpectedExit: 0,
			})
		})
	}
}

func TestE2EMapStringRangePreservesKeysAndValues(t *testing.T) {
	const source = `package main

import "std/maps"

func printf(format string, ...)

func main() int {
    replaces := make(map[string]string)
    replaces["fmt"] = "../../std/fmt"
    replaces["os"] = "../../std/os"

    found := false
    for module, target := range replaces {
        if module == "fmt" && target == "../../std/fmt" {
            found = true
        }
    }
    if !found { return 1 }
    printf("MAP_STRING_RANGE=%d\n", len(replaces))
    return 0
}
`

	for _, goHike := range []bool{false, true} {
		name := "go"
		if goHike {
			name = "go-hike"
		}
		t.Run(name, func(t *testing.T) {
			RunHikeCase(t, HikeTestCase{
				Source: source, GoHike: goHike,
				ExpectedOut: "MAP_STRING_RANGE=2\n", ExpectedExit: 0,
			})
		})
	}
}

func TestE2EMapDeleteAndLen(t *testing.T) {
	const source = `package main

import "std/maps"

func printf(format string, ...) int

func main() int {
    m := make(map[string]int)
    m["alpha"] = 1
    m["beta"] = 2
    m["gamma"] = 3
    lenBefore := len(m)

    delete(m, "beta")
    lenAfter := len(m)

    printf("BEFORE=%d,AFTER=%d,A=%d,B=%d,G=%d\n", lenBefore, lenAfter, m["alpha"], m["beta"], m["gamma"])
    return 0
}
`
	runMapValueCase(t, source, "BEFORE=3,AFTER=2,A=1,B=0,G=3")
}

func TestE2EMapLiteralInitialization(t *testing.T) {
	const source = `package main

import "std/maps"

func printf(format string, ...) int

func main() int {
    var values = map[string]int{
        "alpha": 10,
        "beta": 20,
    }
    values["gamma"] = 30
    printf("LEN=%d,A=%d,B=%d,G=%d\n", len(values), values["alpha"], values["beta"], values["gamma"])
    return 0
}
`
	runMapValueCase(t, source, "LEN=3,A=10,B=20,G=30")
}

func TestE2EMapStringRangePreservesInsertionOrder(t *testing.T) {
	const source = `package main

import "std/maps"

func printf(format string, ...) int

func main() int {
    values := make(map[string]string)
    values["first"] = "one"
    values["second"] = "two"
    values["third"] = "three"
    for key, value := range values {
        printf("%s=%s\n", key, value)
    }
    return 0
}
`
	runMapValueCase(t, source, "first=one\nsecond=two\nthird=three")
}
