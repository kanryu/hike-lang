package e2e_test

import "testing"

func TestParser_Const_SingleIota(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

const First = iota

func main() int {
    printf("FIRST=%d\n", First)
    return 0
}
`,
		ExpectedOut:  "FIRST=0",
		ExpectedExit: 0,
	})
}

func TestParser_Const_GroupIotaInheritance(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

const (
    Zero = iota
    One
    Offset = iota + 10
    Next
)

func main() int {
    printf("IOTA=%d,%d,%d,%d\n", Zero, One, Offset, Next)
    return 0
}
`,
		ExpectedOut:  "IOTA=0,1,12,13",
		ExpectedExit: 0,
	})
}

func TestParser_Const_RepeatedIotaReferences(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

const (
    First = iota
    Second = iota
    Double = iota + 10
    Inherited
)

func main() int {
    printf("IOTA_REPEAT=%d,%d,%d,%d\n", First, Second, Double, Inherited)
    return 0
}
`,
		ExpectedOut:  "IOTA_REPEAT=0,1,12,13",
		ExpectedExit: 0,
	})
}

func TestParser_Const_IotaOffsetMatchingNames(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

const (
    Zero = iota
    One
    Eleven = iota + 9
    Twelve
)

func main() int {
    printf("IOTA_OFFSET=%d,%d,%d,%d\n", Zero, One, Eleven, Twelve)
    return 0
}
`,
		ExpectedOut:  "IOTA_OFFSET=0,1,11,12",
		ExpectedExit: 0,
	})
}
