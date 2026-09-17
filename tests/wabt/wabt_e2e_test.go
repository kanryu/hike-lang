package wabt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(src, []byte("package main\nfunc main() int { value := \"Wabt\"; _ = value; return 42 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	wat := filepath.Join(tmp, "main.wat")
	cmd := exec.Command("go", "run", "./cmd/hikec", "emit-ir", "-target", "wabt", "-o", wat, src)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Wabt emit-ir failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(wat)
	if err != nil || len(data) == 0 || data[0] != '(' {
		t.Fatalf("expected WAT output in %s", wat)
	}
	if !strings.Contains(string(data), "(data ") || !strings.Contains(string(data), "\\00") {
		t.Fatalf("expected NUL-terminated string data section in %s", wat)
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

func TestWabtStructuredCFGThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    value := 6
    if value == 6 {
        return 42
    }
	return 0
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt CFG output = %q, want %q", got, want)
	}
}

func TestWabtTypedMemoryStoreThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

func main() int {
    var value int
    value = 42
    return value
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt typed memory output = %q, want %q", got, want)
	}
}

func TestWabtBuiltinMallocThroughNode(t *testing.T) {
	wasm := buildWabt(t, `package main

extern func malloc(size int) *byte

func main() int {
    _ = malloc(16)
    return 42
}
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt builtin malloc output = %q, want %q", got, want)
	}
}

func TestWabtUserFunctionCannotCollideWithRuntimeSymbol(t *testing.T) {
	wasm := buildWabt(t, `package main

func malloc(value int) int { return value + 1 }
func main() int { return malloc(41) }
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt mangled user function output = %q, want %q", got, want)
	}
}

func TestWabtMultiValueFunctionSignature(t *testing.T) {
	wasm := buildWabt(t, `package main

func pair() (int, int) { return 20, 22 }
func main() int { return 42 }
`)
	if got, want := runWabt(t, wasm), "WABT_RESULT=42\n"; got != want {
		t.Fatalf("Wabt multi-value output = %q, want %q", got, want)
	}
}
