package compiler

import (
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

func TestCompilerStringIncludesVerboseLevel(t *testing.T) {
	c := New(target.DefaultTarget())
	c.SetVerboseLevel(2)

	got := c.String()
	if !strings.Contains(got, "verbose=true") {
		t.Fatalf("Compiler.String() = %q, want verbose=true", got)
	}
	if !strings.Contains(got, "verboseLevel=2") {
		t.Fatalf("Compiler.String() = %q, want verboseLevel=2", got)
	}
}

func TestCompilerSetVerboseDefaultsToLevelOne(t *testing.T) {
	c := New(target.DefaultTarget())
	c.SetVerbose(true)

	if got := c.String(); !strings.Contains(got, "verboseLevel=1") {
		t.Fatalf("Compiler.String() = %q, want verboseLevel=1", got)
	}
}
