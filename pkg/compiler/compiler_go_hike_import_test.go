package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

// TestGoHikeCompilesImportedTokenPackage exercises the same package boundary
// used by the self-hosting compiler: a Hike entry file imports the independent
// token package, whose implementation is a Go-shaped source file.
func TestGoHikeCompilesImportedTokenPackage(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	mod := "module compiler-import-test\nhike 0.1.0\nGoReplace hikec-go/pkg => " + filepath.ToSlash(filepath.Join(repoRoot, "pkg")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

import "hikec-go/pkg/token"

func main() int {
	if token.LookupIdent("package") == token.PACKAGE {
		return 1
	}
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
		t.Fatalf("Go-Hike import compilation failed: %v\n%s", err, c.Reporter().FormatAll())
	}
}
