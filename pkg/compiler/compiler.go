package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/backend/llvm"
	"hikec-go/pkg/diag"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/loader"
	"hikec-go/pkg/lower"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/target"
	"hikec-go/pkg/transform"
)

type Compiler struct {
	target     *target.Target
	verbose    bool
	wasmMode   string
	regionMode bool
	goHikeMode bool
	reporter   *diag.Reporter
}

func New(tgt *target.Target) *Compiler {
	if tgt == nil {
		tgt = target.DefaultTarget()
	}
	return &Compiler{
		target:   tgt,
		verbose:  false,
		wasmMode: "normal",
		reporter: diag.NewReporter(),
	}
}

func (c *Compiler) SetWasmMode(mode string) {
	if mode == "concurrent" {
		c.wasmMode = mode
	} else {
		c.wasmMode = "normal"
	}
}

func (c *Compiler) SetVerbose(v bool) {
	c.verbose = v
}

// SetRegionMode enables the optional --alloc=region arena allocator.
func (c *Compiler) SetRegionMode(enabled bool) { c.regionMode = enabled }

// SetGoHikeMode enables compilation of Go-compatible self-hosting sources.
func (c *Compiler) SetGoHikeMode(enabled bool) { c.goHikeMode = enabled }

func (c *Compiler) Reporter() *diag.Reporter {
	return c.reporter
}

// safeExecute は各コンパイルフェーズを安全に実行し、パニックが発生した場合も捕捉してエラー情報へ正規化する
func (c *Compiler) safeExecute(defaultFile string, fn func() error) error {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("%v", r)
			c.reporter.Add(diag.ParseDiagnostic(defaultFile, msg))
		}
	}()

	if err := fn(); err != nil {
		c.reporter.AddRaw(defaultFile, err.Error())
	}
	return nil
}

// CompileToHIR はフロントエンド・ミドルエンドを実行し、ターゲットに応じた HIR を生成します
func (c *Compiler) CompileToHIR(entryPaths ...string) (*hir.Program, *sema.Context, *ast.Program, error) {
	if len(entryPaths) == 0 {
		return nil, nil, nil, fmt.Errorf("no input files provided")
	}

	c.reporter.Clear()
	primaryFile := entryPaths[0]

	targetTriple := ""
	if c.target != nil {
		targetTriple = c.target.Triple
		sema.SetTargetArchitecture(targetTriple)
	}

	rootDir := filepath.Dir(primaryFile)
	if rootDir == "" {
		rootDir = "."
	}

	var rawProg *ast.Program
	var semaCtx *sema.Context
	var concreteProg *ast.Program
	var hirProg *hir.Program

	// 1. パッケージ探索・構文解析フェーズ
	_ = c.safeExecute(primaryFile, func() error {
		ld := loader.New(rootDir)
		ld.SetTarget(c.target)
		ld.SetVerbose(c.verbose)
		ld.SetGoHikeMode(c.goHikeMode)
		p, err := ld.Load(entryPaths...)
		if err != nil {
			return err
		}
		rawProg = p
		return nil
	})
	if c.reporter.HasErrors() {
		return nil, nil, nil, c.reporter
	}
	if c.target != nil && c.target.IsWasm && c.wasmMode != "concurrent" {
		for _, decl := range rawProg.Decls {
			if jfn, ok := decl.(*ast.JFuncDecl); ok && jfn.HasDirectives {
				return nil, nil, nil, fmt.Errorf("jfunc %s uses main/worker directives and requires -wasm-mode=concurrent", jfn.Name.Value)
			}
		}
	}

	// 2. 意味解析・型検査フェーズ
	_ = c.safeExecute(primaryFile, func() error {
		ctx, err := sema.AnalyzeWithReporterModes(rawProg, c.reporter, primaryFile, c.regionMode, c.goHikeMode)
		if err != nil {
			return err
		}
		semaCtx = ctx
		return nil
	})
	if c.reporter.HasErrors() {
		return nil, nil, nil, c.reporter
	}

	// 3. ジェネリクス単相化フェーズ
	_ = c.safeExecute(primaryFile, func() error {
		tf := transform.New(rawProg, semaCtx)
		p, err := tf.Transform()
		if err != nil {
			return err
		}
		concreteProg = p
		return nil
	})
	if c.reporter.HasErrors() {
		return nil, nil, nil, c.reporter
	}

	// 4. HIR への Lowering フェーズ
	_ = c.safeExecute(primaryFile, func() error {
		is32Bit := (c.target != nil && (c.target.IsWasm || sema.PointerSize == 4 || strings.HasPrefix(targetTriple, "wasm32")))
		lw := lower.New(concreteProg, semaCtx)
		lw.Set32Bit(is32Bit)
		lw.SetRegionMode(c.regionMode)
		hirProg = lw.Lower()
		return nil
	})
	if c.reporter.HasErrors() {
		return nil, nil, nil, c.reporter
	}

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

	primaryFile := entryPaths[0]
	var llvmIR string

	// 5. LLVM バックエンドによるコード出力フェーズ
	_ = c.safeExecute(primaryFile, func() error {
		emitter := llvm.New(hirProg, semaCtx, targetTriple)
		llvmIR = emitter.Emit()
		return nil
	})
	if c.reporter.HasErrors() {
		return "", nil, nil, c.reporter
	}

	return llvmIR, semaCtx, concreteProg, nil
}

