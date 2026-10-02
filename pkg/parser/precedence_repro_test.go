package parser

import (
	"testing"

	"hikec-go/pkg/lexer"
)

// This is the smallest expression that exposed the self-hosting failure:
// map-based precedence lookup could miss '&' and make the grouped expression
// parser report "expected ')'" instead of building a binary expression.
func TestBitwisePrecedenceRegression(t *testing.T) {
	source := `package main

func main() int {
    return (5 & 3) + (5 | 3)
}
`

	p := New(lexer.New(source))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("bitwise expression unexpectedly failed to parse: %v", errs)
	}
	if len(program.Decls) != 1 {
		t.Fatalf("parsed declaration count = %d, want 1", len(program.Decls))
	}
}
