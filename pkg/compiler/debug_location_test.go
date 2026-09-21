package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

const debugLocationSource = `package main

func main() int {
    value := 41
    return value + 1
}
`

func TestHIRInstructionsKeepHikeSourceLocations(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "main.hike")
	if err := os.WriteFile(sourcePath, []byte(debugLocationSource), 0644); err != nil {
		t.Fatal(err)
	}

	tgt, err := target.ParseTarget("wabt")
	if err != nil {
		t.Fatal(err)
	}
	program, _, _, err := New(tgt).CompileToHIR(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(program.InstructionLocations) == 0 {
		t.Fatal("HIR instruction source locations were not recorded")
	}

	foundSourceLine := false
	for instructionKey, location := range program.InstructionLocations {
		if instructionKey == "" {
			t.Fatal("empty HIR instruction key has a source location")
		}
		if location.Filename != sourcePath {
			t.Fatalf("instruction %q location filename = %q, want %q", instructionKey, location.Filename, sourcePath)
		}
		if location.Line <= 0 || location.Column <= 0 {
			t.Fatalf("instruction %q has invalid source location %+v", instructionKey, location)
		}
		if location.Line == 4 {
			foundSourceLine = true
		}
	}
	if !foundSourceLine {
		t.Fatal("no HIR instruction was mapped to the variable declaration line")
	}

	for _, fn := range program.Functions {
		if fn.Name == "main" {
			if fn.Location.Filename != sourcePath || fn.Location.Line != 3 {
				t.Fatalf("main function location = %+v, want %s:3", fn.Location, sourcePath)
			}
			return
		}
	}
	t.Fatal("main function was not found")
}

func TestCFuncReturnExpressionKeepsReturnLine(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "main.hike")
	source := "package main\n\ncfunc AddNumbers(a int, b int) int {\n\treturn a + b\n}\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	tgt, err := target.ParseTarget("wabt")
	if err != nil {
		t.Fatal(err)
	}
	program, _, _, err := New(tgt).CompileToHIR(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for key, location := range program.InstructionLocations {
		if strings.Contains(key, "*hir.InstrBinary@") && location.Line == 4 {
			debugCompiler := New(tgt)
			debugCompiler.SetDebugInfo(true)
			if _, _, _, err := debugCompiler.CompileToWAT(sourcePath); err != nil {
				t.Fatal(err)
			}
			for _, fn := range debugCompiler.WABTDebugInfo().Functions {
				if fn.Name == "__hike_impl_AddNumbers" {
					foundClosingLine := false
					for _, debugLine := range fn.Lines {
						if debugLine == 5 {
							foundClosingLine = true
						}
					}
					for _, local := range fn.Locals {
						if local.Name == "return_of_function" && local.Line == 5 && foundClosingLine {
							return
						}
					}
					t.Fatal("WABT debug info omitted the pre-return closing-brace location")
				}
			}
			t.Fatal("WABT debug info did not contain the cfunc implementation")
		}
	}
	t.Fatal("cfunc return expression was not mapped to line 4")
}

func TestLLVMEmitterIncludesDebugMetadata(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "main.hike")
	if err := os.WriteFile(sourcePath, []byte(debugLocationSource), 0644); err != nil {
		t.Fatal(err)
	}

	tgt, err := target.ParseTarget("linux")
	if err != nil {
		t.Fatal(err)
	}
	c := New(tgt)
	c.SetDebugInfo(true)
	ir, _, _, err := c.CompileToLLVM(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"!llvm.dbg.cu", "!DICompileUnit", "!DILocation", "!DISubprogram", "!dbg !"} {
		if !strings.Contains(ir, marker) {
			t.Fatalf("debug LLVM IR does not contain %q", marker)
		}
	}
}

func TestLLVMEmitterIncludesStructVariableMetadata(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "struct.hike")
	source := `package main

type Point struct { x int, y int }

func main() int {
    point := Point{x: 1, y: 2}
    return point.x
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt, err := target.ParseTarget("linux")
	if err != nil {
		t.Fatal(err)
	}
	c := New(tgt)
	c.SetDebugInfo(true)
	ir, _, _, err := c.CompileToLLVM(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"!DICompositeType(tag: DW_TAG_structure_type, name: \"Point\"", "!DIDerivedType(tag: DW_TAG_member, name: \"x\"", "!DIDerivedType(tag: DW_TAG_member, name: \"y\"", "!DILocalVariable(name: \"point\"", "@llvm.dbg.declare"} {
		if !strings.Contains(ir, marker) {
			t.Fatalf("debug LLVM IR does not contain struct marker %q", marker)
		}
	}
}
