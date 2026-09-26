package native_debug_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"hikec-go/tests/testutil"
)

var hikecPath string

func TestMain(m *testing.M) {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			fmt.Fprintln(os.Stderr, "could not find project root")
			os.Exit(1)
		}
		root = parent
	}

	tmp, err := os.MkdirTemp("", "hike-native-debug-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	hikecPath = filepath.Join(tmp, "hikec")
	if runtime.GOOS == "windows" {
		hikecPath += ".exe"
	}
	if output, err := testutil.BuildHikec(root, hikecPath); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build hikec: %v\n%s", err, output)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func TestNativeDebugBuildShowsLocalsAndReturnValue(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is not installed")
	}
	lldb, err := exec.LookPath("lldb")
	if err != nil {
		t.Skip("lldb is not installed")
	}
	returnRegister := "rax"
	if runtime.GOARCH == "arm64" {
		returnRegister = "x0"
	} else if runtime.GOARCH != "amd64" {
		t.Skipf("native return register is not defined for %s", runtime.GOARCH)
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

	outputPath := filepath.Join(tmp, "debug-app")
	if runtime.GOOS == "windows" {
		outputPath += ".exe"
	}
	build := exec.Command(hikecPath, "build", "-g", "-o", outputPath, sourcePath)
	build.Dir = tmp
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("native debug build failed: %v\n%s\nclang: %s", err, output, clang)
	}

	// Stop on the return statement, inspect both source locals, advance one
	// source line, and inspect the value returned in the native ABI register.
	commands := []string{
		"settings set target.load-script-from-symbol-file false",
		"breakpoint set --file main.hike --line 6",
		"run",
		"frame variable A B",
		"next",
		"frame info",
		"register read " + returnRegister,
		"quit",
	}
	debuggerArgs := []string{"--batch", "--no-lldbinit"}
	for _, command := range commands {
		debuggerArgs = append(debuggerArgs, "-o", command)
	}
	debuggerArgs = append(debuggerArgs, "--", outputPath)
	debugger := exec.Command(lldb, debuggerArgs...)
	debugger.Dir = tmp
	var stdout, stderr bytes.Buffer
	debugger.Stdout = &stdout
	debugger.Stderr = &stderr
	if err := debugger.Run(); err != nil {
		t.Fatalf("lldb session failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	debugOutput := stdout.String() + "\n" + stderr.String()
	if !regexp.MustCompile(`(?m)\bA\s*=\s*12\b`).MatchString(debugOutput) {
		t.Fatalf("LLDB did not show A=12:\n%s", debugOutput)
	}
	if !regexp.MustCompile(`(?m)\bB\s*=\s*10\b`).MatchString(debugOutput) {
		t.Fatalf("LLDB did not show B=10:\n%s", debugOutput)
	}
	if !regexp.MustCompile(`(?m)main\.hike:10\b`).MatchString(debugOutput) {
		t.Fatalf("LLDB did not advance to the caller after the return statement:\n%s", debugOutput)
	}
	returnValuePattern := fmt.Sprintf(`(?mi)\b%s\s*=\s*(?:0x)?0*16\b`, returnRegister)
	if !regexp.MustCompile(returnValuePattern).MatchString(debugOutput) {
		t.Fatalf("LLDB did not show return value 22 (0x16) in %s:\n%s", returnRegister, debugOutput)
	}
}
