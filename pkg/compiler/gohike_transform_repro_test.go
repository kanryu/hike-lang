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

// TestGoHikeNativeCStringSliceRepro isolates the lowerer failure from the
// full self-hosting build: an omitted upper bound on a cstring slice.
func TestGoHikeNativeCStringSliceRepro(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module cstring-slice-repro\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	const source = `package main

func main() {
    raw := cstring("hello")
    _ = raw[:]
}
`
	if err := os.WriteFile(filepath.Join(root, "main.hike"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "main.ll")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nativeCompiler, "emit-ir", "-go-hike=1", "-target", "windows", "-o", output, "main.hike")
	cmd.Dir = root
	combined, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("cstring slice reproducer timed out: %v\n%s", ctx.Err(), combined)
	}
	if err != nil {
		t.Fatalf("cstring slice reproducer failed: %v\n%s", err, combined)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("LLVM IR was not produced: %v", err)
	}
}
