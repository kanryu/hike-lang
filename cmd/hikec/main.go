package main

import (
	"flag"
	"fmt"
	"os"

	"hikec-go/pkg/codegen"
	"hikec-go/pkg/loader"
	"hikec-go/pkg/sema"
)

func main() {
	var outputFile string
	var verbose bool

	flag.StringVar(&outputFile, "o", "output.ll", "Output LLVM IR file path")
	flag.BoolVar(&verbose, "v", false, "Enable verbose logging")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: hikec [-v] [-o output.ll] <entry_file.hike>")
		os.Exit(1)
	}

	entryFile := args[0]
	if verbose {
		fmt.Printf("[CLI] Loading entry file: %s (Verbose Mode: ON)\n", entryFile)
	}

	// 1. プログラム全体の読み込みと再帰インポート解決
	l := loader.New(verbose)
	prog, err := l.Load(entryFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Loader error: %v\n", err)
		os.Exit(1)
	}

	// 2. セマンティクス解析 (型検査・レイアウト計算)
	if verbose {
		fmt.Println("[SEMA] Starting semantic analysis...")
	}
	semaCtx, err := sema.Analyze(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Semantic error: %v\n", err)
		os.Exit(1)
	}

	// 3. LLVM IR コード生成
	if verbose {
		fmt.Println("[CODEGEN] Starting LLVM IR generation...")
	}
	cg := codegen.New(prog, semaCtx)
	cg.SetVerbose(verbose)
	ir := cg.Generate()

	// 4. LLVM IR の書き出し
	err = os.WriteFile(outputFile, []byte(ir), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write output file: %v\n", err)
		os.Exit(1)
	}

	if verbose {
		fmt.Printf("[CLI] Compilation finished successfully: %s -> %s\n", entryFile, outputFile)
	} else {
		fmt.Printf("Compiled %s -> %s\n", entryFile, outputFile)
	}
}
