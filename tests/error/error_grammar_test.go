package error_test

import "testing"

// Hikeコンパイラーでも、同一文の複数エラーと後続文のエラーを収集する。
func TestHikeCompilerCollectsSemanticErrors(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{
		Source: `
package main

func main() int {
    return missingFirst + missingSecond
    return missingAfter
}
`,
		ExpectedErrorLines: []string{
			"undefined: missingFirst",
			"undefined: missingSecond",
			"undefined: missingAfter",
		},
	})
}
