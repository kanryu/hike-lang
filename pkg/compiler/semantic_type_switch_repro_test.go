package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

// TestGoHikeSemanticTypeSwitchesDoNotConfuseASTTypes reproduces the
// self-hosting failure where a semantic Type switch was lowered with an AST
// type of the same name. That produced an AST value boxed as sema.Type and
// later caused invalid dispatch (or an access violation) in the compiler.
func TestGoHikeSemanticTypeSwitchesDoNotConfuseASTTypes(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module semantic-type-switch-repro\nhike 0.1.0\n" +
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
    return sema.SizeOf(sema.TypeInt)
}
`
	entry := filepath.Join(root, "main.hike")
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetX86_64Windows
	c := New(&tgt)
	c.SetGoHikeMode(true)
	ir, _, _, err := c.CompileToLLVM(entry)
	if err != nil {
		t.Fatalf("Go-Hike semantic type-switch reproduction failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	for _, name := range []string{"StructType", "MapType", "SliceType", "PointerType"} {
		wrongItab := "@__itab_ast_" + name + "_sema_Type"
		if strings.Contains(ir, wrongItab) {
			t.Fatalf("generated IR contains AST-as-semantic-Type itab %s", wrongItab)
		}
	}
	if !strings.Contains(ir, "@sema_newSemanticMapType") {
		t.Fatal("generated IR does not use the semantic map-type constructor")
	}
}
