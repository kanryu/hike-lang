package wasm_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"hikec-go/tests/testutil"
)

var hikecBin string

func wasmProjectRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(filepath.Join(root, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestMain(m *testing.M) {
	root, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	root, err = filepath.Abs(filepath.Join(root, "..", ".."))
	if err != nil {
		os.Exit(1)
	}
	base, err := os.MkdirTemp(root, ".wasm-test-")
	if err != nil {
		os.Exit(1)
	}
	hikecBin = filepath.Join(base, "hikec.exe")
	if output, err := testutil.BuildHikec(root, hikecBin); err != nil {
		os.Stderr.WriteString("hikec build failed: " + err.Error() + "\n")
		os.Stderr.Write(output)
		_ = os.RemoveAll(base)
		os.Exit(1)
	}
	status := m.Run()
	_ = os.RemoveAll(base)
	os.Exit(status)
}

func buildWasm(t *testing.T, source, mode string) string {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("Clang is required for the WebAssembly E2E test")
	}
	root := wasmProjectRoot(t)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "main.hike")
	if err := os.WriteFile(src, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	wasm := filepath.Join(tmp, "main.wasm")
	args := []string{"build", "-target", "wasm32", "-wasm-mode", mode, "-o", wasm, src}
	build := exec.Command(hikecBin, args...)
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("WASM build failed: %v\n%s", err, output)
	}
	return wasm
}

func runWasm(t *testing.T, wasm, scriptName string) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for the WebAssembly E2E test")
	}
	runner := filepath.Join(wasmProjectRoot(t), "tests", "wasm", "langwasm.js")
	script := filepath.Join(filepath.Dir(runner), scriptName)
	args := []string{runner, wasm, script}
	run := exec.Command("node", args...)
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("Node.js WASM execution failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}
