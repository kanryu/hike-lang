package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

func TestGoHikeCompilesFilepathGlob(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module filepath-glob-test\nhike 0.1.0\nGoReplace std => " + filepath.ToSlash(filepath.Join(repoRoot, "std")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "main.hike")
	source := `package main

import "std/path/filepath"

func main() int {
	paths, err := filepath.Glob("*.hike")
	if err != nil { return 1 }
	return len(paths)
}
`
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetWasm32
	c := New(&tgt)
	c.SetGoHikeMode(true)
	if _, _, _, err := c.CompileToLLVM(entry); err != nil {
		t.Fatalf("Go-Hike filepath.Glob compilation failed: %v\n%s", err, c.Reporter().FormatAll())
	}
}