// Compile はコンパイルを実行し、エラーが発生した場合は Go コンパイラ形式で stderr に出力して終了します
func (c *Compiler) Compile(entryPaths ...string) error {
	_, _, _, err := c.CompileToLLVM(entryPaths...)
	if err != nil {
		if c.reporter.HasErrors() {
			fmt.Fprintln(os.Stderr, c.reporter.FormatAll())
			return fmt.Errorf("compilation failed with %d error(s)", c.reporter.ErrorCount())
		}
		return err
	}
	return nil
}

// CompileProgram は AST Program から直接コンパイルを実行し、フェーズゲート制御を行う
func (c *Compiler) CompileProgram(prog *ast.Program, filename string) error {
	c.reporter.Clear()

	// 1. Sema フェーズ (エラーが出ても最後まで回す)
	var semaCtx *sema.Context
	_ = c.safeExecute(filename, func() error {
		ctx, err := sema.AnalyzeWithReporterModes(prog, c.reporter, filename, c.regionMode, c.goHikeMode)
		if err != nil {
			return err
		}
		semaCtx = ctx
		return nil
	})
	if c.reporter.HasErrors() {
		fmt.Fprintln(os.Stderr, c.reporter.FormatAll())
		return fmt.Errorf("compilation failed with %d error(s)", c.reporter.ErrorCount())
	}

	// 2. Transform フェーズ
	var concreteProg *ast.Program
	_ = c.safeExecute(filename, func() error {
		tf := transform.New(prog, semaCtx)
		p, err := tf.Transform()
		if err != nil {
			return err
		}
		concreteProg = p
		return nil
	})
	if c.reporter.HasErrors() {
		fmt.Fprintln(os.Stderr, c.reporter.FormatAll())
		return fmt.Errorf("compilation failed with %d error(s)", c.reporter.ErrorCount())
	}

	// 3. Lower (HIR) / Emit (LLVM) フェーズ
	var hirProg *hir.Program
	targetTriple := ""
	if c.target != nil {
		targetTriple = c.target.Triple
	}
	_ = c.safeExecute(filename, func() error {
		is32Bit := (c.target != nil && (c.target.IsWasm || sema.PointerSize == 4 || strings.HasPrefix(targetTriple, "wasm32")))
		lw := lower.New(concreteProg, semaCtx)
		lw.Set32Bit(is32Bit)
		lw.SetRegionMode(c.regionMode)
		hirProg = lw.Lower()
		return nil
	})
	if c.reporter.HasErrors() {
		fmt.Fprintln(os.Stderr, c.reporter.FormatAll())
		return fmt.Errorf("compilation failed with %d error(s)", c.reporter.ErrorCount())
	}

	_ = c.safeExecute(filename, func() error {
		emitter := llvm.New(hirProg, semaCtx, targetTriple)
		_ = emitter.Emit()
		return nil
	})
	if c.reporter.HasErrors() {
		fmt.Fprintln(os.Stderr, c.reporter.FormatAll())
		return fmt.Errorf("compilation failed with %d error(s)", c.reporter.ErrorCount())
	}

	return nil
}

// CompileFile は単一ファイルのコンパイル用ショートカット
func (c *Compiler) CompileFile(entryPath string) (string, error) {
	ir, _, _, err := c.CompileToLLVM(entryPath)
	return ir, err
}
