package main

import (
	"flag"
	"fmt"
	"os"

	"hikec-go/pkg/compiler"
)

func main() {
	outputPath := flag.String("o", "output.ll", "output LLVM IR file path")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: hikec <input.hike> [-o <output.ll>]")
		os.Exit(1)
	}
	inputFile := args[0]

	c := compiler.New()
	llvmIR, err := c.CompileFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Compilation failed: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputPath, []byte(llvmIR), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Compiled %s -> %s\n", inputFile, *outputPath)
}
