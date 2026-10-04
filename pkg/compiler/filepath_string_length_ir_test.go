package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

// TestStringJoinPreservesExplicitStringLength guards the string ABI at a
// string-concatenation boundary. The generated IR must preserve the explicit
// lengths passed to the length-aware concatenation instead of converting the
// result back to a C string and calling strlen.
func TestStringJoinPreservesExplicitStringLength(t *testing.T) {
	root := t.TempDir()
	source := `package main

func Join(a string, b string) string {
    return a + "/" + b
}

func main() int {
    joined := Join("left", "right")
    return len(joined)
}
`
	sourcePath := filepath.Join(root, "main.hike")
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetX86_64Linux
	ir, _, _, err := New(&tgt).CompileToLLVM(sourcePath)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	start := strings.Index(ir, "define { i8*, i32, i32 } @Join(")
	if start < 0 {
		t.Fatal("Join was not emitted")
	}
	end := strings.Index(ir[start:], "\ndefine ")
	if end < 0 {
		t.Fatal("could not delimit Join IR")
	}
	body := ir[start : start+end]
	if !strings.Contains(body, "call i8* @hike_strcat_len") {
		t.Fatalf("Join does not use length-aware concatenation:\n%s", body)
	}
	if strings.Contains(body, "call i64 @strlen") || strings.Contains(body, "call i32 @strlen32") {
		t.Fatalf("Join converts its length-aware result through strlen:\n%s", body)
	}
	if !strings.Contains(body, "add") {
		t.Fatalf("Join does not compute the result length from operand lengths:\n%s", body)
	}
}
