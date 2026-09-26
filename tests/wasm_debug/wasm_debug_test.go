package wasm_debug_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

var hikecPath string

func TestMain(m *testing.M) {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tmp, err := os.MkdirTemp("", "hike-wasm-debug-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	hikecPath = filepath.Join(tmp, "hikec")
	if os.PathSeparator == '\\' {
		hikecPath += ".exe"
	}
	if output, err := buildHikec(root, hikecPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build hikec: %v\n%s", err, output)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func findProjectRoot() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root, nil
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", fmt.Errorf("could not find project root")
		}
		root = parent
	}
}

func buildHikec(root, output string) ([]byte, error) {
	cmd := exec.Command("go", "build", "-o", output, filepath.Join(root, "cmd", "hikec"))
	cmd.Dir = root
	return cmd.CombinedOutput()
}

func TestWabtWasmtimeDebugStepsReturnAndReadsValue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Wasmtime JIT guest debugging is not available in the Windows test environment")
	}
	for _, tool := range []string{"clang", "wat2wasm", "wasmtime", "lldb"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required for the Wasmtime debug test", tool)
		}
	}
	tmp := t.TempDir()
	source := `package main

func calculate(a int, b int) int {
    A := a + 10
    B := b * 2
    return A + B
}

func main() int {
    return calculate(2, 5)
}
`
	sourcePath := filepath.Join(tmp, "main.hike")
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	wasmPath := filepath.Join(tmp, "debug.wasm")
	build := exec.Command(hikecPath, "build", "-g", "-target", "wabt", "-o", wasmPath, sourcePath)
	build.Dir = tmp
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("WABT debug build failed: %v\n%s", err, output)
	}

	wasmtime, _ := exec.LookPath("wasmtime")
	lldb, _ := exec.LookPath("lldb")
	commands := []string{
		"settings set plugin.jit-loader.gdb.enable on",
		"breakpoint set --name calculate",
		"run",
		"breakpoint set --file main.hike --line 6",
		"continue",
		"frame variable A B",
		"next",
		"frame info",
		"frame variable return_of_function",
		"continue",
	}
	debuggerArgs := []string{"--batch", "--no-lldbinit"}
	for _, command := range commands {
		debuggerArgs = append(debuggerArgs, "-o", command)
	}
	debuggerArgs = append(debuggerArgs, "--", wasmtime, "run", "-D", "debug-info", "-D", "address-map=y", "-O", "opt-level=0", wasmPath)
	debugger := exec.Command(lldb, debuggerArgs...)
	debugger.Dir = tmp
	debugger.Env = append(os.Environ(), "_NO_DEBUG_HEAP=1")
	var stdout, stderr bytes.Buffer
	debugger.Stdout = &stdout
	debugger.Stderr = &stderr
	if err := debugger.Run(); err != nil {
		t.Fatalf("LLDB/Wasmtime session failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	debugOutput := stdout.String() + "\n" + stderr.String()
	if !regexp.MustCompile(`(?m)\bA\s*=\s*12\b`).MatchString(debugOutput) {
		t.Fatalf("debugger did not show A=12:\n%s", debugOutput)
	}
	if !regexp.MustCompile(`(?m)\bB\s*=\s*10\b`).MatchString(debugOutput) {
		t.Fatalf("debugger did not show B=10:\n%s", debugOutput)
	}
	if !regexp.MustCompile(`(?m)main\.hike:10\b`).MatchString(debugOutput) {
		t.Fatalf("debugger did not advance to the caller after the return statement:\n%s", debugOutput)
	}
	if !regexp.MustCompile(`(?m)\breturn_of_function\s*=\s*22\b`).MatchString(debugOutput) {
		t.Fatalf("debugger did not show return_of_function=22:\n%s", debugOutput)
	}
}
