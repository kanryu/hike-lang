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

// TestGoHikeNativeTransformRepro reproduces the self-hosted compiler failure
// that occurs while transforming the compiler package itself.  The failing
// build repeatedly enters transformCallExpr with function=any and never
// produces LLVM IR.
func TestGoHikeNativeTransformRepro(t *testing.T) {
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

	output := filepath.Join(t.TempDir(), "hikec.ll")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nativeCompiler,
		"emit-ir", "-go-hike=1", "-vv", "-o", output,
		filepath.Join("cmd", "hikec", "main.go"),
	)
	cmd.Dir = repoRoot
	combined, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("self-hosted compiler transform timed out: %v\n%s", ctx.Err(), combined)
	}
	if err != nil {
		t.Fatalf("self-hosted compiler transform failed: %v\n%s", err, combined)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("LLVM IR was not produced: %v", err)
	}
}
