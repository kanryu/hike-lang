package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

func TestStableMapUsesEntryIndexAccess(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module stable-map-test\nhike 0.1.0\nreplace std => " + filepath.ToSlash(filepath.Join(repoRoot, "std")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

import "std/maps"

var Metrics = stable map[string]int64{"requests_total": 0, "errors_total": 0}

func main() int {
    Metrics["requests_total"] = 1
    return Metrics["requests_total"]
}
`
	entry := filepath.Join(root, "main.hike")
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	tgt := target.TargetX86_64Windows
	ir, _, _, err := New(&tgt).CompileToLLVM(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ir, "@__hike_cdict_get_index") || !strings.Contains(ir, "@__hike_cdict_set_index") {
		t.Fatalf("stable map did not use entry-index helpers: %s", ir)
	}
}
