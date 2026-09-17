package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/codegen/jsruntime"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

func TestWasmHikeCEntryPointEmitsRuntimeJFunc(t *testing.T) {
	sourcePath := filepath.Join("main.hike")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	p := parser.New(lexer.New(string(source)))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("main.hike must parse: %v", p.Errors())
	}

	var init *ast.JFuncDecl
	for _, decl := range program.Decls {
		if jfn, ok := decl.(*ast.JFuncDecl); ok && jfn.Name.Value == "wasm_hikec_runtime_init" {
			init = jfn
			break
		}
	}
	if init == nil {
		t.Fatal("main.hike must declare wasm_hikec_runtime_init")
	}
	if len(init.ReturnTypes) != 1 {
		t.Fatalf("runtime init JFUNC return types = %d, want 1", len(init.ReturnTypes))
	}

	runtime := jsruntime.GenerateWasmJSRuntimeMode("normal", program)
	for _, fragment := range []string{
		`env["__hike_js_wasm_hikec_runtime_init"]`,
		`globalThis . __hikeWasmC`,
		`globalThis . __hikeWasmC . memory = wasmMemory`,
	} {
		if !strings.Contains(runtime, fragment) {
			t.Fatalf("generated runtime.js does not contain %q", fragment)
		}
	}
}
