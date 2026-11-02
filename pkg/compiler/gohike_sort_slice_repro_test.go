package compiler

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestGoHikeNativeSortSliceRepro isolates generic sort.Slice specialization
// over package-defined slice element types without compiling the compiler.
func TestGoHikeNativeSortSliceRepro(t *testing.T) {
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

	root, err := os.MkdirTemp(repoRoot, ".sort-slice-repro-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err := os.Mkdir(filepath.Join(root, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	stdRoot := filepath.ToSlash(filepath.Join(repoRoot, "std"))
	mod := "module sort-slice-repro\nhike 0.1.0\n" +
		"GoReplace sort => " + stdRoot + "/sort\n" +
		"GoReplace sort-slice-repro/lib => ./lib\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	const libSource = `package lib

import "sort"

type Item struct { Name string }
type Other struct { Receiver string }
type Node struct { Value int }
type Body []Node

func Collect() {
    x := []Item{{Name: "b"}, {Name: "a"}}
    sort.Slice(x, func(i, j int) bool { return x[i].Name < x[j].Name })
    y := []Other{{Receiver: "b"}, {Receiver: "a"}}
    sort.Slice(y, func(i, j int) bool { return y[i].Receiver < y[j].Receiver })
    body := Body{{Value: 0}}
    Mutate(&body)
}

func Mutate(body *Body) {
    (*body)[0] = Node{Value: 1}
}
`
	if err := os.WriteFile(filepath.Join(root, "lib", "lib.go"), []byte(libSource), 0644); err != nil {
		t.Fatal(err)
	}
	const source = `package main

import "sort-slice-repro/lib"

func main() {
    lib.Collect()
}
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "main.ll")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nativeCompiler, "emit-ir", "-go-hike=1", "-vv", "-target", "windows", "-o", output, "main.go")
	cmd.Dir = root
	combined, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("sort.Slice reproducer timed out: %v\n%s", ctx.Err(), combined)
	}
	if err != nil {
		t.Fatalf("sort.Slice reproducer failed: %v\n%s", err, combined)
	}
	if bytes.Contains(combined, []byte("sort_Slice_int_")) {
		t.Fatalf("sort.Slice was specialized with int instead of the slice element type\n%s", combined)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("LLVM IR was not produced: %v", err)
	}
}
