package e2e_test

import "testing"

func TestGrammar_ArithmeticAndLogic(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    a := 10 + 20 * 3 - 5 / 2 % 3
    b := !(true && false) || (10 > 20)
    printf("A=%d,B=%d\n", a, b)
    return 0
}
`,
		ExpectedOut:  "A=68,B=1",
		ExpectedExit: 0,
	})
}

func TestGrammar_Bitwise(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    a := 5
    b := 3
    printf("AND=%d,OR=%d,XOR=%d\n", a & b, a | b, a ^ b)
    return 0
}
`,
		ExpectedOut:  "AND=1,OR=7,XOR=6",
		ExpectedExit: 0,
	})
}

func TestGrammar_Generics(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func Max[T](a T, b T) T {
    if a > b {
        return a
    }
    return b
}

func main() int {
    m := Max[int](10, 50)
    printf("MAX=%d\n", m)
    return 0
}
`,
		ExpectedOut:  "MAX=50",
		ExpectedExit: 0,
	})
}
