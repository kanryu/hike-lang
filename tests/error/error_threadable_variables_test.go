package error_test

import "testing"

func TestThreadableVariableMustStartWithLowercase(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

var threadable(64) {
    WorkerValue int
}

func main() int { return 0 }
`,
		ExpectedError: "memory block variable \"WorkerValue\" must start with a lowercase letter",
	})
}

func TestConcurrentVariableMustStartWithLowercase(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

var concurrent(64) {
    SharedValue int
}

func main() int { return 0 }
`,
		ExpectedError: "memory block variable \"SharedValue\" must start with a lowercase letter",
	})
}
