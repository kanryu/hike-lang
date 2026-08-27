package compiler

import (
	"fmt"
	"os"

	"hikec-go/pkg/codegen"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
	"hikec-go/pkg/sema"
)

type Compiler struct{}

func New() *Compiler {
	return &Compiler{}
}

// Compile はHIKEソースコード文字列をコンパイルし、LLVM IRアセンブラ文字列を出力します
func (c *Compiler) Compile(source string) (string, error) {
	l := lexer.New(source)
	p := parser.New(l)
	prog := p.ParseProgram()
	if prog == nil {
		return "", fmt.Errorf("failed to parse program")
	}

	semaCtx, err := sema.Analyze(prog)
	if err != nil {
		return "", fmt.Errorf("semantic error: %w", err)
	}

	cg := codegen.New(prog, semaCtx)
	return cg.Generate(), nil
}

// CompileFile はファイルパスを受け取り、コンパイルしてLLVM IR文字列を返します
func (c *Compiler) CompileFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return c.Compile(string(data))
}
