package e2e_test

import "testing"

// All lifetime cases intentionally run in one Hike process. CASE markers
// preserve failure localization while avoiding one compiler/linker/process
// startup for every ownership pattern.
func TestBufferLifetime_Batch(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main

func printf(format string, ...) int

func consumeString(value string) int {
    middle := value[1:len(value)-1]
    if len(value) > 2 { branch := value[0:2]; printf("CASE=string-branch:%s\n", branch) }
    for i := 0; i < 1; i = i + 1 { loop := value[0:1]; printf("CASE=string-loop:%s\n", loop) }
    return len(middle)
}
func consumeSlice(value []int) int {
    middle := value[1:len(value)]
    if len(value) > 2 { branch := value[0:2]; printf("CASE=slice-branch:%d\n", branch[0]) }
    for i := 0; i < 1; i = i + 1 { loop := value[0:1]; printf("CASE=slice-loop:%d\n", loop[0]) }
    return middle[0] + len(middle)
}
func chooseString(value string, right int) int { var part string; if right != 0 { part = value[2:5] } else { part = value[0:3] }; printf("CASE=if-string:%s,%d\n", part, len(part)); return len(value) }
func chooseSlice(value []int, right int) int { var part []int; if right != 0 { part = value[2:4] } else { part = value[0:2] }; printf("CASE=if-slice:%d,%d\n", part[0], len(part)); return value[0] }
func scanString(value string) int { total := 0; for i := 0; i < len(value)-1; i = i + 1 { part := value[i:i+2]; total = total + len(part) }; return total }
func scanSlice(value []int) int { total := 0; for i := 0; i < len(value); i = i + 1 { part := value[i:i+1]; total = total + part[0] }; return total }
func consumeCString(raw cstring) int { value := string(raw); part := value[1:len(value)-1]; printf("CASE=cstring:%s,%d\n", part, len(part)); return len(part) }

cfunc fromCString(input *byte, length int) cstring { raw := cstring(input, length); value := string(raw); if len(value) > 2 { value = value[1:len(value)-1] }; return cstring(value + "!") }

func rawPointerSlice() int { data := make([]byte, 4); data[0] = 10; data[1] = 20; data[2] = 30; data[3] = 40; ptr := &data[0]; var view []byte; if ptr != nil { view = ptr[1:3] }; printf("CASE=raw:%d,%d,%d\n", view[0], view[1], len(view)); return view[0] }
func joinPath(a string, b string) string { return a + "/" + b }
func appendNil() []int { var values []int; values = append(values, 1, 2); return values }
func appendGrow(values []int) []int { values = append(values, 30, 40); values = append(values, 50, 60, 70); return values }
func appendVariadic(values []int, extra []int) []int { return append(values, extra...) }

func main() int {
    literal := "literal"
    dynamic := string([]byte{'d','y','n','a','m','i','c'})
    literalSlice := []int{10, 20, 30}
    dynamicSlice := make([]int, 4, 6)
    dynamicSlice[0] = 40; dynamicSlice[1] = 50; dynamicSlice[2] = 60; dynamicSlice[3] = 70
    printf("CASE=args:%d,%d,%d,%d\n", consumeString(literal), consumeString(dynamic), consumeSlice(literalSlice), consumeSlice(dynamicSlice))
	chooseString(literal, 0); chooseString(dynamic, 1); chooseSlice(literalSlice, 0); chooseSlice(dynamicSlice, 1)
    printf("CASE=loops:%d,%d\n", scanString("loop"), scanSlice([]int{7,11,13}))
    consumeCString(cstring("headered"))
    data := []byte{'a','b','c','d'}; converted := fromCString(&data[0], 4); printf("CASE=cfunc:%s,%d\n", converted, len(converted)); rawPointerSlice()
    path1 := joinPath(literal, "child"); path2 := joinPath(dynamic, "child"); printf("CASE=return-string:%d,%d\n", len(path1), len(path2))
    nilValues := appendNil(); base := make([]int, 2, 2); base[0] = 10; base[1] = 20; grown := appendGrow(base); variadic := appendVariadic(grown, []int{80, 90})
    printf("CASE=append:%d,%d,%d,%d\n", nilValues[0], len(nilValues), variadic[1], len(variadic))
    return 0
}
`, ExpectedOut: "CASE=string-branch:li\nCASE=string-loop:l\nCASE=string-branch:dy\nCASE=string-loop:d\nCASE=slice-branch:10\nCASE=slice-loop:10\nCASE=slice-branch:40\nCASE=slice-loop:40\nCASE=args:5,5,22,53\nCASE=if-string:lit,3\nCASE=if-string:nam,3\nCASE=if-slice:10,2\nCASE=if-slice:60,2\nCASE=loops:6,31\nCASE=cstring:eadere,6\nCASE=cfunc:bc!,3\nCASE=raw:20,30,2\nCASE=return-string:13,13\nCASE=append:1,2,20,9\n", ExpectedExit: 0})
}
