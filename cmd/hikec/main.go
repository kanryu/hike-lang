package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/backend/wabt"
	"hikec-go/pkg/codegen"
	gocode "hikec-go/pkg/codegen/go"
	"hikec-go/pkg/codegen/symbols"
	"hikec-go/pkg/compiler"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/target"
	"hikec-go/pkg/toolchain"
)

func getDefaultTargetName() string {
	switch runtime.GOOS {
	case "windows":
		return "x86_64-w64-windows-gnu"
	case "darwin":
		return "arm64-apple-darwin"
	default:
		return "x86_64-unknown-linux-gnu"
	}
}

func printUsage() {
	fmt.Println("Usage: hikec <command> [options] <source.hike... | directory>")
	fmt.Println("\nCommands:")
	fmt.Println("  go          Compile all .go.hike files in directory into a single .syso object")
	fmt.Println("  emit-ir     Generate LLVM IR or Wabt WAT from Hike source (default)")
	fmt.Println("  emit-js     Generate the WebAssembly JavaScript runtime")
	fmt.Println("  build       Compile Hike source into a native/Wasm binary (Wabt uses wat2wasm)")
	fmt.Println("  run         Build and immediately execute the Hike program (supports native and Wasm via Node.js)")
	fmt.Println("  get         Download modules listed in hike.mod into .hike/deps")
	fmt.Println("\nOptions for go:")
	fmt.Println("  -o <path>        Output .syso file path")
	fmt.Println("  -target <name>   Target platform (windows, windows-msvc, linux, darwin, wasm32, wabt)")
	fmt.Println("  -v               Enable verbose logging")
	fmt.Println("  -vv              Enable detailed (instruction-level) verbose logging")
	fmt.Println("\nOptions for emit-ir / build / run:")
	fmt.Println("  -o <path>        Output file path (default: <source>.ll, <source>.wasm, or executable)")
	fmt.Println("  -header <path>   Output C/C++ header file path")
	fmt.Println("  -target <name>   Target platform (windows, windows-msvc, linux, darwin, wasm32, wasm64, wabt)")
	fmt.Println("  -wasm-mode <mode> WebAssembly runtime mode: normal or concurrent")
	fmt.Println("  --alloc=<mode> Allocation mode: heap (default) or region")
	fmt.Println("  -go-hike=1       Enable Go-compatible self-hosting mode (.go sources and Go replacements)")
	fmt.Println("  -cflags <flags>  Additional flags passed directly to Clang")
	fmt.Println("  -g               Generate DWARF debug information")
	fmt.Println("  --export-symbols <path>  Export source symbols as JSON")
	fmt.Println("  -v               Enable verbose logging")
	fmt.Println("  -vv              Enable detailed (instruction-level) verbose logging")
}

func isGoHikeFlag(arg string) bool {
	return arg == "-go-hike" || arg == "--go-hike" || arg == "-go-hike=1" || arg == "--go-hike=1"
}

