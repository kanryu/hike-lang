package codegen

import (
	"os"
	"path/filepath"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/codegen/jsruntime"
)

// GenerateWasmJSRuntime returns the browser runtime source without performing
// any filesystem operation. Use jsruntime directly when embedding the runtime.
func GenerateWasmJSRuntime(programs ...*ast.Program) string {
	return jsruntime.GenerateWasmJSRuntime(programs...)
}

func GenerateWasmJSRuntimeMode(mode string, programs ...*ast.Program) string {
	return jsruntime.GenerateWasmJSRuntimeMode(mode, programs...)
}

// WriteWasmJSRuntime writes the generated runtime to runtime.js.
func WriteWasmJSRuntime(destPath string, programs ...*ast.Program) error {
	return WriteWasmJSRuntimeMode(destPath, "normal", programs...)
}

// WriteWasmJSRuntimeMode is the filesystem adapter for the JS runtime
// generator. The generator itself remains usable in filesystem-free Wasm code.
func WriteWasmJSRuntimeMode(destPath, mode string, programs ...*ast.Program) error {
	dir := filepath.Dir(destPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(destPath, []byte(GenerateWasmJSRuntimeMode(mode, programs...)), 0644)
}
