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

// TestE2ESyncOnceNamedFunctionValue covers the named-function path used by
// the compiler's logger initialization callback.
func TestE2ESyncOnceNamedFunctionValue(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/sync"

func printf(format string, ...) int

func initOnce() {
    printf("NAMED_ONCE\n")
}

func main() int {
    var once sync.Once
    once.Do(initOnce)
    return 0
}
`,
		GoHike:       true,
		ExpectedOut:  "NAMED_ONCE",
		ExpectedExit: 0,
	})
}

// TestE2ESyncOnceGlobalNamedFunctionValue is the smallest reproduction of the
// logger path: a package global Once invokes a named package function through
// a method parameter.
func TestE2ESyncOnceGlobalNamedFunctionValue(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/sync"

func printf(format string, ...) int

var levelInit sync.Once

func initLevel() {
    printf("GLOBAL_INIT\n")
}

func getLevel() int {
    levelInit.Do(initLevel)
    return 2
}

func main() int {
    getLevel()
    return 0
}
`,
		GoHike:       true,
		ExpectedOut:  "GLOBAL_INIT",
		ExpectedExit: 0,
	})
}

// TestE2ESyncOnceImportedGoMethodFunctionValue covers the Go-Hike package
// boundary where an imported method's semantic signature includes its
// receiver while the source template contains only explicit arguments.
func TestE2ESyncOnceImportedGoMethodFunctionValue(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "test-runner/helper"

func printf(format string, ...) int

var once helper.Once

func callback() {
    printf("IMPORTED_ONCE\n")
}

func main() int {
    once.Do(callback)
    return 0
}
`,
		Files: map[string]string{
			"helper/helper.go": `package helper

type Once struct{}

func (o *Once) Do(fn func()) {
    fn()
}
`,
		},
		GoHike:       true,
		ExpectedOut:  "IMPORTED_ONCE",
		ExpectedExit: 0,
	})
}
