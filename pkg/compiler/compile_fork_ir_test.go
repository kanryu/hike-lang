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
