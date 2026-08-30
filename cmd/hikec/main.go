package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hikec-go/pkg/cgen"
	"hikec-go/pkg/codegen"
	"hikec-go/pkg/loader"
	"hikec-go/pkg/sema"
)

func main() {
	outputLL := "output.ll"
	outputHeader := ""
	verbose := false
	var sourceFiles []string

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" && i+1 < len(args) {
			outputLL = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-o=") {
			outputLL = strings.TrimPrefix(arg, "-o=")
		} else if (arg == "-header" || arg == "--header") && i+1 < len(args) {
			outputHeader = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-header=") || strings.HasPrefix(arg, "--header=") {
			parts := strings.SplitN(arg, "=", 2)
			outputHeader = parts[1]
		} else if arg == "-v" || arg == "--verbose" {
			verbose = true
		} else if strings.HasPrefix(arg, "-") {
			// 未知のオプション
		} else {
			sourceFiles = append(sourceFiles, arg)
		}
	}

	if len(sourceFiles) == 0 {
		fmt.Println("Usage: hikec [options] <source.hike...>")
		fmt.Println("Options:")
		fmt.Println("  -o <path>        Output LLVM IR file path (default: output.ll)")
		fmt.Println("  -header <path>   Output C/C++ header file path")
		fmt.Println("  -v               Enable verbose logging")
		os.Exit(1)
	}

	// ルートディレクトリとエントリーファイルを渡す
	rootDir := "."
	if len(sourceFiles) > 0 {
		rootDir = filepath.Dir(sourceFiles[0])
		if rootDir == "" {
			rootDir = "."
		}
	}

	ld := loader.New(rootDir)
	ld.SetVerbose(verbose)

	// 【修正点】 sourceFiles を Load に渡す
	prog, err := ld.Load(sourceFiles...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Loader error: %v\n", err)
		os.Exit(1)
	}

	semaCtx, err := sema.Analyze(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Sema error: %v\n", err)
		os.Exit(1)
	}

	cg := codegen.New(prog, semaCtx)
	cg.SetVerbose(verbose)
	llvmIR := cg.Generate()

	if err := os.WriteFile(outputLL, []byte(llvmIR), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Codegen write error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Compiled [%d target(s)] -> %s\n", len(sourceFiles), outputLL)

	// C/C++ヘッダー出力
	if outputHeader != "" {
		headerCode := cgen.GenerateHeader(prog, semaCtx, outputHeader)
		if err := os.WriteFile(outputHeader, []byte(headerCode), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Header write error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Generated C/C++ Header -> %s\n", outputHeader)
	}
}
