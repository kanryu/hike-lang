package sema

import (
	"strings"
	"testing"

	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

func analyzeStableMapTest(t *testing.T, source string) (*Context, error) {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) != 0 {
		t.Fatalf("parse errors: %s", strings.Join(errs, "; "))
	}
	return AnalyzeMode(program, true)
}

func TestStableMapRequiresDeclaredKeysAndAllowsOverwrite(t *testing.T) {
	ctx, err := analyzeStableMapTest(t, `
package main

var Metrics = stable map[string]int64{"requests_total": 0, "errors_total": 0}

func main() int {
    Metrics["requests_total"] = 1
    return Metrics["requests_total"]
}
`)
	if err != nil {
		t.Fatalf("stable map analysis failed: %v", err)
	}
	if _, ok := ctx.StableMapKeys["Metrics"]["s:requests_total"]; !ok {
		t.Fatal("stable map key set was not recorded")
	}
}

func TestStableMapRejectsKeySetMutation(t *testing.T) {
	for name, op := range map[string]string{
		"unknown assignment": `Metrics["new"] = 1`,
		"delete":             `delete(Metrics, "requests_total")`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := analyzeStableMapTest(t, "package main\n\nvar Metrics = stable map[string]int64{\"requests_total\": 0}\n\nfunc main() int {\n    "+op+"\n    return 0\n}\n")
			if err == nil {
				t.Fatal("expected stable map mutation to be rejected")
			}
		})
	}
}
