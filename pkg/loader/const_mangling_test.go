package loader

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/ast"
)

func TestLoadQualifiesImportedConstReferences(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module const-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "values"), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"main.hike": `package main
import "./values"
func main() int { return values.Value() }
`,
		"values/values.hike": `package values
const Answer = 42
func Value() int { return Answer }
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	program, err := New(root).Load(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range program.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Value != "values_Value" {
			continue
		}
		if fn.Body == nil || len(fn.Body.Statements) != 1 {
			t.Fatalf("values_Value body was not loaded as expected")
		}
		ret, ok := fn.Body.Statements[0].(*ast.ReturnStmt)
		if !ok || len(ret.Values) != 1 {
			t.Fatalf("values_Value return was not loaded as expected")
		}
		id, ok := ret.Values[0].(*ast.Identifier)
		if !ok || id.Value != "values_Answer" {
			t.Fatalf("constant reference = %#v, want values_Answer", ret.Values[0])
		}
		return
	}
	t.Fatal("values_Value was not found")
}
