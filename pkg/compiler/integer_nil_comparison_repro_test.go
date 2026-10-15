package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

// TestGoHikeInferExprTypeReproducesNullItab reproduces the malformed interface
// value emitted by the native compiler while compiling the self-hosting entry
// point in Go-Hike mode.
func TestGoHikeInferExprTypeReproducesNullItab(t *testing.T) {
	root := t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	modTemplate, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "hikec", "hike.mod"))
	if err != nil {
		t.Fatal(err)
	}
	mod := strings.ReplaceAll(string(modTemplate), "../../pkg", filepath.ToSlash(filepath.Join(repoRoot, "pkg")))
	mod = strings.ReplaceAll(mod, "../../std", filepath.ToSlash(filepath.Join(repoRoot, "std")))
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "main.go")
	source, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "hikec", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, source, 0644); err != nil {
		t.Fatal(err)
	}
	getSource, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "hikec", "get.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "get.go"), getSource, 0644); err != nil {
		t.Fatal(err)
	}
	compiler := New(&target.TargetX86_64Windows)
	compiler.SetGoHikeMode(true)
	compiler.SetCompileFork(true)
	ir, _, _, err := compiler.CompileToLLVM(sourcePath)
	if err != nil {
		t.Fatalf("native compiler failed: %v", err)
	}
	const functionName = "define { i8*, i8* } @sema_Context_ptr_InferExprType("
	if !strings.Contains(ir, functionName) {
		t.Fatalf("reproduction did not emit InferExprType")
	}
	start := strings.Index(ir, functionName)
	end := strings.Index(ir[start:], "\ndefine ")
	if end < 0 {
		t.Fatal("could not delimit infer IR")
	}
	body := ir[start : start+end]
	if bad := strings.Index(body, "i8* null, 1"); bad >= 0 {
		contextStart := bad - 180
		if contextStart < 0 {
			contextStart = 0
		}
		contextEnd := bad + 120
		if contextEnd > len(body) {
			contextEnd = len(body)
		}
		t.Fatalf("native compiler emitted a non-NULL data pointer with a NULL itab; IR context:\n%s", body[contextStart:contextEnd])
	}
}
