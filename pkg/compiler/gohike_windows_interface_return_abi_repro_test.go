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

// TestGoHikeNativeInterfaceReturnABIRepro isolates the Windows x64 aggregate
// return path used after lowering an interface method call. The source is
// intentionally small: one concrete receiver is boxed into an interface and
// the interface method returns a 40-byte aggregate on Windows x64.
func TestGoHikeNativeInterfaceReturnABIRepro(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module interface-return-abi-repro\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	const source = `package main

type Payload struct {
    a int
    b int
    c int
    d int
    e int
}

type Factory interface {
    Build() Payload
}

type builder struct{}

func (b *builder) Build() Payload {
    return Payload{a: 1, b: 2, c: 3, d: 4, e: 5}
}

func main() int {
    var f Factory = &builder{}
    p := f.Build()
    return p.a + p.b + p.c + p.d + p.e
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
		t.Fatalf("interface return ABI reproducer timed out: %v\n%s", ctx.Err(), combined)
	}
	if err != nil {
		t.Fatalf("interface return ABI reproducer failed: %v\n%s", err, combined)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("LLVM IR was not produced: %v", err)
	}
}
