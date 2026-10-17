package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

func TestGoHikeStringsCut(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module strings-cut-repro\nhike 0.1.0\n" +
		"replace std => " + filepath.ToSlash(filepath.Join(repoRoot, "std")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "main.hike")
	source := `package main

import "std/strings"

func main() int {
    before, after, found := strings.Cut("name=value", "=")
    if !found || before != "name" || after != "value" { return 1 }
    return 0
}
`
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	target := target.TargetWasm32
	c := New(&target)
	c.SetGoHikeMode(true)
	if _, _, _, err := c.CompileToLLVM(entry); err != nil {
		t.Fatalf("Go-Hike strings.Cut failed: %v\n%s", err, c.Reporter().FormatAll())
	}
}
