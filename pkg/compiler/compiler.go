package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/backend/llvm"
	"hikec-go/pkg/backend/wabt"
	"hikec-go/pkg/diag"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/loader"
	"hikec-go/pkg/lower"
	"hikec-go/pkg/parser"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/target"
	"hikec-go/pkg/transform"
)

type Compiler struct {
	target         *target.Target
	verbose        bool
	wasmMode       string
	regionMode     bool
	retainRelease  bool
	goHikeMode     bool
	debugInfo      bool
	lineTablesOnly bool
	wabtDebug      *wabt.DebugInfo
	reporter       *diag.Reporter
}

// traceVV emits phase-level diagnostics while verbose tracing is enabled.
// Write small phase markers so native crashes cannot hide buffered console
// output. The trace is reset at the start of each LLVM compilation.
func (c *Compiler) traceVV(message string) {
	if c.verbose {
		path := ".tmp-hikec-vv-trace.log"
		previous, _ := os.ReadFile(path)
		previous = append(previous, []byte(message+"\n")...)
		_ = os.WriteFile(path, previous, 0644)
	}
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

// SetRetainRelease enables the experimental automatic retain/release
// lowering for string and slice values. The default is disabled.
func (c *Compiler) SetRetainRelease(enabled bool) { c.retainRelease = enabled }

// SetGoHikeMode enables compilation of Go-compatible self-hosting sources.
func (c *Compiler) SetGoHikeMode(enabled bool) { c.goHikeMode = enabled }

// SetDebugInfo enables source-level debug metadata emission for LLVM and WABT.
func (c *Compiler) SetDebugInfo(enabled bool) { c.debugInfo = enabled }

// SetDebugLineTablesOnly enables debug locations without emitting local
// variable or type metadata.
func (c *Compiler) SetDebugLineTablesOnly(enabled bool) {
	c.lineTablesOnly = enabled
	if enabled {
		c.debugInfo = true
	}
}

// WABTDebugInfo returns the source table collected by the WABT emitter during
// the most recent WAT compilation.  The command driver uses it after WABT has
// assembled the module so DWARF custom sections can be appended to the final
// Wasm binary.
func (c *Compiler) WABTDebugInfo() *wabt.DebugInfo { return c.wabtDebug }

func (c *Compiler) Reporter() *diag.Reporter {
	return c.reporter
}

// safeExecute は各コンパイルフェーズを安全に実行し、パニックが発生した場合も捕捉してエラー情報へ正規化する
func (c *Compiler) safeExecute(defaultFile string, fn func() error) error {
	c.traceVV("phase begin " + defaultFile)
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("%v\n%s", r, debug.Stack())
			c.traceVV("phase panic")
			c.reporter.Add(diag.ParseDiagnostic(defaultFile, msg))
		}
		c.traceVV("phase end " + defaultFile)
	}()

	if err := fn(); err != nil {
		_ = os.WriteFile(".tmp-hikec-vv-error.log", []byte(err.Error()+"\n"), 0644)
		c.traceVV("phase returned error")
		c.reporter.AddRaw(defaultFile, err.Error())
	}
	return nil
}

