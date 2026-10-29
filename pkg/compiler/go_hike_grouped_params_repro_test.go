package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

// The self-hosted parser must accept a pointer parameter followed by grouped
// parameters. This is used by pkg/codegen/go/stub.go.
func TestGoHikeGroupedParametersAfterPointerRepro(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module grouped-params\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "main.go")
	source := `package main

type S struct{}

func grouped(value *S, area, offset int) int {
    return area + offset
}

func main() int {
    return grouped(nil, 1, 2) - 3
}
`
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	target := target.TargetX86_64Windows
	c := New(&target)
	c.SetGoHikeMode(true)
	if _, _, _, err := c.CompileToLLVM(entry); err != nil {
		t.Fatalf("grouped parameters after pointer failed: %v\n%s", err, c.Reporter().FormatAll())
	}
}
