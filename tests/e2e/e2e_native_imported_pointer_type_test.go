package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const qualifiedFunctionMain = `package main

import "repro/template"
import "repro/compiler"

func main() int {
	t := template.New()
	t.Parse()
	_ = compiler.New()
	return 0
}
`

const qualifiedFunctionTemplate = `package template

type Template struct{}

func New() *Template { return &Template{} }
func (t *Template) Parse() {}
`

const qualifiedFunctionCompiler = `package compiler

func New() int { return 1 }
`

const importedStructPointerMain = `package main

import "repro/dep"

extern func report(value *dep.Compiler) int
`

const importedStructPointerDep = `package dep

type Compiler struct{}
`

// TestE2ENativeQualifiedFunctionResolution is a minimal reproducer for the
// native compiler failure: template.New must not resolve to compiler.New.
func TestE2ENativeQualifiedFunctionResolution(t *testing.T) {
	if !useNativeBins {
		t.Skip("requires a native HikeC binary")
	}

	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "template"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "compiler"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(qualifiedFunctionMain), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "template", "template.hike"), []byte(qualifiedFunctionTemplate), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "compiler", "compiler.hike"), []byte(qualifiedFunctionCompiler), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "hike.mod"), []byte("module repro\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(tmpDir, "main.ll")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := addCompileFork([]string{"emit-ir", "-go-hike=1", "-o", output, "main.go"})
	cmd := exec.CommandContext(ctx, hikecBin, args...)
	cmd.Dir = tmpDir
	combined, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("minimal imported compiler package program timed out:\n%s", tailOutput(combined))
	}
	if err != nil {
		t.Fatalf("minimal imported compiler package program failed: %v\n%s", err, tailOutput(combined))
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("EmitIR did not create %s: %v", output, err)
	}
}

// TestE2ENativeImportedStructPointerParameter exercises the Hike source shape
// that failed in the self-hosted compiler: an imported struct type is used as
// a pointer parameter in the main package.
func TestE2ENativeImportedStructPointerParameter(t *testing.T) {
	if !useNativeBins {
		t.Skip("requires a native HikeC binary")
	}

	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "dep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.hike"), []byte(importedStructPointerMain), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "dep", "dep.hike"), []byte(importedStructPointerDep), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "hike.mod"), []byte("module repro\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(tmpDir, "main.ll")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := addCompileFork([]string{"emit-ir", "-go-hike=1", "-o", output, "main.hike"})
	cmd := exec.CommandContext(ctx, hikecBin, args...)
	cmd.Dir = tmpDir
	combined, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("imported struct pointer parameter test timed out:\n%s", tailOutput(combined))
	}
	if err != nil {
		t.Fatalf("imported struct pointer parameter compilation failed: %v\n%s", err, tailOutput(combined))
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("EmitIR did not create %s: %v", output, err)
	}
}

func tailOutput(data []byte) string {
	const maxBytes = 8192
	if len(data) <= maxBytes {
		return string(data)
	}
	return "...<truncated>...\n" + string(data[len(data)-maxBytes:])
}
