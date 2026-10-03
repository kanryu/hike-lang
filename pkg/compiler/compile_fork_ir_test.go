package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

func TestCompileForkEmitsOrderedValidIR(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"left", "right"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module fork-ir-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"main.hike": `package main
import "./left"
import "./right"
func main() int { return left.Value() + right.Value() }
`,
		"left/left.hike":   "package left\nfunc Value() int { return 1 }\n",
		"right/right.hike": "package right\nfunc Value() int { return 2 }\n",
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tgt := target.TargetWasm32
	c := New(&tgt)
	c.SetCompileFork(true)
	ir, _, _, err := c.CompileToLLVM(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatalf("compile-fork failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	left := strings.Index(ir, "define i32 @left_Value")
	right := strings.Index(ir, "define i32 @right_Value")
	if left < 0 || right < 0 || left >= right {
		t.Fatalf("imported function buffers are not in source order: left=%d right=%d", left, right)
	}

	llvmAs, err := exec.LookPath("llvm-as")
	if err != nil {
		t.Skip("llvm-as is not installed")
	}
	irPath := filepath.Join(root, "fork.ll")
	bcPath := filepath.Join(root, "fork.bc")
	if err := os.WriteFile(irPath, []byte(ir), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(llvmAs, irPath, "-o", bcPath).CombinedOutput(); err != nil {
		t.Fatalf("llvm-as rejected compile-fork IR: %v\n%s", err, output)
	}
}

func TestCompileForkEmitsWabtOrderedWAT(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"left", "right"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module fork-wat-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"main.hike":        "package main\nimport \"./left\"\nimport \"./right\"\nfunc main() int { return left.Value() + right.Value() }\n",
		"left/left.hike":   "package left\nfunc Value() int { return 1 }\n",
		"right/right.hike": "package right\nfunc Value() int { return 2 }\n",
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tgt := target.TargetWabt
	c := New(&tgt)
	c.SetCompileFork(true)
	wat, _, _, err := c.CompileToWAT(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatalf("compile-fork WAT failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	left := strings.Index(wat, "(func $left_Value")
	right := strings.Index(wat, "(func $right_Value")
	if left < 0 || right < 0 || left >= right {
		t.Fatalf("imported WAT function buffers are not in source order: left=%d right=%d", left, right)
	}
}

func TestWabtPanicUsesHostJSFunctionBeforeTrap(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module panic-wat-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.hike"), []byte(`package main

func main() int {
    panic("wabt panic sentinel")
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetWabt
	c := New(&tgt)
	wat, _, _, err := c.CompileToWAT(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatalf("WABT panic compilation failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	for _, marker := range []string{
		`(import "env" "__hike_js_JSPunkPanic"`,
		"(call $__hike_js_JSPunkPanic",
		"(call $llvm.trap)",
	} {
		if !strings.Contains(wat, marker) {
			t.Fatalf("WABT panic WAT is missing %q:\n%s", marker, wat)
		}
	}
}

func TestWasm32PanicUsesHostJSPunkPanic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module panic-wasm-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.hike"), []byte("package main\nfunc main() int { panic(\"wasm panic sentinel\") }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetWasm32
	c := New(&tgt)
	ir, _, _, err := c.CompileToLLVM(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatalf("wasm32 panic compilation failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	for _, marker := range []string{
		"declare void @__hike_js_JSPunkPanic(i8*, i32)",
		"call void @__hike_js_JSPunkPanic",
	} {
		if !strings.Contains(ir, marker) {
			t.Fatalf("wasm32 panic IR is missing %q:\n%s", marker, ir)
		}
	}
}

func TestWabtUserJSPunkPanicSuppressesDefaultImport(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module user-panic-wat-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

jfunc JSPunkPanic(ptr int, length int) {
    console.log("user panic")
}

func main() int { panic("user panic sentinel") }
`
	if err := os.WriteFile(filepath.Join(root, "main.hike"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetWabt
	c := New(&tgt)
	wat, _, _, err := c.CompileToWAT(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatalf("user WABT panic compilation failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	if count := strings.Count(wat, `(import "env" "__hike_js_JSPunkPanic"`); count != 1 {
		t.Fatalf("expected exactly one user WABT panic import, got %d:\n%s", count, wat)
	}
	if !strings.Contains(wat, "(call $__hike_js_JSPunkPanic") || !strings.Contains(wat, "(call $llvm.trap)") {
		t.Fatalf("user WABT panic does not call the handler and terminate:\n%s", wat)
	}
}

func TestWasm32UserJSPunkPanicSuppressesDefaultDeclaration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module user-panic-wasm-test\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

jfunc JSPunkPanic(ptr int, length int) {
    console.log("user panic")
}

func main() int { panic("user panic sentinel") }
`
	if err := os.WriteFile(filepath.Join(root, "main.hike"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetWasm32
	c := New(&tgt)
	ir, _, _, err := c.CompileToLLVM(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatalf("user wasm32 panic compilation failed: %v\n%s", err, c.Reporter().FormatAll())
	}
	if strings.Contains(ir, "declare void @__hike_js_JSPunkPanic(i8*, i32)") {
		t.Fatalf("default wasm32 panic declaration was injected for user implementation:\n%s", ir)
	}
	if !strings.Contains(ir, "declare void @__hike_js_JSPunkPanic(i32, i32)") ||
		!strings.Contains(ir, "call void @__hike_js_JSPunkPanic") {
		t.Fatalf("user wasm32 panic declaration or call is missing:\n%s", ir)
	}
}
