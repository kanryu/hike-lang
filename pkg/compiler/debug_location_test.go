package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/backend/wabt"
	"hikec-go/pkg/target"
)

const debugLocationSource = `package main

func main() int {
    value := 41
    return value + 1
}
`

const panicSiteSource = `package main

func main() int {
    panic("boom")
    return 0
}
`

const llvmPanicInvokeSource = `package main

func mark() {}

func child() {
    panic("boom")
}

func main() int {
    defer mark()
    child()
    return 0
}
`

func TestHIRRecordsPanicSite(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "panic.hike")
	if err := os.WriteFile(sourcePath, []byte(panicSiteSource), 0644); err != nil {
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
	if len(program.Functions) == 0 || len(program.Functions[0].PanicSites) != 1 {
		t.Fatalf("panic site table = %#v, want one site", program.Functions)
	}
	site := program.Functions[0].PanicSites[0]
	if site.ID != 0 || site.Function != "main" || site.Location.Filename != sourcePath || site.Location.Line != 4 {
		t.Fatalf("unexpected panic site: %#v", site)
	}
}

func TestBackendsEmitPanicRecordCall(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "panic.hike")
	if err := os.WriteFile(sourcePath, []byte(panicSiteSource), 0644); err != nil {
		t.Fatal(err)
	}
	wabtTarget, err := target.ParseTarget("wabt")
	if err != nil {
		t.Fatal(err)
	}
	wat, _, _, err := New(wabtTarget).CompileToWAT(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wat, "__hike_panic_set") || !strings.Contains(wat, "i32.const 0") {
		t.Fatalf("WAT does not contain panic record call:\n%s", wat)
	}
	llvmTarget, err := target.ParseTarget("linux")
	if err != nil {
		t.Fatal(err)
	}
	llvm, _, _, err := New(llvmTarget).CompileToLLVM(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(llvm, "@__hike_panic_set") {
		t.Fatalf("LLVM IR does not contain panic record call:\n%s", llvm)
	}
}

func TestLLVMDeferFunctionUsesFatalPanicPath(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "panic_invoke.hike")
	if err := os.WriteFile(sourcePath, []byte(llvmPanicInvokeSource), 0644); err != nil {
		t.Fatal(err)
	}
	tgt, err := target.ParseTarget("linux")
	if err != nil {
		t.Fatal(err)
	}
	ir, _, _, err := New(tgt).CompileToLLVM(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"@__hike_panic_fatal", "call void @__hike_panic_fatal"} {
		if !strings.Contains(ir, marker) {
			t.Fatalf("LLVM IR does not contain %q:\n%s", marker, ir)
		}
	}
	for _, marker := range []string{"invoke", "landingpad", "__gxx_personality"} {
		if strings.Contains(ir, marker) {
			t.Fatalf("LLVM IR unexpectedly contains cross-function EH marker %q:\n%s", marker, ir)
		}
	}
	llvmAs, err := exec.LookPath("llvm-as")
	if err != nil {
		t.Skip("llvm-as is not installed")
	}
	irPath := filepath.Join(tmp, "panic_invoke.ll")
	bcPath := filepath.Join(tmp, "panic_invoke.bc")
	if err := os.WriteFile(irPath, []byte(ir), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(llvmAs, irPath, "-o", bcPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("llvm-as rejected generated IR: %v\n%s\nIR:\n%s", err, output, ir)
	}
}

func TestLLVMWindowsDeferUsesFatalPanicPath(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "panic_windows.hike")
	if err := os.WriteFile(sourcePath, []byte(llvmPanicInvokeSource), 0644); err != nil {
		t.Fatal(err)
	}
	tgt, err := target.ParseTarget("windows")
	if err != nil {
		t.Fatal(err)
	}
	ir, _, _, err := New(tgt).CompileToLLVM(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"@__hike_panic_fatal", "call void @__hike_panic_fatal"} {
		if !strings.Contains(ir, marker) {
			t.Fatalf("Windows LLVM IR does not contain %q:\n%s", marker, ir)
		}
	}
	for _, marker := range []string{"__gxx_personality_seh0", "catch i8* null", "__cxa_begin_catch", "__cxa_rethrow", "invoke", "landingpad"} {
		if strings.Contains(ir, marker) {
			t.Fatalf("Windows LLVM IR unexpectedly contains cross-function EH marker %q:\n%s", marker, ir)
		}
	}
	llvmAs, err := exec.LookPath("llvm-as")
	if err != nil {
		t.Skip("llvm-as is not installed")
	}
	irPath := filepath.Join(tmp, "panic_windows.ll")
	bcPath := filepath.Join(tmp, "panic_windows.bc")
	if err := os.WriteFile(irPath, []byte(ir), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(llvmAs, irPath, "-o", bcPath).CombinedOutput(); err != nil {
		t.Fatalf("llvm-as rejected Windows IR: %v\n%s", err, output)
	}
}

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

func TestWABTDebugInfoTracksUserLocalsAndReturnValue(t *testing.T) {
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "main.hike")
	source := `package main

func calculate(a int, b int) int {
    A := a + 10
    B := b * 2
    return A + B
}

func main() int {
    return calculate(2, 5)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	tgt, err := target.ParseTarget("wabt")
	if err != nil {
		t.Fatal(err)
	}
	c := New(tgt)
	c.SetDebugInfo(true)
	if _, _, _, err := c.CompileToWAT(sourcePath); err != nil {
		t.Fatal(err)
	}
	info := c.WABTDebugInfo()
	if info == nil {
		t.Fatal("WABT debug info was not collected")
	}

	for _, fn := range info.Functions {
		if fn.Name != "calculate" {
			continue
		}
		if fn.Line != 3 {
			t.Fatalf("calculate source line = %d, want 3", fn.Line)
		}
		locals := make(map[string]wabt.DebugLocal)
		for _, local := range fn.Locals {
			locals[local.Name] = local
		}
		for name, wantLine := range map[string]uint32{"A": 4, "B": 5, "return_of_function": 7} {
			local, ok := locals[name]
			if !ok {
				t.Fatalf("calculate debug locals omit %q: %#v", name, fn.Locals)
			}
			if local.Line != wantLine {
				t.Fatalf("debug local %q line = %d, want %d", name, local.Line, wantLine)
			}
			if local.Size != 4 {
				t.Fatalf("debug local %q size = %d, want 4", name, local.Size)
			}
		}
		for _, wantLine := range []uint32{4, 5, 6, 7} {
			found := false
			for _, line := range fn.Lines {
				if line == wantLine {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("calculate debug line table omits source line %d: %v", wantLine, fn.Lines)
			}
		}
		return
	}
	t.Fatal("WABT debug info did not contain calculate")
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