func main() {
	for _, arg := range os.Args {
		if arg == "-vv" || arg == "--vv" {
			os.Setenv("HIKEC_VERBOSE_LEVEL", "2")
			break
		}
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	var cmdArgs []string

	switch cmd {
	case "go":
		cmdArgs = os.Args[2:]
		runGo(cmdArgs)
	case "emit-ir":
		cmdArgs = os.Args[2:]
		runEmitIR(cmdArgs)
	case "emit-js":
		cmdArgs = os.Args[2:]
		runEmitJS(cmdArgs)
	case "build":
		cmdArgs = os.Args[2:]
		runBuild(cmdArgs)
	case "run":
		cmdArgs = os.Args[2:]
		runRun(cmdArgs)
	case "get":
		cmdArgs = os.Args[2:]
		runGet(cmdArgs)
	case "help", "-h", "--help":
		printUsage()
	default:
		cmdArgs = os.Args[1:]
		runEmitIR(cmdArgs)
	}
}

// -----------------------------------------------------------------------------
// go: ディレクトリ内の *.go.hike を集約して単一 .syso を生成
// -----------------------------------------------------------------------------
func runGo(args []string) {
	opts := gocode.GoBuildOptions{
		Dir:        ".",
		TargetName: getDefaultTargetName(),
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" && i+1 < len(args) {
			opts.OutputFile = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-o=") {
			opts.OutputFile = strings.TrimPrefix(arg, "-o=")
		} else if (arg == "-target" || arg == "--target") && i+1 < len(args) {
			opts.TargetName = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-target=") || strings.HasPrefix(arg, "--target=") {
			opts.TargetName = strings.SplitN(arg, "=", 2)[1]
		} else if arg == "-vv" || arg == "--vv" {
			opts.Verbose = true
			os.Setenv("HIKEC_VERBOSE_LEVEL", "2")
		} else if arg == "-v" || arg == "--verbose" {
			opts.Verbose = true
		} else if !strings.HasPrefix(arg, "-") {
			opts.Dir = arg
		}
	}

	if err := gocode.BuildGoPackage(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// -----------------------------------------------------------------------------
// emit-ir: LLVM IR生成処理
// -----------------------------------------------------------------------------
func runEmitIR(args []string) {
	outputLL := ""
	outputHeader := ""
	targetName := getDefaultTargetName()
	wasmMode := "normal"
	regionMode := false
	goHikeMode := false
	debugInfo := false
	exportSymbolsPath := ""
	verbose := false
	var sourceFiles []string

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
		} else if (arg == "-target" || arg == "--target") && i+1 < len(args) {
			targetName = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-target=") || strings.HasPrefix(arg, "--target=") {
			parts := strings.SplitN(arg, "=", 2)
			targetName = parts[1]
		} else if (arg == "-wasm-mode" || arg == "--wasm-mode") && i+1 < len(args) {
			wasmMode = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-wasm-mode=") || strings.HasPrefix(arg, "--wasm-mode=") {
			wasmMode = strings.SplitN(arg, "=", 2)[1]
		} else if arg == "--alloc=region" || arg == "-alloc=region" {
			regionMode = true
		} else if isGoHikeFlag(arg) {
			goHikeMode = true
		} else if arg == "-g" || arg == "--debug" {
			debugInfo = true
		} else if (arg == "--export-symbols" || arg == "-export-symbols") && i+1 < len(args) {
			exportSymbolsPath = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--export-symbols=") || strings.HasPrefix(arg, "-export-symbols=") {
			exportSymbolsPath = strings.SplitN(arg, "=", 2)[1]
		} else if arg == "-cflags" && i+1 < len(args) {
			i++
		} else if strings.HasPrefix(arg, "-cflags=") {
			// スキップ
		} else if arg == "-vv" || arg == "--vv" {
			verbose = true
			os.Setenv("HIKEC_VERBOSE_LEVEL", "2")
		} else if arg == "-v" || arg == "--verbose" {
			verbose = true
		} else if strings.HasPrefix(arg, "-") {
			// 未知フラグはスキップ
		} else {
			sourceFiles = append(sourceFiles, arg)
		}
	}

	if len(sourceFiles) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no input files provided for emit-ir")
		os.Exit(1)
	}
	if wasmMode != "normal" && wasmMode != "concurrent" {
		fmt.Fprintf(os.Stderr, "Error: invalid -wasm-mode %q (want normal or concurrent)\n", wasmMode)
		os.Exit(1)
	}
	tgt, err := target.ParseTarget(targetName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target error: %v\n", err)
		os.Exit(1)
	}
	comp := compiler.New(tgt)
	comp.SetVerbose(verbose)
	comp.SetWasmMode(wasmMode)
	comp.SetRegionMode(regionMode)
	comp.SetGoHikeMode(goHikeMode)
	comp.SetDebugInfo(debugInfo)

	var llvmIR string
	var semaCtx *sema.Context
	var prog *ast.Program
	if tgt.Name == target.TargetWabt.Name {
		var wat string
		wat, semaCtx, prog, err = comp.CompileToWAT(sourceFiles...)
		llvmIR = wat
	} else {
		llvmIR, semaCtx, prog, err = comp.CompileToLLVM(sourceFiles...)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Compilation error: %v\n", err)
		os.Exit(1)
	}
	if exportSymbolsPath != "" {
		file, writeErr := os.Create(exportSymbolsPath)
		if writeErr == nil {
			writeErr = symbols.WriteJSON(file, prog, semaCtx)
			closeErr := file.Close()
			if writeErr == nil {
				writeErr = closeErr
			}
		}
		if writeErr != nil {
			fmt.Fprintf(os.Stderr, "Symbol export error: %v\n", writeErr)
			os.Exit(1)
		}
	}

	srcPath := sourceFiles[0]
	if outputLL == "" {
		ext := filepath.Ext(srcPath)
		base := strings.TrimSuffix(filepath.Base(srcPath), ext)
		outputLL = base + ".ll"
		if tgt.Name == target.TargetWabt.Name {
			outputLL = base + ".wat"
		}
	}

	if err := os.WriteFile(outputLL, []byte(llvmIR), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Codegen write error: %v\n", err)
		os.Exit(1)
	}
	if verbose {
		fmt.Printf("Compiled [%d target(s)] -> %s (Target: %s)\n", len(sourceFiles), outputLL, tgt.Triple)
	}

	if outputHeader != "" {
		headerCode := codegen.GenerateHeader(prog, semaCtx, outputHeader)
		if err := os.WriteFile(outputHeader, []byte(headerCode), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Header write error: %v\n", err)
			os.Exit(1)
		}
		if verbose {
			fmt.Printf("Generated C/C++ Header -> %s\n", outputHeader)
		}
	}
	if tgt.Name == target.TargetWabt.Name {
		runtimePath := filepath.Join(filepath.Dir(outputLL), "runtime.js")
		if err := codegen.WriteWasmJSRuntimeMode(runtimePath, wasmMode, prog); err != nil {
			fmt.Fprintf(os.Stderr, "Runtime write error: %v\n", err)
			os.Exit(1)
		}
	}
}

