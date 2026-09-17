package wabt_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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

func requireTools(t *testing.T) {
	t.Helper()
	for _, name := range []string{"wat2wasm", "node"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " is required for the Wabt E2E test")
		}
	}
}

func buildWabt(t *testing.T, source string) string {
	t.Helper()
	requireTools(t)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "main.hike")
	if err := os.WriteFile(src, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	wasm := filepath.Join(tmp, "main.wasm")
	cmd := exec.Command("go", "run", "./cmd/hikec", "build", "-target", "wabt", "-o", wasm, src)
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Wabt build failed: %v\n%s", err, out)
	}
	return wasm
}

func runWabt(t *testing.T, wasm string) string {
	t.Helper()
	requireTools(t)
	runner := filepath.Join(projectRoot(t), "tests", "wabt", "langwabt.js")
	script := filepath.Join(filepath.Dir(runner), "wabt_arithmetic.js")
	cmd := exec.Command("node", runner, wasm, script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Node.js Wabt execution failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}
