package loader

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/ast"
)

func TestLoadPreservesPackageDiscoveryOrder(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"first", "second"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module order-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.hike"), []byte("package main\n\nimport \"order-test/first\"\nimport \"order-test/second\"\n\nfunc main() int { return 0 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "first", "first.hike"), []byte("package first\n\ntype First struct {}\n\nfunc (f *First) Value() int { return 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "second", "second.hike"), []byte("package second\n\ntype Second struct {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	program, err := New(root).Load(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	var names []string
	for _, decl := range program.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			names = append(names, td.Name.Value)
		}
	}
	want := []string{"first_First", "second_Second"}
	if len(names) != len(want) {
		t.Fatalf("type declaration order = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("type declaration order = %v, want %v", names, want)
		}
	}
	var method *ast.FuncDecl
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Receiver != nil {
			method = fn
			break
		}
	}
	if method == nil {
		t.Fatal("first package method was not loaded")
	}
	receiver, ok := method.Receiver.Type.(*ast.PointerType)
	if !ok {
		t.Fatalf("method receiver = %T, want *ast.PointerType", method.Receiver.Type)
	}
	named, ok := receiver.Base.(*ast.NamedType)
	if !ok || named.Name.Value != "first_First" {
		t.Fatalf("method receiver base = %v, want first_First", method.Receiver.Type)
	}
}
