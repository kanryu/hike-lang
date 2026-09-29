package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E32BitTargetBuild validates the cross-compilation path without
// attempting to execute a binary for a different processor width. Set
// HIKE_E2E_TARGET to linux-x86 or windows-x86 to run it.
func TestE2E32BitTargetBuild(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is required for the 32-bit target E2E build")
	}

	targetName := strings.TrimSpace(os.Getenv("HIKE_E2E_TARGET"))
	if targetName == "" {
		t.Skip("set HIKE_E2E_TARGET to run the target-specific E2E build")
	}

	triple := map[string]string{
		"linux-x86":   "i686-unknown-linux-gnu",
		"windows-x86": "i686-w64-windows-gnu",
	}[targetName]
	if triple == "" {
		t.Fatalf("unsupported HIKE_E2E_TARGET %q", targetName)
	}

	tmpDir := t.TempDir()
	sourcePath := filepath.Join(tmpDir, "main.hike")
	irPath := filepath.Join(tmpDir, "main.ll")
	objectPath := filepath.Join(tmpDir, "main.o")
	source := `package main

func main() int {
    value := 32
    return value + 10
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "hike.mod"), []byte("module e2e-32bit\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	emit := exec.Command(hikecBin, "emit-ir", "-target", targetName, "-o", irPath, sourcePath)
	emit.Dir = tmpDir
	if output, err := emit.CombinedOutput(); err != nil {
		t.Fatalf("32-bit %s IR emission failed: %v\n%s", targetName, err, output)
	}

	ir, err := os.ReadFile(irPath)
	if err != nil {
		t.Fatal(err)
	}
	irText := string(ir)
	if !strings.Contains(irText, `target triple = "`+triple+`"`) {
		t.Fatalf("generated IR has the wrong target triple:\n%s", irText[:min(len(irText), 1000)])
	}
	if !strings.Contains(irText, "@__hike_slice_alloc32") {
		t.Fatal("generated IR does not contain the 32-bit common runtime")
	}

	compile := exec.Command(clang, "--target="+triple, "-c", irPath, "-o", objectPath)
	compile.Dir = tmpDir
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("32-bit %s object build failed: %v\n%s", targetName, err, output)
	}
	if info, err := os.Stat(objectPath); err != nil || info.Size() == 0 {
		t.Fatalf("32-bit object was not produced: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
