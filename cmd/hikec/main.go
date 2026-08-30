package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hikec-go/pkg/codegen"
	"hikec-go/pkg/loader"
	"hikec-go/pkg/sema"
)

func main() {
	outPath := "output.ll"
	verbose := false
	var inputTargets []string

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" || arg == "--o" || arg == "-output" {
			if i+1 < len(args) {
				outPath = args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "-o=") {
			outPath = strings.TrimPrefix(arg, "-o=")
		} else if strings.HasPrefix(arg, "--o=") {
			outPath = strings.TrimPrefix(arg, "--o=")
		} else if arg == "-v" || arg == "--verbose" || arg == "-verbose" {
			verbose = true
		} else if strings.HasPrefix(arg, "-") {
			// 未知のフラグ
		} else {
			inputTargets = append(inputTargets, arg)
		}
	}

	if len(inputTargets) == 0 {
		fmt.Println("Usage: hikec [options] <file1.hike> [file2.hike...] / <dir>")
		fmt.Println("Options:")
		fmt.Println("  -o <path>    Output LLVM IR file path (default: output.ll)")
		fmt.Println("  -v           Enable verbose logging")
		os.Exit(1)
	}

	rootDir, err := os.Getwd()
	if err != nil {
		rootDir = "."
	}

	ld := loader.New(rootDir)
	ld.SetVerbose(verbose)

	if verbose {
		fmt.Printf("[CLI] Loading %d source target(s) (Verbose Mode: ON)\n", len(inputTargets))
	}

	prog, err := ld.Load(inputTargets...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load Error: %v\n", err)
		os.Exit(1)
	}

	if verbose {
		fmt.Println("[SEMA] Starting semantic analysis...")
	}
	semaCtx, err := sema.Analyze(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Semantic Error: %v\n", err)
		os.Exit(1)
	}

	if verbose {
		fmt.Println("[CODEGEN] Starting LLVM IR generation...")
	}
	cg := codegen.New(prog, semaCtx)
	cg.SetVerbose(verbose)
	llvmIR := cg.Generate()

	cleanOutPath := filepath.Clean(outPath)
	err = os.WriteFile(cleanOutPath, []byte(llvmIR), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "File Write Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Compiled [%d target(s)] -> %s\n", len(inputTargets), cleanOutPath)
}