// CompileToHIR はフロントエンド・ミドルエンドを実行し、ターゲットに応じた HIR を生成します
func (c *Compiler) CompileToHIR(entryPaths ...string) (*hir.Program, *sema.Context, *ast.Program, error) {
	c.traceVV("CompileToHIR begin")
	if len(entryPaths) == 0 {
		c.traceVV("CompileToHIR rejected empty entry paths")
		return nil, nil, nil, fmt.Errorf("no input files provided")
	}

	c.reporter.Clear()
	primaryFile := entryPaths[0]

	targetTriple := ""
	if c.target != nil {
		targetTriple = c.target.Triple
		sema.SetTargetArchitecture(targetTriple)
		sema.SetTargetPointerBits(c.target.PointerBits)
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
	_ = c.safeExecute(primaryFile+" [load]", func() error {
		ld := loader.New(rootDir)
		ld.SetTarget(c.target)
		ld.SetVerbose(c.verbose)
		ld.SetGoHikeMode(c.goHikeMode)
		p, err := ld.Load(entryPaths...)
		if err != nil {
			return err
		}
		rawProg = p
		c.traceVV(fmt.Sprintf("load result decls=%d imports=%d", len(p.Decls), len(p.Imports)))
		return nil
	})
	if c.reporter.HasErrors() {
		c.traceVV("CompileToHIR failed stage=load")
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
	_ = c.safeExecute(primaryFile+" [sema]", func() error {
		ctx, err := sema.AnalyzeWithReporterModes(rawProg, c.reporter, primaryFile, c.regionMode, c.goHikeMode)
		if err != nil {
			return err
		}
		semaCtx = ctx
		return nil
	})
	if c.reporter.HasErrors() {
		c.traceVV("CompileToHIR failed stage=sema")
		return nil, nil, nil, c.reporter
	}

	// 3. ジェネリクス単相化フェーズ
	_ = c.safeExecute(primaryFile+" [transform]", func() error {
		tf := transform.New(rawProg, semaCtx)
		p, err := tf.Transform()
		if err != nil {
			return err
		}
		if err := transform.ValidateConcreteProgram(p); err != nil {
			return err
		}
		concreteProg = p
		return nil
	})
	if c.reporter.HasErrors() {
		c.traceVV("CompileToHIR failed stage=transform")
		return nil, nil, nil, c.reporter
	}

	// 4. HIR への Lowering フェーズ
	_ = c.safeExecute(primaryFile+" [lower]", func() error {
		is32Bit := (c.target != nil && (c.target.PointerBits == 32 || c.target.IsWasm || sema.PointerSize == 4 || strings.HasPrefix(targetTriple, "wasm32")))
		lw := lower.New(concreteProg, semaCtx)
		lw.Set32Bit(is32Bit)
		lw.SetRegionMode(c.regionMode)
		lw.SetRetainRelease(c.retainRelease)
		hirProg = lw.Lower()
		return nil
	})
	if c.reporter.HasErrors() {
		c.traceVV("CompileToHIR failed stage=lower")
		return nil, nil, nil, c.reporter
	}

	c.traceVV("CompileToHIR success")
	return hirProg, semaCtx, concreteProg, nil
}

// CompileToLLVM は HIR 生成を経て、最終的な LLVM IR 文字列を出力します
func (c *Compiler) CompileToLLVM(entryPaths ...string) (string, *sema.Context, *ast.Program, error) {
	if c.verbose {
		_ = os.WriteFile(".tmp-hikec-vv-trace.log", []byte{}, 0644)
	}
	c.traceVV("CompileToLLVM begin")
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
	_ = c.safeExecute(primaryFile, func() error {
		emitter := llvm.New(hirProg, semaCtx, targetTriple, primaryFile, c.debugInfo)
		if c.target != nil {
			emitter.SetPointerBits(c.target.PointerBits)
		}
		emitter.SetLineTablesOnly(c.lineTablesOnly)
		llvmIR = emitter.Emit()
		return nil
	})
	if c.reporter.HasErrors() {
		c.traceVV("CompileToLLVM failed stage=llvm_emit")
		return "", nil, nil, c.reporter
	}
	c.traceVV("CompileToLLVM success")
	return llvmIR, semaCtx, concreteProg, nil
}

// CompileToWAT lowers the source through HIR and emits WebAssembly text
// directly. It intentionally does not pass through LLVM or Clang.
func (c *Compiler) CompileToWAT(entryPaths ...string) (string, *sema.Context, *ast.Program, error) {
	c.wabtDebug = nil
	hirProg, semaCtx, concreteProg, err := c.CompileToHIR(entryPaths...)
	if err != nil {
		return "", nil, nil, err
	}
	emitter := wabt.New(hirProg, semaCtx)
	emitter.SetConcurrent(c.wasmMode == "concurrent")
	emitter.SetDebugInfo(c.debugInfo)
	emitter.SetLineTablesOnly(c.lineTablesOnly)
	wat := emitter.Emit()
	if c.debugInfo {
		c.wabtDebug = emitter.DebugInfo(entryPaths[0])
	}
	return wat, semaCtx, concreteProg, nil
}

// CompileSourceToWAT compiles one source string without consulting the
// filesystem. It is the browser/WASM entry point used by wasm-hikec.
func (c *Compiler) CompileSourceToWAT(source string) (string, *ast.Program, error) {
	filename := "input.hike"
	c.reporter.Clear()
	sema.SetTargetArchitecture(c.target.Triple)
	sema.SetTargetPointerBits(c.target.PointerBits)
	parserInstance := parser.New(lexer.New(source))
	p := parserInstance.ParseProgram()
	if len(parserInstance.Errors()) > 0 {
		return "", nil, fmt.Errorf("parse error in %s: %s", filename, strings.Join(parserInstance.Errors(), "\n"))
	}
	ctx, err := sema.AnalyzeWithReporterModes(p, c.reporter, filename, c.regionMode, c.goHikeMode)
	if err != nil || c.reporter.HasErrors() {
		if err != nil {
			return "", nil, err
		}
		return "", nil, c.reporter
	}
	concrete, err := transform.New(p, ctx).Transform()
	if err != nil {
		return "", nil, err
	}
	if err := transform.ValidateConcreteProgram(concrete); err != nil {
		return "", nil, err
	}
	lw := lower.New(concrete, ctx)
	lw.Set32Bit(c.target.IsWasm)
	lw.SetRegionMode(c.regionMode)
	program := lw.Lower()
	emitter := wabt.New(program, ctx)
	emitter.SetConcurrent(c.wasmMode == "concurrent")
	emitter.SetDebugInfo(c.debugInfo)
	emitter.SetLineTablesOnly(c.lineTablesOnly)
	wasmText := emitter.Emit()
	if c.debugInfo {
		c.wabtDebug = emitter.DebugInfo(filename)
	}
	return wasmText, concrete, nil
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
		if err := transform.ValidateConcreteProgram(p); err != nil {
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
		is32Bit := (c.target != nil && (c.target.PointerBits == 32 || c.target.IsWasm || sema.PointerSize == 4 || strings.HasPrefix(targetTriple, "wasm32")))
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
		emitter := wabt.New(hirProg, semaCtx)
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
