package e2e_test

import "testing"

func TestBufferLifetime_FunctionArgumentsAndDerivedViews(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int
func consumeString(label string, value string) int {
    middle := value[1:len(value)-1]
    printf("%s=%s\n", label, middle)
    if len(value) > 2 { branch := value[0:2]; printf("B=%s\n", branch) }
    for i := 0; i < 1; i = i + 1 { loop := value[0:1]; printf("L=%s\n", loop) }
    return len(middle)
}
func consumeSlice(label string, value []int) int {
    middle := value[1:len(value)]
    printf("%s=%d,%d\n", label, middle[0], len(middle))
    return middle[0] + len(middle)
}
func main() int {
    literal := "literal"
    dynamic := string([]byte{'d', 'y', 'n', 'a', 'm', 'i', 'c'})
    literalStringResult := consumeString("SL", literal)
    dynamicStringResult := consumeString("SD", dynamic)
    literalSlice := []int{10, 20, 30}
    dynamicSlice := make([]int, 4, 6)
    dynamicSlice[0] = 40; dynamicSlice[1] = 50; dynamicSlice[2] = 60; dynamicSlice[3] = 70
    literalSliceResult := consumeSlice("VL", literalSlice)
    dynamicSliceResult := consumeSlice("VD", dynamicSlice)
    printf("RESULT=%d,%d,%d,%d\n", literalStringResult, dynamicStringResult, literalSliceResult, dynamicSliceResult)
    return 0
}
`, ExpectedOut: "SL=itera\nB=li\nL=l\nSD=ynami\nB=dy\nL=d\nVL=20,2\nVD=50,3\nRESULT=5,5,22,53\n", ExpectedExit: 0})
}

func TestBufferLifetime_IfBranches(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int
func chooseString(value string, right int) int {
    var part string
    if right != 0 { part = value[2:5] } else { part = value[0:3] }
    printf("PART=%s,LEN=%d\n", part, len(part)); return len(value)
}
func chooseSlice(value []int, right int) int {
    var part []int
    if right != 0 { part = value[2:4] } else { part = value[0:2] }
    printf("SLICE=%d,%d\n", part[0], len(part)); return value[0]
}
func main() int {
    literal := "abcdef"
    dynamic := string([]byte{'u', 'v', 'w', 'x', 'y', 'z'})
    values := make([]int, 4); values[0] = 10; values[1] = 20; values[2] = 30; values[3] = 40
    chooseString(literal, 0); chooseString(dynamic, 1)
    chooseSlice(values, 0); chooseSlice(values, 1)
    printf("DONE=%d\n", len(dynamic)); return 0
}
`, ExpectedOut: "PART=abc,LEN=3\nPART=wxy,LEN=3\nSLICE=10,2\nSLICE=30,2\nDONE=6", ExpectedExit: 0})
}

func TestBufferLifetime_ForLoops(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int
func scanString(value string) int {
    total := 0
    for i := 0; i < len(value)-1; i = i + 1 { part := value[i:i+2]; total = total + len(part) }
    return total
}
func scanSlice(value []int) int {
    total := 0
    for i := 0; i < len(value); i = i + 1 { part := value[i:i+1]; total = total + part[0] }
    return total
}
func main() int {
    text := string([]byte{'l', 'o', 'o', 'p'})
    numbers := make([]int, 3); numbers[0] = 7; numbers[1] = 11; numbers[2] = 13
    printf("LOOP=%d,%d\n", scanString(text), scanSlice(numbers)); return 0
}
`, ExpectedOut: "LOOP=6,31", ExpectedExit: 0})
}
