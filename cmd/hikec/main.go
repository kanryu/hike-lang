package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"hikec-go/pkg/cgen"
	"hikec-go/pkg/codegen"
	"hikec-go/pkg/loader"
	"hikec-go/pkg/sema"
)

func printUsage() {
	fmt.Println("Usage: hikec <command> [options] <source.hike...>")
	fmt.Println("\nCommands:")
	fmt.Println("  emit-ir     Generate LLVM IR from Hike source (default)")
	fmt.Println("  build       Compile Hike source into a native/Wasm binary via Clang")
	fmt.Println("  run         Build and immediately execute the Hike program")
	fmt.Println("\nOptions for emit-ir:")
	fmt.Println("  -o <path>        Output LLVM IR file path (default: <source>.ll)")
	fmt.Println("  -header <path>   Output C/C++ header file path")
	fmt.Println("  -target <name>   Target platform (windows, linux, darwin, wasm32, wasm64)")
	fmt.Println("  -g               Generate DWARF debug information")
	fmt.Println("  -v               Enable verbose logging")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	var cmdArgs []string

	switch cmd {
	case "emit-ir":
		cmdArgs = os.Args[2:]
		runEmitIR(cmdArgs)
	case "build":
		cmdArgs = os.Args[2:]
		runBuild(cmdArgs)
	case "run":
		cmdArgs = os.Args[2:]
		runRun(cmdArgs)
	case "help", "-h", "--help":
		printUsage()
	default:
		// サブコマンド省略時は後方互換性のため emit-ir として実行
		cmdArgs = os.Args[1:]
		runEmitIR(cmdArgs)
	}
}

// -----------------------------------------------------------------------------
// emit-ir: LLVM IR生成処理
// -----------------------------------------------------------------------------
func runEmitIR(args []string) {
	outputLL := ""
	outputHeader := ""
	targetName := ""
	debugInfo := false
	verbose := false
	var sourceFiles []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-g" {
			debugInfo = true
		} else if arg == "-o" && i+1 < len(args) {
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
		} else if arg == "-v" || arg == "--verbose" {
			verbose = true
		} else if strings.HasPrefix(arg, "-") {
			// 未知のオプションは無視
		} else {
			sourceFiles = append(sourceFiles, arg)
		}
	}

	if len(sourceFiles) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no input files provided for emit-ir")
		os.Exit(1)
	}

	target, err := codegen.ParseTarget(targetName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target error: %v\n", err)
		os.Exit(1)
	}

	rootDir := "."
	if len(sourceFiles) > 0 {
		rootDir = filepath.Dir(sourceFiles[0])
		if rootDir == "" {
			rootDir = "."
		}
	}

	ld := loader.New(rootDir)
	ld.SetVerbose(verbose)

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

	srcPath := sourceFiles[0]
	cg := codegen.New(prog, semaCtx, target, srcPath, debugInfo)
	cg.SetVerbose(verbose)
	llvmIR := cg.Generate()

	if outputLL == "" {
		ext := filepath.Ext(srcPath)
		base := strings.TrimSuffix(filepath.Base(srcPath), ext)
		outputLL = base + ".ll"
	}

	if err := os.WriteFile(outputLL, []byte(llvmIR), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Codegen write error: %v\n", err)
		os.Exit(1)
	}
	if verbose {
		fmt.Printf("Compiled [%d target(s)] -> %s\n", len(sourceFiles), outputLL)
	}

	if outputHeader != "" {
		headerCode := cgen.GenerateHeader(prog, semaCtx, outputHeader)
		if err := os.WriteFile(outputHeader, []byte(headerCode), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Header write error: %v\n", err)
			os.Exit(1)
		}
		if verbose {
			fmt.Printf("Generated C/C++ Header -> %s\n", outputHeader)
		}
	}
}

// -----------------------------------------------------------------------------
// build: Clang を呼び出して実行可能バイナリを直接出力する
// -----------------------------------------------------------------------------
func runBuild(args []string) {
	outputBin := ""
	targetName := ""
	debugInfo := false
	var passThroughArgs []string
	var sourceFiles []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-o" && i+1 < len(args) {
			outputBin = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-o=") {
			outputBin = strings.TrimPrefix(arg, "-o=")
		} else if arg == "-target" && i+1 < len(args) {
			targetName = args[i+1]
			passThroughArgs = append(passThroughArgs, "-target", targetName)
			i++
		} else if arg == "-g" {
			debugInfo = true
			passThroughArgs = append(passThroughArgs, "-g")
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

	target, err := codegen.ParseTarget(targetName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target error: %v\n", err)
		os.Exit(1)
	}

	tempLL := filepath.Join(os.TempDir(), fmt.Sprintf("hike_build_%d.ll", os.Getpid()))
	defer os.Remove(tempLL)

	// 1. 中間 IR を生成
	emitArgs := append([]string{"-o", tempLL}, passThroughArgs...)
	runEmitIR(emitArgs)

	// 2. 出力バイナリ名の決定
	srcBase := strings.TrimSuffix(filepath.Base(sourceFiles[0]), filepath.Ext(sourceFiles[0]))
	if outputBin == "" {
		if target.IsWasm {
			outputBin = srcBase + ".wasm"
		} else if target.Triple == codegen.TargetX86_64Windows.Triple {
			outputBin = srcBase + ".exe"
		} else {
			outputBin = srcBase
		}
	}

	// 3. Clang 呼び出し（ネイティブ時にも --target を明示）
	var clangArgs []string
	if target.IsWasm {
		clangArgs = []string{
			"--target=" + target.Triple,
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
		clangArgs = append(clangArgs, "--target="+target.Triple, opt, tempLL, "-o", outputBin)
	}

	cmd := exec.Command("clang", clangArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Clang build failed: %v\n", err)
		os.Exit(1)
	}

	// run 以外から直接 build が呼ばれた場合のみ完了表示
	if !strings.Contains(outputBin, "hike_run_") {
		fmt.Printf("Build completed -> %s\n", outputBin)
	}
}

// -----------------------------------------------------------------------------
// run: ビルドして即時実行する
// -----------------------------------------------------------------------------
func runRun(args []string) {
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
		os.Exit(1)
	}
}
