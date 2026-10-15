package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

// TestGoHikeJSONMarshalIndentIsAvailable reproduces the Build1 failure where
// the Hike replacement for std/encoding/json no longer exposed the API used
// by the compiler's Go-shaped packages.
func TestGoHikeJSONMarshalIndentIsAvailable(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module json-marshal-indent-repro\nhike 0.1.0\n" +
		"replace std => " + filepath.ToSlash(filepath.Join(repoRoot, "std")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

import "std/encoding/json"

func main() int {
	json.NewEncoder(nil)
    json.MarshalIndent(json.NewNull(), "", "  ")
    return 0
}
`
	entry := filepath.Join(root, "main.hike")
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetWasm32
	c := New(&tgt)
	c.SetGoHikeMode(true)
	if _, _, _, err := c.CompileToLLVM(entry); err != nil {
		t.Fatalf("Go-Hike JSON MarshalIndent reproduction failed: %v\n%s", err, c.Reporter().FormatAll())
	}
}
