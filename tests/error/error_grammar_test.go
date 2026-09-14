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

func TestGrammar_UndefinedFunction(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return missingFunction(1)
}
`, ExpectedError: "undefined: missingFunction"})
}

func TestGrammar_MismatchedBinaryOperands(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return 1 + true
}
`, ExpectedError: "invalid operation"})
}

func TestGrammar_UndefinedReturnValue(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return missingReturnValue
}
`, ExpectedError: "undefined: missingReturnValue"})
}

func TestGrammar_VariableInitializerMismatch(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    var message string = 1
    return 0
}
`, ExpectedError: "cannot use int as string"})
}

func TestGrammar_NonBooleanCondition(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    if 1 { return 1 }
    return 0
}
`, ExpectedError: "cannot use int as bool"})
}

func TestGrammar_MapMakeWithoutImport(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    values := make(map[string]int)
    return len(values)
}
`, ExpectedError: "requires importing 'std/maps'"})
}

func TestGrammar_InvalidPointerOperation(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return *1
}
`, ExpectedError: "cannot dereference"})
}

func TestGrammar_InvalidIndexOperation(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return 1[0]
}
`, ExpectedError: "does not support indexing"})
}

func TestGrammar_UndefinedInitializer(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    value := missingInitializer
    return value
}
`, ExpectedError: "undefined: missingInitializer"})
}

func TestGrammar_BoolInitializerMismatch(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    var enabled bool = 1
    return 0
}
`, ExpectedError: "cannot use int as bool"})
}

func TestGrammar_StringInitializerMismatch(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    var label string = true
    return 0
}
`, ExpectedError: "cannot use int as string"})
}

func TestGrammar_MapVariableWithoutImport(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    values map[string]int
    return 0
}
`, ExpectedError: "requires importing 'std/maps'"})
}

func TestGrammar_MapMakeWithDifferentTypesWithoutImport(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    values := make(map[int]string)
    return 0
}
`, ExpectedError: "requires importing 'std/maps'"})
}

func TestGrammar_UndefinedCallArgument(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return printf(missingFormat)
}
`, ExpectedError: "undefined: printf"})
}

func TestGrammar_UndefinedValueInBinaryExpression(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return knownValue + missingOperand
}
`, ExpectedError: "undefined: knownValue"})
}

func TestGrammar_InvalidArrayIndexTarget(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return true[0]
}
`, ExpectedError: "type 'bool' does not support indexing"})
}

func TestGrammar_InvalidPointerTarget(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return *true
}
`, ExpectedError: "cannot dereference non-pointer type bool"})
}

func TestGrammar_MultipleUndefinedArguments(t *testing.T) {
	RunHikeCompileErrorCase(t, HikeCompileErrorCase{Source: `
package main
func main() int {
    return missingFunction(firstMissing, secondMissing)
}
`, ExpectedErrorLines: []string{
		"undefined: missingFunction",
		"undefined: firstMissing",
		"undefined: secondMissing",
	}})
}
