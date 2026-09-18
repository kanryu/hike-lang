package wabt_test

import "testing"

// These tests cover parser/parser_expr cases whose generated program can be
// checked through the WABT main return value.
func TestWabtParserAndParserExprCoverage(t *testing.T) {
	wasm := buildWabt(t, `package main

func classify(v any) int {
    switch x := v.(type) {
    case int:
        return x
    default:
        return 0
    }
}

func main() int {
    // Omitted for clauses, expression precedence, and multi-case switch.
    sum := 0
    for i := 0; i < 5; {
        sum = sum + i*2
        i = i + 1
    }
    switch sum {
    case 20, 30:
        sum = sum + 7
    default:
        sum = 0
    }
    return classify(sum) + (1 | 2 & 2) + (20 >> (1 + 1))
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=35\n"; got != want {
		t.Fatalf("Wabt parser/parser_expr output = %q, want %q", got, want)
	}
}

func TestWabtLowerGlobalInitializersAndInterfaceBoxing(t *testing.T) {
	wasm := buildWabt(t, `package main

func initial() int { return 40 }
var first int = initial()
var second int = first + 2

type Item struct { value int }

func inspect(v any) int {
    switch x := v.(type) {
    case int:
        return x
    default:
        return 0
    }
}

func main() int {
    return second + inspect(1)
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=43\n"; got != want {
		t.Fatalf("Wabt lower output = %q, want %q", got, want)
	}
}
