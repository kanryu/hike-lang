// wasm-hikec is the WebAssembly-only Hike compiler frontend.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hikec-go/pkg/codegen"
	"hikec-go/pkg/compiler"
	"hikec-go/pkg/target"
)

func usage() {
	fmt.Println("Usage: wasm-hikec build [options] <source.hike...>")
	fmt.Println("\nOptions:")
	fmt.Println("  -o <path>              Output WebAssembly module")
	fmt.Println("  -js <path>             Output browser runtime JavaScript")
	fmt.Println("  -wasm-mode <mode>      normal or concurrent")
	fmt.Println("  --alloc=region         Use region allocation")
	fmt.Println("  -v, -vv                Enable compiler logging")
}

func main() {
	if len(os.Args) < 3 || os.Args[1] != "build" {
		usage()
		os.Exit(1)
	}

	output := ""
	jsOutput := ""
	wasmMode := "normal"
	region := false
	verbose := false
	var sources []string
	for i := 2; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch {
		case arg == "-o" && i+1 < len(os.Args):
			output = os.Args[i+1]
			i++
		case strings.HasPrefix(arg, "-o="):
			output = strings.TrimPrefix(arg, "-o=")
		case arg == "-js" && i+1 < len(os.Args):
			jsOutput = os.Args[i+1]
			i++
		case strings.HasPrefix(arg, "-js="):
			jsOutput = strings.TrimPrefix(arg, "-js=")
		case arg == "-wasm-mode" && i+1 < len(os.Args):
			wasmMode = os.Args[i+1]
			i++
		case strings.HasPrefix(arg, "-wasm-mode="):
			wasmMode = strings.TrimPrefix(arg, "-wasm-mode=")
		case arg == "--alloc=region":
			region = true
		case arg == "-v" || arg == "-vv":
			verbose = true
			if arg == "-vv" {
				os.Setenv("HIKEC_VERBOSE_LEVEL", "2")
			}
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "unknown option: %s\n", arg)
			os.Exit(1)
		default:
			sources = append(sources, arg)
		}
	}
	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "no Hike source files provided")
		os.Exit(1)
	}
	if wasmMode != "normal" && wasmMode != "concurrent" {
		fmt.Fprintf(os.Stderr, "invalid wasm mode: %s\n", wasmMode)
		os.Exit(1)
	}

	tgt, _ := target.ParseTarget("wasm32")
	comp := compiler.New(tgt)
	comp.SetWasmMode(wasmMode)
	comp.SetRegionMode(region)
	comp.SetVerbose(verbose)
	llvmIR, _, program, err := comp.CompileToLLVM(sources...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compilation error: %v\n", err)
		os.Exit(1)
	}

	if output == "" {
		base := strings.TrimSuffix(filepath.Base(sources[0]), filepath.Ext(sources[0]))
		output = base + ".wasm"
	}
	llPath := filepath.Join(os.TempDir(), fmt.Sprintf("wasm_hikec_%d.ll", os.Getpid()))
	defer os.Remove(llPath)
	if err := os.WriteFile(llPath, []byte(llvmIR), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write temporary IR: %v\n", err)
		os.Exit(1)
	}
	clangArgs := []string{
		"--target=" + tgt.Triple, "-O2", "-nostdlib",
		"-Wl,--no-entry", "-Wl,--export-all", "-Wl,--allow-undefined",
		llPath, "-o", output,
	}
	if verbose {
		fmt.Printf("Running: clang %s\n", strings.Join(clangArgs, " "))
	}
	cmd := exec.Command("clang", clangArgs...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Wasm link failed: %v\n", err)
		os.Exit(1)
	}

	if jsOutput == "" {
		jsOutput = filepath.Join(filepath.Dir(output), "runtime.js")
	}
	if err := codegen.WriteWasmJSRuntimeMode(jsOutput, wasmMode, program); err != nil {
		fmt.Fprintf(os.Stderr, "JavaScript runtime generation failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Wasm build completed -> %s\n", output)
	fmt.Printf("Browser runtime generated -> %s\n", jsOutput)
}