// -----------------------------------------------------------------------------
// emit-js: WebAssembly runtime.js 生成
// -----------------------------------------------------------------------------
func runEmitJS(args []string) {
	output := ""
	targetName := "wasm32"
	wasmMode := "normal"
	regionMode := false
	goHikeMode := false
	verbose := false
	var sourceFiles []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-o" && i+1 < len(args):
			output = args[i+1]
			i++
		case strings.HasPrefix(arg, "-o="):
			output = strings.TrimPrefix(arg, "-o=")
		case (arg == "-target" || arg == "--target") && i+1 < len(args):
			targetName = args[i+1]
			i++
		case strings.HasPrefix(arg, "-target=") || strings.HasPrefix(arg, "--target="):
			targetName = strings.SplitN(arg, "=", 2)[1]
		case (arg == "-wasm-mode" || arg == "--wasm-mode") && i+1 < len(args):
			wasmMode = args[i+1]
			i++
		case strings.HasPrefix(arg, "-wasm-mode=") || strings.HasPrefix(arg, "--wasm-mode="):
			wasmMode = strings.SplitN(arg, "=", 2)[1]
		case arg == "--alloc=region" || arg == "-alloc=region":
			regionMode = true
		case isGoHikeFlag(arg):
			goHikeMode = true
		case arg == "-vv" || arg == "--vv":
			verbose = true
			os.Setenv("HIKEC_VERBOSE_LEVEL", "2")
		case arg == "-v" || arg == "--verbose":
			verbose = true
		case !strings.HasPrefix(arg, "-"):
			sourceFiles = append(sourceFiles, arg)
		}
	}
	if len(sourceFiles) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no input files provided for emit-js")
		os.Exit(1)
	}
	tgt, err := target.ParseTarget(targetName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target error: %v\n", err)
		os.Exit(1)
	}
	if !tgt.IsWasm {
		fmt.Fprintln(os.Stderr, "Error: emit-js requires a WebAssembly target")
		os.Exit(1)
	}
	if wasmMode != "normal" && wasmMode != "concurrent" {
		fmt.Fprintf(os.Stderr, "Error: invalid -wasm-mode %q (want normal or concurrent)\n", wasmMode)
		os.Exit(1)
	}
	comp := compiler.New(tgt)
	comp.SetVerbose(verbose)
	comp.SetWasmMode(wasmMode)
	comp.SetRegionMode(regionMode)
	comp.SetGoHikeMode(goHikeMode)
	_, _, program, err := comp.CompileToLLVM(sourceFiles...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Compilation error: %v\n", err)
		os.Exit(1)
	}
	if output == "" {
		output = filepath.Join(filepath.Dir(sourceFiles[0]), "runtime.js")
	}
	if err := codegen.WriteWasmJSRuntimeMode(output, wasmMode, program); err != nil {
		fmt.Fprintf(os.Stderr, "Runtime write error: %v\n", err)
		os.Exit(1)
	}
	if verbose {
		fmt.Printf("Generated WebAssembly JS Runtime -> %s (mode: %s)\n", output, wasmMode)
	}
}

