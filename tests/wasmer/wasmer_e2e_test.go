package wasmer_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"hikec-go/tests/testutil"
)

var hikecBin string

func projectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
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
	base, err := os.MkdirTemp(root, ".wasmer-test-")
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

func TestWabtWasmRunsWithWasmer(t *testing.T) {
	root := projectRoot(t)
	wasmer := os.Getenv("WASMER_PATH")
	if wasmer == "" {
		wasmer = filepath.Join(root, "wasmer.exe")
		if _, err := os.Stat(wasmer); err != nil {
			wasmer = filepath.Join(root, "wasmer")
		}
	}
	if _, err := os.Stat(wasmer); err != nil {
		t.Skipf("built wasmer executable not found: %s", wasmer)
	}
	if _, err := exec.LookPath("wat2wasm"); err != nil {
		t.Skip("wat2wasm is required for the WABT build")
	}

	tmp := t.TempDir()
	source := filepath.Join(tmp, "main.hike")
	wasm := filepath.Join(tmp, "main.wasm")
	if err := os.WriteFile(source, []byte(`package main

func main() int {
    return (6 * 7) + 1
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	build := exec.Command(hikecBin, "build", "-target", "wabt", "-o", wasm, source)
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("WABT build failed: %v\n%s", err, output)
	}

	var stdout, stderr bytes.Buffer
	run := exec.Command(wasmer, wasm, "main")
	run.Stdout = &stdout
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("wasmer execution failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if got, want := stdout.String(), "43\n"; got != want {
		t.Fatalf("wasmer output = %q, want %q", got, want)
	}
}
