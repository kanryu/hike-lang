package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

func TestGoHikeLocalStringShadowsGlobalConstantInRange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module local-shadow-repro\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "main.go")
	source := `package main

const version = 1

func scan(version string) int {
    total := 0
    for _, r := range version {
        if r >= 'a' && r <= 'z' { total = total + int(r) }
    }
    return total
}

func main() int { return scan("v") }
`
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	target := target.TargetX86_64Windows
	c := New(&target)
	c.SetGoHikeMode(true)
	ir, _, _, err := c.CompileToLLVM(entry)
	if err != nil {
		t.Fatalf("Go-Hike local shadowing failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	if strings.Contains(ir, "bitcast i64 1 to i8*") {
		t.Fatal("local string parameter was replaced by the global integer constant")
	}
}
