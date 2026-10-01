package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const importedPointerTypeMain = `package main

import "hikec-go/pkg/backend/llvm"

func main() int {
	return 0
}
`

// TestE2ENativeImportedLLVMBackend is a minimal reproducer for the native
// compiler failure. Importing the LLVM backend must compile and emit IR; its
// runtime template call must not select a same-named function from another
// package during native lowering.
func TestE2ENativeImportedLLVMBackend(t *testing.T) {
	if !useNativeBins {
		t.Skip("requires a native HikeC binary")
	}

	tmpDir := t.TempDir()
	modSource, err := os.ReadFile(filepath.Join(projectRoot, "cmd", "hikec", "hike.mod"))
	if err != nil {
		t.Fatal(err)
	}
	stdRoot := filepath.ToSlash(filepath.Join(projectRoot, "std"))
	mod := strings.ReplaceAll(string(modSource), "../../std", stdRoot)
	mod = strings.ReplaceAll(mod, "../../pkg", filepath.ToSlash(filepath.Join(projectRoot, "pkg")))
	mod = strings.Replace(mod, "module hikec-go", "module repro", 1)
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(importedPointerTypeMain), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(tmpDir, "main.ll")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, hikecBin, "emit-ir", "-go-hike=1", "-o", output, "main.go")
	cmd.Dir = tmpDir
	combined, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("minimal imported compiler package program timed out:\n%s", tailOutput(combined))
	}
	if err != nil {
		t.Fatalf("minimal imported compiler package program failed: %v\n%s", err, tailOutput(combined))
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("EmitIR did not create %s: %v", output, err)
	}
}

func tailOutput(data []byte) string {
	const maxBytes = 8192
	if len(data) <= maxBytes {
		return string(data)
	}
	return "...<truncated>...\n" + string(data[len(data)-maxBytes:])
}
