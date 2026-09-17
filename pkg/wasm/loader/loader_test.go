package loader

import (
	"hikec-go/pkg/ast"
	"testing"
)

func TestJSONSourceProviderLoadsPackageGraph(t *testing.T) {
	bundle := []byte(`{"main":["package main\nimport \"std/math\"\nfunc main() int { return math_Add(1, 2) }"],"std/math":["package math\nfunc Add(a int, b int) int { return a + b }"]}`)
	provider, err := NewJSONSourceProvider(bundle)
	if err != nil {
		t.Fatal(err)
	}
	program, err := New(provider).Load("main")
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Decls) != 2 {
		t.Fatalf("expected main and imported declarations, got %d", len(program.Decls))
	}
	if _, ok := program.Decls[1].(*ast.FuncDecl); !ok {
		t.Fatalf("expected imported function declaration, got %T", program.Decls[1])
	}
}

func TestLoaderReportsMissingPackage(t *testing.T) {
	provider, err := NewJSONSourceProvider([]byte(`{"main":["package main"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(provider).Load("missing"); err == nil {
		t.Fatal("expected missing package error")
	}
}