// -----------------------------------------------------------------------------
// build: Clang を呼び出して実行可能バイナリを出力する
// -----------------------------------------------------------------------------
func runBuild(args []string) {
	outputBin := ""
	targetName := getDefaultTargetName()
	extraCflags := ""
	debugInfo := false
	exportSymbolsPath := ""
	sourceMapBaseURL := ""
	embedSourceMap := true
	verbose := false
	wasmMode := "normal"
	regionMode := false
	goHikeMode := false
	var passThroughArgs []string
	var sourceFiles []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" && i+1 < len(args) {
			outputBin = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-o=") {
			outputBin = strings.TrimPrefix(arg, "-o=")
		} else if (arg == "-target" || arg == "--target") && i+1 < len(args) {
			targetName = args[i+1]
			passThroughArgs = append(passThroughArgs, "-target", targetName)
			i++
		} else if strings.HasPrefix(arg, "-target=") || strings.HasPrefix(arg, "--target=") {
			targetName = strings.SplitN(arg, "=", 2)[1]
			passThroughArgs = append(passThroughArgs, "-target", targetName)
		} else if (arg == "-wasm-mode" || arg == "--wasm-mode") && i+1 < len(args) {
			wasmMode = args[i+1]
			passThroughArgs = append(passThroughArgs, "-wasm-mode", wasmMode)
			i++
		} else if strings.HasPrefix(arg, "-wasm-mode=") || strings.HasPrefix(arg, "--wasm-mode=") {
			wasmMode = strings.SplitN(arg, "=", 2)[1]
			passThroughArgs = append(passThroughArgs, "-wasm-mode", wasmMode)
		} else if arg == "--alloc=region" || arg == "-alloc=region" {
			regionMode = true
		} else if isGoHikeFlag(arg) {
			goHikeMode = true
			passThroughArgs = append(passThroughArgs, "-go-hike=1")
		} else if arg == "-cflags" && i+1 < len(args) {
			extraCflags = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-cflags=") {
			extraCflags = strings.TrimPrefix(arg, "-cflags=")
		} else if arg == "-g" {
			debugInfo = true
			passThroughArgs = append(passThroughArgs, "-g")
		} else if (arg == "--export-symbols" || arg == "-export-symbols") && i+1 < len(args) {
			exportSymbolsPath = args[i+1]
			passThroughArgs = append(passThroughArgs, "--export-symbols", exportSymbolsPath)
			i++
		} else if strings.HasPrefix(arg, "--export-symbols=") || strings.HasPrefix(arg, "-export-symbols=") {
			exportSymbolsPath = strings.SplitN(arg, "=", 2)[1]
			passThroughArgs = append(passThroughArgs, "--export-symbols", exportSymbolsPath)
		} else if arg == "--source-map-base" && i+1 < len(args) {
			sourceMapBaseURL = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--source-map-base=") {
			sourceMapBaseURL = strings.TrimPrefix(arg, "--source-map-base=")
		} else if arg == "--no-source-map" {
			// DWARF and source maps can describe the same Wasm addresses in
			// different ways.  Browser debugging uses DWARF as the authoritative
			// mapping when this switch is selected.
			embedSourceMap = false
		} else if arg == "-vv" || arg == "--vv" {
			verbose = true
			passThroughArgs = append(passThroughArgs, "-vv")
			os.Setenv("HIKEC_VERBOSE_LEVEL", "2")
		} else if arg == "-v" || arg == "--verbose" {
			verbose = true
			passThroughArgs = append(passThroughArgs, "-v")
		} else if !strings.HasPrefix(arg, "-") {
			sourceFiles = append(sourceFiles, arg)
			passThroughArgs = append(passThroughArgs, arg)
		} else {
			passThroughArgs = append(passThroughArgs, arg)
		}
	}

	if len(sourceFiles) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no input files provided for build")
		os.Exit(1)
	}

	tgt, err := target.ParseTarget(targetName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target error: %v\n", err)
		os.Exit(1)
	}
	if wasmMode != "normal" && wasmMode != "concurrent" {
		fmt.Fprintf(os.Stderr, "Error: invalid -wasm-mode %q (want normal or concurrent)\n", wasmMode)
		os.Exit(1)
	}
	useWabtBackend := tgt.Name == target.TargetWabt.Name || (goHikeMode && tgt.IsWasm)

	// Collect jfunc declarations for the generated wasm runtime. The normal
	// emit-ir path remains authoritative for LLVM output; this frontend pass
	// only supplies source-owned JavaScript bindings to runtime.js.
	var runtimeProgram *ast.Program
	var wabtCompiler *compiler.Compiler
	if tgt.IsWasm {
		wabtCompiler = compiler.New(tgt)
		wabtCompiler.SetVerbose(verbose)
		wabtCompiler.SetWasmMode(wasmMode)
		wabtCompiler.SetRegionMode(regionMode)
		wabtCompiler.SetGoHikeMode(goHikeMode)
		wabtCompiler.SetDebugInfo(debugInfo)
		var program *ast.Program
		var compileErr error
		if useWabtBackend {
			_, _, program, compileErr = wabtCompiler.CompileToWAT(sourceFiles...)
		} else {
			_, _, program, compileErr = wabtCompiler.CompileToLLVM(sourceFiles...)
		}
		if compileErr != nil {
			fmt.Fprintf(os.Stderr, "Compilation error: %v\n", compileErr)
			os.Exit(1)
		}
		runtimeProgram = program
	}

	tempLL := filepath.Join(os.TempDir(), fmt.Sprintf("hike_build_%d.ll", os.Getpid()))
	defer os.Remove(tempLL)

	emitArgs := append([]string{"-o", tempLL}, passThroughArgs...)
	if useWabtBackend {
		// Go-Hike wasm32 uses the WAT backend while retaining the wasm32 ABI.
		// Append the effective target so it wins over the user-facing wasm32
		// spelling already present in passThroughArgs.
		emitArgs = append(emitArgs, "-target", target.TargetWabt.Name)
	}
	if regionMode {
		emitArgs = append(emitArgs, "--alloc=region")
	}
	runEmitIR(emitArgs)

	srcBase := strings.TrimSuffix(filepath.Base(sourceFiles[0]), filepath.Ext(sourceFiles[0]))
	if outputBin == "" {
		if tgt.IsWasm {
			outputBin = srcBase + ".wasm"
		} else if tgt.Triple == target.TargetX86_64Windows.Triple || tgt.Triple == target.TargetX86_64WindowsMSVC.Triple {
			outputBin = srcBase + ".exe"
		} else {
			outputBin = srcBase
		}
	}

	if useWabtBackend {
		wat2wasmArgs := []string{"--enable-threads", tempLL, "-o", outputBin}
		if debugInfo {
			// Preserve the WAT function and local names in the standard Wasm
			// name custom section. This is the WABT counterpart of keeping
			// LLVM debug symbols when -g is enabled.
			wat2wasmArgs = append(wat2wasmArgs, "--debug-names")
		}
		cmd := exec.Command("wat2wasm", wat2wasmArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "wat2wasm build failed: %v\n", err)
			os.Exit(1)
		}
		if debugInfo {
			wasm, readErr := os.ReadFile(outputBin)
			if readErr != nil {
				fmt.Fprintf(os.Stderr, "WABT debug read failed: %v\n", readErr)
				os.Exit(1)
			}
			mapPath := outputBin + ".map"
			sourceMapURL := "./" + filepath.Base(mapPath)
			if sourceMapBaseURL != "" {
				sourceMapURL = strings.TrimRight(sourceMapBaseURL, "/") + "/" + filepath.Base(mapPath)
			}
			sourceURL := ""
			if sourceMapBaseURL != "" {
				sourceURL = strings.TrimRight(sourceMapBaseURL, "/") + "/" + filepath.Base(sourceFiles[0])
			}
			debug := wabtCompiler.WABTDebugInfo()
			if debug != nil {
				debug.SourceURL = sourceURL
			}
			wasm, dwarfErr := wabt.AppendDWARF(wasm, debug)
			if dwarfErr != nil {
				fmt.Fprintf(os.Stderr, "WABT DWARF emission failed: %v\n", dwarfErr)
				os.Exit(1)
			}
			sourceMap, mapErr := wabt.SourceMapJSON(wasm, debug)
			if mapErr != nil {
				fmt.Fprintf(os.Stderr, "WABT source map emission failed: %v\n", mapErr)
				os.Exit(1)
			}
			if writeMapErr := os.WriteFile(mapPath, sourceMap, 0644); writeMapErr != nil {
				fmt.Fprintf(os.Stderr, "WABT source map write failed: %v\n", writeMapErr)
				os.Exit(1)
			}
			if embedSourceMap {
				wasm = wabt.AppendSourceMapURL(wasm, sourceMapURL)
			}
			if writeErr := os.WriteFile(outputBin, wasm, 0644); writeErr != nil {
				fmt.Fprintf(os.Stderr, "WABT debug write failed: %v\n", writeErr)
				os.Exit(1)
			}
		}
	} else {
		var clangArgs []string
		if tgt.IsWasm {
			clangArgs = []string{
				"--target=" + tgt.Triple,
				"-O2", "-nostdlib",
				"-Wl,--no-entry", "-Wl,--export-all", "-Wl,--allow-undefined",
				tempLL, "-o", outputBin,
			}
		} else {
			opt := "-O2"
			if debugInfo {
				opt = "-O0"
				clangArgs = append(clangArgs, "-g")
			}
			clangArgs = append(clangArgs, "--target="+tgt.Triple, opt, tempLL, "-o", outputBin)
		}

		command := "clang"
		args := clangArgs
		if profileName := nativeToolchainProfile(tgt); profileName != "" {
			if cfg, loadErr := toolchain.LoadFromWorkingDirectory(); loadErr == nil {
				if configuredCommand, configuredArgs, ok := cfg.Command(profileName, tempLL, outputBin); ok {
					command = configuredCommand
					args = configuredArgs
				}
			} else if !os.IsNotExist(loadErr) {
				fmt.Fprintf(os.Stderr, "Toolchain config error: %v\n", loadErr)
				os.Exit(1)
			}
		}
		if command == "clang" {
			if tgt.Cflags != "" {
				args = append(args, strings.Fields(tgt.Cflags)...)
			}
			if extraCflags != "" {
				args = append(args, strings.Fields(extraCflags)...)
			}
		}

		cmd := exec.Command(command, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "%s build failed: %v\n", command, err)
			os.Exit(1)
		}
	}

	// Wasm ターゲット時は runtime.js を自動生成して配置
	if tgt.IsWasm {
		runtimePath := filepath.Join(filepath.Dir(outputBin), "runtime.js")
		if err := codegen.WriteWasmJSRuntimeMode(runtimePath, wasmMode, runtimeProgram); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate runtime.js: %v\n", err)
		} else if !strings.Contains(outputBin, "hike_run_") && verbose {
			fmt.Printf("Generated Wasm JS Runtime -> %s\n", runtimePath)
		}
	}

	if !strings.Contains(outputBin, "hike_run_") {
		fmt.Printf("Build completed -> %s\n", outputBin)
	}
}

