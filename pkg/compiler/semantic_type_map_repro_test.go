package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

// TestGoHikeSemanticTypeMapValueRoundTrip reproduces the self-hosting path
// that stores a semantic Type interface in a map and reads it back.
func TestGoHikeSemanticTypeMapValueRoundTrip(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module semantic-type-map-repro\nhike 0.1.0\n" +
		"GoReplace hikec-go/pkg => " + filepath.ToSlash(filepath.Join(repoRoot, "pkg")) + "\n" +
		"replace std => " + filepath.ToSlash(filepath.Join(repoRoot, "std")) + "\n" +
		"GoReplace fmt => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "fmt")) + "\n"
	stdEntries, err := os.ReadDir(filepath.Join(repoRoot, "std"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range stdEntries {
		if entry.IsDir() {
			mod += "GoReplace " + entry.Name() + " => " + filepath.ToSlash(filepath.Join(repoRoot, "std", entry.Name())) + "\n"
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}

	source := `package main

import "hikec-go/pkg/sema"

func main() int {
    values := make(map[string]sema.Type)
    values["int"] = sema.TypeInt
    return sema.SizeOf(values["int"])
}
`
	entry := filepath.Join(root, "main.hike")
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetX86_64Windows
	c := New(&tgt)
	c.SetGoHikeMode(true)
	if _, _, _, err := c.CompileToLLVM(entry); err != nil {
		t.Fatalf("Go-Hike semantic Type map reproduction failed: %v\n%s", err, c.Reporter().FormatAll())
	}
}
