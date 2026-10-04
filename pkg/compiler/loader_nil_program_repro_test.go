package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"hikec-go/pkg/target"
)

// A loader failure must not leave a nil AST flowing into semantic analysis.
// The self-hosted compiler previously missed the returned error and crashed
// while dereferencing prog.Imports in sema_configureAnalyzeMode.
func TestCompileToHIRRejectsLoaderErrorBeforeSema(t *testing.T) {
	root := t.TempDir()
	stdDir, err := filepath.Abs(filepath.Join("..", "..", "std"))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module loader-nil-program-test\nhike 0.1.0\nreplace std => " + filepath.ToSlash(stdDir) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

import "std/does-not-exist"

func main() int { return 0 }
`
	entry := filepath.Join(root, "main.hike")
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetWasm32
	c := New(&tgt)
	c.SetGoHikeMode(true)

	hir, _, prog, err := c.CompileToHIR(entry)
	if err == nil {
		t.Fatal("expected loader error for a missing imported package")
	}
	if hir != nil {
		t.Fatal("loader failure must not produce HIR")
	}
	if prog != nil {
		t.Fatal("loader failure must not produce an AST")
	}
}
