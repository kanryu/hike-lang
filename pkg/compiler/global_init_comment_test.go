package compiler

import (
	"os"
	"strings"
	"testing"

	"hikec-go/pkg/logger"
	"hikec-go/pkg/target"
)

func TestLLVMVerboseGlobalInitKeepsSourceComment(t *testing.T) {
	dir := t.TempDir()
	path := dir + string(os.PathSeparator) + "main.hike"
	source := "package main\n\nvar seed = 42\n\nfunc main() int { return seed }\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	logger.SetLevel(2)
	defer logger.SetLevel(0)
	comp := New(&target.TargetX86_64Linux)
	ir, _, _, err := comp.CompileToLLVM(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ir, "; global-init: @seed at ") || !strings.Contains(ir, ": var seed = 42") {
		t.Fatalf("global initialization source comment missing:\n%s", ir)
	}

	logger.SetLevel(0)
	withoutVerbose, _, _, err := comp.CompileToLLVM(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutVerbose, "; global-init: @seed at ") {
		t.Fatal("global initialization source comment emitted without -vv")
	}
}
