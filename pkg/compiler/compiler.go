package compiler

import (
	"fmt"
	"path/filepath"

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

// CompileToHIR はフロントエンド・ミドルエンドを実行し、ターゲット非依存の HIR を生成します
func (c *Compiler) CompileToHIR(entryPaths ...string) (*hir.Program, *sema.Context, *ast.Program, error) {
	if len(entryPaths) == 0 {
		return nil, nil, nil, fmt.Errorf("no input files provided")
	}

	rootDir := filepath.Dir(entryPaths[0])
	if rootDir == "" {
		rootDir = "."
	}

	// 1. パッケージ探索・構文解析・名前マングリング・AST統合
	ld := loader.New(rootDir)
	ld.SetVerbose(c.verbose)
	rawProg, err := ld.Load(entryPaths...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loader error: %w", err)
	}

	// 2. 意味解析・型検査・エスケープ解析・暗黙キャスト挿入
	semaCtx, err := sema.Analyze(rawProg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("semantic error: %w", err)
	}

	// 3. ジェネリクス単相化（Monomorphization）
	tf := transform.New(rawProg, semaCtx)
	concreteProg, err := tf.Transform()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("transform error: %w", err)
	}

	// 4. ターゲット非依存 HIR への Lowering
	lw := lower.New(concreteProg, semaCtx)
	hirProg := lw.Lower()

	return hirProg, semaCtx, concreteProg, nil
}

// CompileToLLVM は HIR 生成を経て、最終的な LLVM IR 文字列を出力します
func (c *Compiler) CompileToLLVM(entryPaths ...string) (string, *sema.Context, *ast.Program, error) {
	hirProg, semaCtx, concreteProg, err := c.CompileToHIR(entryPaths...)
	if err != nil {
		return "", nil, nil, err
	}

	// 5. LLVM バックエンドによるコード出力
	emitter := llvm.New(hirProg, semaCtx, c.target.Triple)
	llvmIR := emitter.Emit()

	return llvmIR, semaCtx, concreteProg, nil
}

// CompileFile は単一ファイルのコンパイル用ショートカット（後方互換性）
func (c *Compiler) CompileFile(entryPath string) (string, error) {
	ir, _, _, err := c.CompileToLLVM(entryPath)
	return ir, err
}
