package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestGoHikeNativeStringSwitchTableRepro checks that structured string-switch
// table cases retain their lowered bodies in the self-hosted compiler.
func TestGoHikeNativeStringSwitchTableRepro(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the reproducer currently targets the Windows native binary")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	nativeCompiler := os.Getenv("HIKE_GO_HIKE_BIN")
	if nativeCompiler == "" {
		nativeCompiler = filepath.Join(repoRoot, "bin", "hikec-gohike.exe")
	}
	if _, err := os.Stat(nativeCompiler); err != nil {
		t.Skipf("self-hosted compiler binary is unavailable: %v", err)
	}

	root, err := os.MkdirTemp(repoRoot, ".string-switch-table-repro-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	mod := "module string-switch-table-repro\nhike 0.1.0\n" +
		"GoReplace os => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "os")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(`package main

import "os"

func consume(values []string) { }

func main() int {
    cmd := os.Args[1]
    cmdArgs := os.Args[2:]
    switch cmd {
    case "go": cmdArgs = os.Args[2:]; consume(cmdArgs); return 1
    case "emit-ir": cmdArgs = os.Args[2:]; consume(cmdArgs); return 2
    case "build": cmdArgs = os.Args[2:]; consume(cmdArgs); return 3
    case "run": cmdArgs = os.Args[2:]; consume(cmdArgs); return 4
    case "tool": cmdArgs = os.Args[2:]; consume(cmdArgs); return 5
    case "get": cmdArgs = os.Args[2:]; consume(cmdArgs); return 6
    case "help", "-h", "--help": cmdArgs = os.Args[2:]; consume(cmdArgs); return 7
    case "version": cmdArgs = os.Args[2:]; consume(cmdArgs); return 8
    }
    return 0
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "main.ll")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nativeCompiler, "emit-ir", "-go-hike=1", "-vv", "-o", output, "main.go")
	cmd.Dir = root
	combined, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("string switch table timed out: %v\n%s", ctx.Err(), combined)
	}
	if err != nil {
		t.Fatalf("string switch table failed: %v\n%s", err, combined)
	}
	inMain := false
	for _, line := range strings.Split(string(combined), "\n") {
		if strings.Contains(line, "LLVM emit HIR function begin") {
			inMain = strings.Contains(line, "name=main ")
		}
		if inMain && strings.Contains(line, "structured.table.case.") && strings.Contains(line, "(insts=0)") {
			t.Fatalf("string switch table case body was lost during lowering\n%s", combined)
		}
		if inMain && strings.Contains(line, "LLVM emit HIR function end") {
			inMain = false
		}
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("LLVM IR was not produced: %v", err)
	}
}
