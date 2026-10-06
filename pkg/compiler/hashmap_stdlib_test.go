package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

func TestBuiltinHashMapUsesLegacyRuntimeWhileMapUsesCompactDict(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module builtin-hashmap-test\nhike 0.1.0\nreplace std => " + filepath.ToSlash(filepath.Join(repoRoot, "std")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

import "std/maps"

func main() int {
    legacy := make(hashmap[string]int, 4)
    legacy["answer"] = 42
    value, ok := legacy["answer"]
    if !ok { return 0 }
    delete(legacy, "answer")
    modern := make(map[string]int, 4)
    modern["answer"] = value
    delete(modern, "answer")
    return value + len(legacy) + len(modern)
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
	if !strings.Contains(ir, "call %struct.__hike_map* @__hike_map_create") {
		t.Fatalf("hashmap did not use legacy runtime create: %s", ir)
	}
	if !strings.Contains(ir, "@__hike_cdict_create") {
		t.Fatal("plain map did not use compact-dict runtime")
	}
}
