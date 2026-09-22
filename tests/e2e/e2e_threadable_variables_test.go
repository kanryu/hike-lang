package e2e_test

import "testing"

func TestThreadVariablesLockProtectsConcurrentUpdate(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

var concurrent(32) {
    balance int
    version int
}

func main() int {
    lock {
        balance = 100
        version = version + 1
    }
    lock {
        balance = balance + 25
        version = version + 1
    }
    printf("BALANCE=%d,VERSION=%d\n", balance, version)
    return 0
}
`,
		ExpectedOut:  "BALANCE=125,VERSION=2",
		ExpectedExit: 0,
	})
}

func TestThreadVariablesThreadLocalIsolation(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

var threadable(64) {
    workerValue int
}

var concurrent(64) {
    sharedValue int
}

func main() int {
    workerValue = 100
    sharedValue = 77

    first := Async(func() int {
        workerValue = 1
        return workerValue + sharedValue
    })
    second := Async(func() int {
        workerValue = 2
        return workerValue + sharedValue
    })
    third := Async(func() int {
        workerValue = 3
        return workerValue + sharedValue
    })

    firstResult := <-first
    secondResult := <-second
    thirdResult := <-third
    printf("WORKERS=%d,%d,%d,MAIN=%d,SHARED=%d\n", firstResult, secondResult, thirdResult, workerValue, sharedValue)
    return 0
}
`,
		ExpectedOut:  "WORKERS=78,79,80,MAIN=100,SHARED=77",
		ExpectedExit: 0,
	})
}

func TestThreadVariablesConcurrentVisibility(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

var threadable(32) {
    localValue int
}

var concurrent(32) {
    sharedValue int
}

func readShared(seed int) int {
    localValue = seed
    return sharedValue + localValue
}

func main() int {
    sharedValue = 500
    one := <-Async(func() int { return readShared(1) })
    two := <-Async(func() int { return readShared(2) })
    three := <-Async(func() int { return readShared(3) })
    printf("VISIBLE=%d,%d,%d\n", one, two, three)
    return 0
}
`,
		ExpectedOut:  "VISIBLE=501,502,503",
		ExpectedExit: 0,
	})
}

func TestThreadVariablesMultipleWorkerJoin(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

var threadable(16) {
    workerValue int
}

var concurrent(16) {
    completed int
}

func job(value int) int {
    workerValue = value
    completed = completed + 1
    return workerValue
}

func main() int {
    first := Async(func() int { return job(10) })
    second := Async(func() int { return job(20) })
    third := Async(func() int { return job(30) })
    fourth := Async(func() int { return job(40) })

    total := <-first + <-second + <-third + <-fourth
    printf("TOTAL=%d,COMPLETED=%d\n", total, completed)
    return 0
}
`,
		ExpectedOut:  "TOTAL=100,COMPLETED=4",
		ExpectedExit: 0,
	})
}
