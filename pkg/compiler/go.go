package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
	"hikec-go/pkg/target"
)

// GoBuildOptions: go サブコマンドのビルドオプション
type GoBuildOptions struct {
	Dir        string // 対象ディレクトリ（省略時はカレントディレクトリ）
	OutputFile string // 出力 .syso ファイルパス（省略時は自動決定）
	TargetName string // ターゲットOS/ARCH
	Verbose    bool   // 詳細ログ出力
}

// BuildGoPackage は指定ディレクトリ内の全 *.go.hike ファイルを探索し、
// 単一の .syso オブジェクトファイルへとコンパイルします。
func BuildGoPackage(opts GoBuildOptions) error {
	targetDir := opts.Dir
	if targetDir == "" {
		targetDir = "."
	}

	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	// 1. *.go.hike ソースファイルを収集
	files, err := os.ReadDir(absDir)
	if err != nil {
		return fmt.Errorf("failed to read directory '%s': %w", absDir, err)
	}

	var hikeFiles []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".go.hike") {
			hikeFiles = append(hikeFiles, filepath.Join(absDir, f.Name()))
		}
	}

	if len(hikeFiles) == 0 {
		return fmt.Errorf("no .go.hike files found in directory: %s", absDir)
	}

	if opts.Verbose {
		fmt.Printf("[hikec go] Found %d .go.hike file(s) in %s\n", len(hikeFiles), absDir)
		for _, f := range hikeFiles {
			fmt.Printf("  - %s\n", filepath.Base(f))
		}
	}

	// 2. ターゲット解決
	tgt, err := target.ParseTarget(opts.TargetName)
	if err != nil {
		return fmt.Errorf("target error: %w", err)
	}

	// 3. コンパイラインスタンスの準備
	comp := New(tgt)
	comp.SetVerbose(opts.Verbose)

	// 4. LLVM IR へのコンパイル
	llvmIR, _, prog, err := comp.CompileToLLVM(hikeFiles...)
	if err != nil {
		return fmt.Errorf("compilation error: %w", err)
	}

	dirName := filepath.Base(absDir)
	pkgName := detectPackageName(hikeFiles[0], dirName)

	// 1. Plan 9 アセンブリ（stub.s）の自動生成
	if err := GenerateGoStubs(absDir, pkgName, prog.Decls, opts.Verbose); err != nil {
		return fmt.Errorf("failed to generate stubs: %w", err)
	}

	// 2. Go コンパイラ用宣言ファイル（*.go）の自動生成
	for _, hf := range hikeFiles {
		goFilePath := strings.TrimSuffix(hf, ".hike") // fizzlib.go.hike -> fizzlib.go
		if err := GenerateGoDeclarations(goFilePath, pkgName, prog.Decls, opts.Verbose); err != nil {
			return fmt.Errorf("failed to generate go declarations: %w", err)
		}
	}
	llPath := filepath.Join(absDir, dirName+".ll")
	if err := os.WriteFile(llPath, []byte(llvmIR), 0644); err != nil {
		return fmt.Errorf("failed to write LLVM IR: %w", err)
	}

	// 5. 出力 .syso ファイル名の決定（Goリンカの自動検出規則に準拠）
	outputSyso := opts.OutputFile
	if outputSyso == "" {
		goos, goarch := resolveGoEnv(tgt)
		outputSyso = filepath.Join(absDir, fmt.Sprintf("%s_%s_%s.syso", dirName, goos, goarch))
	}

	// 6. Clang を呼び出して .syso（オブジェクトファイル）を出力
	clangArgs := []string{
		"-c",
		"-O2",                  // 未使用の internal 関数・未解決 C シンボル参照を完全除去
		"-mno-stack-arg-probe", // Windows 固有の ___chkstk_ms 生成を抑制
		llPath,
		"-o",
		outputSyso,
	}
	if tgt.Triple != "" {
		clangArgs = append(clangArgs, "--target="+tgt.Triple)
	}

	if opts.Verbose {
		fmt.Printf("[hikec go] Running: clang %s\n", strings.Join(clangArgs, " "))
	}

	cmd := exec.Command("clang", clangArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clang build failed: %w", err)
	}

	fmt.Printf("[hikec go] Successfully generated: %s\n", outputSyso)
	return nil
}

// detectPackageName はソースファイルの先頭からパッケージ宣言を取得します。
// 取得できない場合はディレクトリ名をフォールバックとして使用します。
func detectPackageName(filePath string, fallback string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fallback
	}
	l := lexer.New(string(content))
	p := parser.New(l)
	fileProg := p.ParseProgram()
	if fileProg.Package != "" && fileProg.Package != "main" {
		return fileProg.Package
	}
	return fallback
}

// resolveGoEnv は Target から Go の GOOS / GOARCH プレフィックスを解決します。
func resolveGoEnv(tgt *target.Target) (string, string) {
	if tgt == nil {
		return runtime.GOOS, runtime.GOARCH
	}
	if strings.Contains(tgt.Triple, "windows") {
		return "windows", "amd64"
	} else if strings.Contains(tgt.Triple, "linux") {
		return "linux", "amd64"
	} else if strings.Contains(tgt.Triple, "darwin") {
		return "darwin", "arm64"
	}
	return runtime.GOOS, runtime.GOARCH
}
