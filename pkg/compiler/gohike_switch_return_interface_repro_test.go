package compiler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestGoHikeNativeSwitchReturnInterfaceRepro isolates return-value coercion
// from a switch arm. This is the smallest case that exercises
// LowerReturnStmt -> emitValueCoerce while the target return type is an
// interface value.
func TestGoHikeNativeSwitchReturnInterfaceRepro(t *testing.T) {
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

	root := t.TempDir()
	mod := "module switch-return-interface-repro\nhike 0.1.0\n" +
		"GoReplace hikec-go/pkg => " + filepath.ToSlash(filepath.Join(repoRoot, "pkg")) + "\n" +
		"GoReplace std => " + filepath.ToSlash(filepath.Join(repoRoot, "std")) + "\n" +
		"GoReplace fmt => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "fmt")) + "\n" +
		"GoReplace strings => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "strings")) + "\n" +
		"GoReplace path => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "path")) + "\n" +
		"GoReplace strconv => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "strconv")) + "\n" +
		"GoReplace unicode => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "unicode")) + "\n" +
		"GoReplace unicode/utf8 => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "unicode", "utf8")) + "\n" +
		"GoReplace sort => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "sort")) + "\n" +
		"GoReplace regexp => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "regexp")) + "\n" +
		"GoReplace bytes => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "bytes")) + "\n" +
		"GoReplace encoding/json => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "encoding", "json")) + "\n" +
		"GoReplace sync => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "sync")) + "\n" +
		"GoReplace io => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "io")) + "\n" +
		"GoReplace os => " + filepath.ToSlash(filepath.Join(repoRoot, "std", "os")) + "\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	const source = `package main

import "hikec-go/pkg/hir"
import "hikec-go/pkg/sema"

func selectValue(n int) (hir.Value, int) {
    switch n {
    case 1:
        return &hir.ConstString{Label: "one", Raw: "one", Length: 3, Typ: sema.TypeString}, 1
    default:
        return &hir.ConstString{Label: "two", Raw: "two", Length: 3, Typ: sema.TypeString}, 2
    }
}

func main() int {
    _, _ = selectValue(1)
    return 0
}
`
	if err := os.WriteFile(filepath.Join(root, "main.hike"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		output := filepath.Join(root, fmt.Sprintf("main-%d.ll", attempt))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, nativeCompiler, "emit-ir", "-go-hike=1", "-compile-fork", "-g", "-vv", "-target", "windows", "-o", output, "main.hike")
		cmd.Dir = root
		combined, err := cmd.CombinedOutput()
		timedOut := ctx.Err()
		cancel()
		if timedOut != nil {
			t.Fatalf("switch return interface reproducer timed out on attempt %d: %v\n%s", attempt, timedOut, combined)
		}
		if err != nil {
			t.Fatalf("switch return interface reproducer failed on attempt %d: %v\n%s", attempt, err, combined)
		}
		if _, err := os.Stat(output); err != nil {
			t.Fatalf("LLVM IR was not produced on attempt %d: %v", attempt, err)
		}
	}
}
