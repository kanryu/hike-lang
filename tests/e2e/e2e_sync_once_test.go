package e2e_test

import "testing"

// TestE2ESyncOnceFunctionValue is a minimal reproducer for the native
// Go-Hike compiler crash in sync.Once.Do: passing a func() value to a method
// and invoking it through the parameter must not jump through a null pointer.
func TestE2ESyncOnceFunctionValue(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/sync"

func printf(format string, ...) int

func main() int {
    var once sync.Once
    once.Do(func() {
        printf("ONCE\n")
    })
    return 0
}
`,
		GoHike:       true,
		ExpectedOut:  "ONCE",
		ExpectedExit: 0,
	})
}
