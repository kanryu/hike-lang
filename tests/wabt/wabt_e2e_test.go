package wabt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWabtBuildAndNodeExecution(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int { return 6 * 7 }
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt output = %q, want %q", got, want)
	}
}

func TestWabtEmitIRProducesWatAndRuntime(t *testing.T) {
	requireTools(t)
	root, tmp := projectRoot(t), t.TempDir()
	src := filepath.Join(tmp, "main.hike")
	if err := os.WriteFile(src, []byte("package main\nfunc main() int { return 42 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	wat := filepath.Join(tmp, "main.wat")
	cmd := exec.Command("go", "run", "./cmd/hikec", "emit-ir", "-target", "wabt", "-o", wat, src)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Wabt emit-ir failed: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(wat); err != nil || len(data) == 0 || data[0] != '(' {
		t.Fatalf("expected WAT output in %s", wat)
	}
	if _, err := os.Stat(filepath.Join(tmp, "runtime.js")); err != nil {
		t.Fatalf("runtime.js was not generated: %v", err)
	}
	wasm := filepath.Join(tmp, "checked.wasm")
	assemble := exec.Command("wat2wasm", wat, "-o", wasm)
	if out, err := assemble.CombinedOutput(); err != nil {
		t.Fatalf("generated WAT is not assemblable: %v\n%s", err, out)
	}
}
