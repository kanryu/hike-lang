package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

// TestGoHikeStructSemanticWrappersDoNotRecurse reproduces the self-hosting
// failure where the exported Go-Hike helper wrappers for StructType called
// themselves instead of executing the concrete implementation.
func TestGoHikeStructSemanticWrappersDoNotRecurse(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module semantic-struct-wrapper-repro\nhike 0.1.0\n" +
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
		t.Fatalf("Go-Hike StructType wrapper reproduction failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	start := strings.Index(ir, "define { i8*, i32, i32 } @sema_StructType_LLVMType(")
	if start < 0 {
		t.Fatal("generated IR does not contain StructType_LLVMType wrapper")
	}
	end := strings.Index(ir[start:], "\ndefine ")
	if end < 0 {
		t.Fatal("could not isolate StructType_LLVMType wrapper")
	}
	body := ir[start : start+end]
	if strings.Count(body, "@sema_StructType_LLVMType(") > 1 {
		t.Fatalf("StructType_LLVMType wrapper recursively calls itself:\n%s", body)
	}
}
