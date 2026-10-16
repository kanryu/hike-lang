package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

func TestGoHikeLocalTypeDeclaration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module local-type\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "main.go")
	source := `package main

func value() int {
    type payload struct { value int }
    item := payload{value: 7}
    return item.value
}

func main() int { return value() }
`
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	target := target.TargetX86_64Windows
	c := New(&target)
	c.SetGoHikeMode(true)
	if _, _, _, err := c.CompileToLLVM(entry); err != nil {
		t.Fatalf("Go-Hike local type declaration failed: %v\n%s", err, c.Reporter().FormatAll())
	}
}
