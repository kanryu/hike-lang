package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompileForkLoadsRecursiveImports(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"first", "first/nested", "second"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module fork-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"main.hike": `package main
import "./first"
import "./second"
func main() int { return first.Value() + second.Value() }
`,
		"first/first.hike": `package first
import "./nested"
func Value() int { return nested.Value() }
`,
		"first/nested/nested.hike": `package nested
func Value() int { return 1 }
`,
		"second/second.hike": `package second
func Value() int { return 2 }
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	l := New(root)
	l.SetCompileFork(true)
	program, err := l.Load(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Decls) != 4 {
		t.Fatalf("got %d declarations, want main plus three imported functions", len(program.Decls))
	}
}
