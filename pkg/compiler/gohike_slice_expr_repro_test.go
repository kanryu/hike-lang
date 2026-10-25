package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestGoHikeNativeSliceExprRepro(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the reproducer currently targets the Windows native binary")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	nativeCompiler := os.Getenv("HIKE_GO_HIKE_BIN")
	if nativeCompiler == "" {
		nativeCompiler = filepath.Join(repoRoot, "bin", "hikec-gohike.exe")
	}
	if _, err := os.Stat(nativeCompiler); err != nil {
		t.Skipf("self-hosted compiler binary is unavailable: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module slice-expr-repro\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(repoRoot, "pkg", "compiler", "testdata", "slice-expr-repro", "main.hike"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.hike"), source, 0644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "main.ll")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nativeCompiler, "emit-ir", "-go-hike=1", "-o", output, "main.hike")
	cmd.Dir = root
	combined, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("slice expression reproducer timed out: %v\n%s", ctx.Err(), combined)
	}
	if err != nil {
		t.Fatalf("slice expression caused self-hosted compiler failure: %v\n%s", err, combined)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("LLVM IR was not produced: %v", err)
	}
}
