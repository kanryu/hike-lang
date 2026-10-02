package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

func TestClosureEscapeIRPromotesReceiverAndParameter(t *testing.T) {
	root := t.TempDir()
	mod := "module closure-escape-ir\nhike 0.1.0\n"
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

type Counter struct { value int }

func (c *Counter) MakeReader(offset int) func() int {
    scratch := "stack"
    scratch += "-reuse"
    scratch += "-one"
    scratch += "-two"
    scratch += "-three"
    scratchLen := len(scratch)
    _ = scratchLen
    return func() int { return c.value + offset }
}

func main() int {
    counter := &Counter{value: 40}
    reader := counter.MakeReader(2)
    return reader()
}
`
	if err := os.WriteFile(filepath.Join(root, "main.hike"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetWasm32
	c := New(&tgt)
	ir, _, _, err := c.CompileToLLVM(filepath.Join(root, "main.hike"))
	if err != nil {
		t.Fatalf("compile failed: %v\n%s", err, c.Reporter().FormatAll())
	}

	start := strings.Index(ir, "@Counter_ptr_MakeReader(")
	if start < 0 {
		t.Fatal("MakeReader was not emitted")
	}
	end := strings.Index(ir[start:], "\ndefine ")
	if end < 0 {
		t.Fatal("could not delimit MakeReader IR")
	}
	body := ir[start : start+end]

	// The closure environment stores the addresses of these slots. Both must
	// therefore be heap-backed rather than pointers into MakeReader's frame.
	if !strings.Contains(body, "call i8* @malloc(i32 4)\n  %c.2 = bitcast") {
		t.Fatalf("receiver slot is not heap-backed in MakeReader IR:\n%s", body)
	}
	if !strings.Contains(body, "call i8* @malloc(i32 4)\n  %offset.4 = bitcast") {
		t.Fatalf("captured parameter slot is not heap-backed in MakeReader IR:\n%s", body)
	}
	if strings.Contains(body, "%c.2 = alloca") {
		t.Fatalf("receiver slot still uses alloca in MakeReader IR:\n%s", body)
	}
}
