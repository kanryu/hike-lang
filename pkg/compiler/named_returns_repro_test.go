package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

func TestGoHikeNamedGroupedReturns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module named-returns\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "main.go")
	source := `package main

func decode(b []byte) (payload, relocs []byte, count int, err error) {
    payload = b
    relocs = b
    count = len(b)
    err = nil
    return payload, relocs, count, err
}

func main() int {
    payload, relocs, count, err := decode([]byte{1, 2, 3})
    if err != nil || len(payload) != 3 || len(relocs) != 3 || count != 3 { return 1 }
    return 0
}
`
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	target := target.TargetX86_64Windows
	c := New(&target)
	c.SetGoHikeMode(true)
	if _, _, _, err := c.CompileToLLVM(entry); err != nil {
		t.Fatalf("Go-Hike named grouped returns failed: %v\n%s", err, c.Reporter().FormatAll())
	}
}