func nativeToolchainProfile(tgt *target.Target) string {
	if tgt == nil || tgt.IsWasm {
		return ""
	}
	switch tgt.Name {
	case target.TargetX86_64WindowsMSVC.Name:
		return "windows-msvc"
	case target.TargetX86_64Windows.Name:
		return "windows-gnu"
	default:
		return ""
	}
}

// -------------------------------------------------------------
// run: ビルドして即時実行する (Native / Wasm 両対応)
// -------------------------------------------------------------
func runRun(args []string) {
	for _, arg := range args {
		if arg == "-vv" || arg == "--vv" {
			os.Setenv("HIKEC_VERBOSE_LEVEL", "2")
			break
		}
	}
	targetName := getDefaultTargetName()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "-target" || arg == "--target") && i+1 < len(args) {
			targetName = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-target=") || strings.HasPrefix(arg, "--target=") {
			targetName = strings.SplitN(arg, "=", 2)[1]
		} else if arg == "--alloc=region" || arg == "-alloc=region" {
			// Forwarded to build, which forwards it to emit-ir.
		}
	}

	tgt, err := target.ParseTarget(targetName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target error: %v\n", err)
		os.Exit(1)
	}

	// 1. WebAssembly ターゲットの場合は Node.js 上で自動実行
	if tgt.IsWasm {
		tempWasm := filepath.Join(os.TempDir(), fmt.Sprintf("hike_run_%d.wasm", os.Getpid()))
		defer os.Remove(tempWasm)
		runtimeJS := filepath.Join(os.TempDir(), "runtime.js")
		defer os.Remove(runtimeJS)

		buildArgs := append([]string{"-o", tempWasm}, args...)
		runBuild(buildArgs)

		nodeScript := fmt.Sprintf(`
const { HikeRuntime } = require(%q);
const rt = new HikeRuntime();
rt.load(%q).then(exports => {
    if (typeof exports.main === 'function') {
        const res = exports.main();
        if (typeof res === 'number' && res !== 0) {
            process.exit(res);
        }
    }
}).catch(err => {
    console.error(err);
    process.exit(1);
});
`, filepath.ToSlash(runtimeJS), filepath.ToSlash(tempWasm))

		cmd := exec.Command("node", "-e", nodeScript)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "failed to run wasm with node: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// 2. ネイティブバイナリの即時実行
	tempExe := filepath.Join(os.TempDir(), fmt.Sprintf("hike_run_%d.exe", os.Getpid()))
	defer os.Remove(tempExe)

	buildArgs := append([]string{"-o", tempExe}, args...)
	runBuild(buildArgs)

	cmd := exec.Command(tempExe)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "failed to run executable: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
