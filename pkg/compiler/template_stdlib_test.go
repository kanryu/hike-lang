package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

func TestStdTextTemplateRenderIsResolvedDuringCompilation(t *testing.T) {
	root := t.TempDir()
	source := `package main

import "std/text/template"

type ABI struct { Bits int }

func main() int {
	value := template.Render("bits={{.Bits}}", ABI{Bits: 32})
	if value == "bits=32" { return 0 }
	return 1
}
`
	entry := filepath.Join(root, "main.hike")
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	tgt := target.TargetWasm32
	llvm, _, _, err := New(&tgt).CompileToLLVM(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(llvm, "bits=32") {
		t.Fatalf("compiled module does not contain the expanded template: %s", llvm)
	}
	if strings.Contains(llvm, "{{.Bits}}") {
		t.Fatal("template action survived compilation")
	}
}
