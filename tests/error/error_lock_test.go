package error_test

import "testing"

func TestNestedLockIsRejected(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

func main() int {
    lock {
        lock {
            return 0
        }
    }
    return 0
}
`,
		ExpectedError: "nested lock blocks are not allowed",
	})
}

func TestFunctionCallInsideLockIsRejected(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

func helper() {}

func main() int {
    lock {
        helper()
    }
    return 0
}
`,
		ExpectedError: "function calls are not allowed inside a lock block",
	})
}
