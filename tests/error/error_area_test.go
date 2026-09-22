package error_test

import "testing"

func TestAreaStringShallowCopyIsRejected(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

func main() int {
    result := ""
    area() {
        value := "temporary"
        result = value
    }
    return 0
}
`,
		ExpectedError: "cannot copy area value string outside its area; use deepcopy",
	})
}

func TestAreaSliceShallowCopyIsRejected(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

func main() int {
    result := []int{}
    area() {
        value := []int{1, 2, 3}
        result = value
    }
    return 0
}
`,
		ExpectedError: "cannot copy area value []int outside its area; use deepcopy",
	})
}

func TestAreaPointerStructShallowCopyIsRejected(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

type Item struct {
    value int
}

func main() int {
    var result *Item
    area() {
        value := &Item{value: 1}
        result = value
    }
    return 0
}
`,
		ExpectedError: "cannot copy area value *Item outside its area; use deepcopy",
	})
}
