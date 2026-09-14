package error_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runGoCompilerError(t *testing.T, source string, expected ...string) {
	t.Helper()
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(source), 0644); err != nil {
		t.Fatalf("Goソース書き込み失敗: %v", err)
	}

	binName := "main"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(tmpDir, binName), srcPath)
	cmd.Dir = tmpDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("Goコンパイルエラーが発生しませんでした\n[標準出力]: %s", stdout.String())
	}

	errorOutput := stderr.String()
	if strings.TrimSpace(errorOutput) == "" {
		t.Fatalf("Goコンパイルエラーが出力されていません\n[標準出力]: %s", stdout.String())
	}
	t.Logf("[Go compiler error]\n%s", errorOutput)
	for _, want := range expected {
		if !strings.Contains(errorOutput, want) {
			t.Errorf("期待したエラーが見つかりませんでした: %q\n[実際]: %s", want, errorOutput)
		}
	}
}

// Goソースとして未定義変数を参照し、Goコンパイラーの診断を確認する。
func TestGrammar_UndefinedIdentifier(t *testing.T) {
	runGoCompilerError(t, `
package main

func main() {
    return uninitializedValue
}
`, "main.go:5:12", "undefined: uninitializedValue")
}

// Goソースとして関数引数の不正な区切りを記述し、構文エラーを確認する。
func TestGrammar_InvalidParameterList(t *testing.T) {
	runGoCompilerError(t, `
package main

func main(, ) {
    return 0
}
`, "main.go:4:11", "syntax error")
}

// Goソースとして関数名がない宣言を記述し、構文エラーを確認する。
func TestGrammar_MissingFunctionName(t *testing.T) {
	runGoCompilerError(t, `
package main

func () {
    return 0
}
`, "main.go:4:9", "syntax error")
}
