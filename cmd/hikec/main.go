package main

import (
	"flag"
	"fmt"
	"os"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/codegen"
	"hikec-go/pkg/loader"
	"hikec-go/pkg/sema"
)

func main() {
	outPath := flag.String("o", "output.ll", "output LLVM IR path")
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: hikec <entry_file.hike> -o <output.ll>")
		os.Exit(1)
	}
	entryFile := flag.Args()[0]

	// 1. ローダーによる依存ファイル・パッケージの収集
	ld := loader.New(".", "./std")
	progs, err := ld.LoadProgram(entryFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load Error: %v\n", err)
		os.Exit(1)
	}

	// 2. 全ファイルの AST を単一の Program にマージ
	mergedProg := &ast.Program{
		Package: "main",
		Imports: []*ast.ImportDecl{},
		Decls:   []ast.Decl{},
	}
	for _, p := range progs {
		mergedProg.Imports = append(mergedProg.Imports, p.Imports...)
		mergedProg.Decls = append(mergedProg.Decls, p.Decls...)
	}

	// 3. セマンティック解析 (sema.Analyze を呼び出し)
	semaCtx, err := sema.Analyze(mergedProg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Semantic Error: %v\n", err)
		os.Exit(1)
	}

	// 4. コード生成
	cg := codegen.New(mergedProg, semaCtx)
	llvmIR := cg.Generate()

	// 5. IR ファイル出力
	if err := os.WriteFile(*outPath, []byte(llvmIR), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Write Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Compiled %s -> %s\n", entryFile, *outPath)
}
