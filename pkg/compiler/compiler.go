package compiler

import (
	"fmt"
	"path/filepath"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/backend/llvm"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/loader"
	"hikec-go/pkg/lower"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/target"
	"hikec-go/pkg/transform"
)

type Compiler struct {
	target  *target.Target
	verbose bool
}

func New(tgt *target.Target) *Compiler {
	if tgt == nil {
		tgt = target.DefaultTarget()
	}
	return &Compiler{
		target:  tgt,
		verbose: false,
	}
}

func (c *Compiler) SetVerbose(v bool) {
	c.verbose = v
}

// CompileToHIR はフロントエンド・ミドルエンドを実行し、ターゲットに応じた HIR を生成します
func (c *Compiler) CompileToHIR(entryPaths ...string) (*hir.Program, *sema.Context, *ast.Program, error) {
	if len(entryPaths) == 0 {
		return nil, nil, nil, fmt.Errorf("no input files provided")
	}

	targetTriple := ""
	if c.target != nil {
		targetTriple = c.target.Triple
		sema.SetTargetArchitecture(targetTriple)
	}

	rootDir := filepath.Dir(entryPaths[0])
	if rootDir == "" {
		rootDir = "."
	}

	// 1. パッケージ探索・構文解析
	ld := loader.New(rootDir)
	ld.SetVerbose(c.verbose)
	rawProg, err := ld.Load(entryPaths...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loader error: %w", err)
	}

	// 2. 意味解析・型検査
	semaCtx, err := sema.Analyze(rawProg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("semantic error: %w", err)
	}

	// 3. ジェネリクス単相化
	tf := transform.New(rawProg, semaCtx)
	concreteProg, err := tf.Transform()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("transform error: %w", err)
	}

	// 4. HIR への Lowering (Compiler が保持するターゲット情報を Lowerer に直接教える)
	is32Bit := (c.target != nil && (c.target.IsWasm || sema.PointerSize == 4 || strings.HasPrefix(targetTriple, "wasm32")))
	lw := lower.New(concreteProg, semaCtx)
	lw.Set32Bit(is32Bit)
	hirProg := lw.Lower()

	return hirProg, semaCtx, concreteProg, nil
}

// CompileToLLVM は HIR 生成を経て、最終的な LLVM IR 文字列を出力します
func (c *Compiler) CompileToLLVM(entryPaths ...string) (string, *sema.Context, *ast.Program, error) {
	hirProg, semaCtx, concreteProg, err := c.CompileToHIR(entryPaths...)
	if err != nil {
		return "", nil, nil, err
	}

	targetTriple := ""
	if c.target != nil {
		targetTriple = c.target.Triple
	}

	// 5. LLVM バックエンドによるコード出力
	emitter := llvm.New(hirProg, semaCtx, targetTriple)
	llvmIR := emitter.Emit()

	// 6. ターゲットに応じたランタイム IR の自動切り替え
	targetRuntime := llvm.GetRuntimeIR(targetTriple)
	defaultRuntime := llvm.GetBuiltinRuntimeIR()
	if targetRuntime != "" && targetRuntime != defaultRuntime && strings.Contains(llvmIR, defaultRuntime) {
		llvmIR = strings.Replace(llvmIR, defaultRuntime, targetRuntime, 1)
	}

	return llvmIR, semaCtx, concreteProg, nil
}

// CompileFile は単一ファイルのコンパイル用ショートカット
func (c *Compiler) CompileFile(entryPath string) (string, error) {
	ir, _, _, err := c.CompileToLLVM(entryPath)
	return ir, err
}
