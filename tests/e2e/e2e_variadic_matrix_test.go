package e2e_test

import "testing"

func TestVariadicAnyAndStringSliceMatrix(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func countAny(prefix string, values ...any) int {
    return len(values)
}

func countStrings(prefix string, values ...string) int {
    return len(values)
}

func forwardStrings(prefix string, values ...string) int {
    return countStrings(prefix, values...)
}

func main() int {
    emptyAny := countAny("any")
    scalarAny := countAny("any", 1, "two", true)
    anyValues := []any{10, "twenty", false}
    spreadAny := countAny("any", anyValues...)

    emptyStrings := countStrings("string")
    scalarStrings := countStrings("string", "a", "b", "c")
    stringValues := []string{"x", "y"}
    spreadStrings := countStrings("string", stringValues...)
    forwardedStrings := forwardStrings("string", stringValues...)

    printf("ANY=%d,%d,%d;STR=%d,%d,%d,%d\n", emptyAny, scalarAny, spreadAny, emptyStrings, scalarStrings, spreadStrings, forwardedStrings)
    return 0
}
`,
		ExpectedOut:  "ANY=0,3,3;STR=0,3,2,2",
		ExpectedExit: 0,
	})
}
