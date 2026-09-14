package error_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var (
	hikecBin    string
	projectRoot string
	testCaseDir string
)

func findProjectRoot() string {
	dir, err := os.Getwd()
	if err == nil {
		for {
			if _, err := os.Stat(filepath.Join(dir, "std")); err == nil {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	abs, _ := filepath.Abs("..")
	return abs
}

func TestMain(m *testing.M) {
	projectRoot = findProjectRoot()

	wd, err := os.Getwd()
	if err != nil {
		wd = projectRoot
	}
	testCaseDir = filepath.Join(wd, ".test_case")
	_ = os.MkdirAll(testCaseDir, 0755)

	binName := "hikec"
	if runtime.GOOS == "windows" {
		binName = "hikec.exe"
	}
	hikecBin = filepath.Join(testCaseDir, binName)

	buildCmd := exec.Command("go", "build", "-o", hikecBin, filepath.Join(projectRoot, "cmd", "hikec"))
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "hikec のビルドに失敗しました: %v\n%s\n", err, string(out))
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(testCaseDir)
	os.Exit(code)
}

// HikeCompileErrorCase は、コンパイルエラーの発生と内容を検証するケースです。
type HikeCompileErrorCase struct {
	Source             string
	ExpectedError      string
	ExpectedErrorLines []string
}

// RunHikeCompileErrorCase は、指定されたHikeソースがコンパイルに失敗し、
// ユーザーに必要なエラー情報を出力することを検証します。
func RunHikeCompileErrorCase(t *testing.T, tc HikeCompileErrorCase) {
	t.Helper()

	tmpDir, err := os.MkdirTemp(testCaseDir, "case-*")
	if err != nil {
		t.Fatalf("一時ディレクトリ作成失敗: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "main.hike")
	if err := os.WriteFile(srcPath, []byte(tc.Source), 0644); err != nil {
		t.Fatalf("ソース書き込み失敗: %v", err)
	}

	cmd := exec.Command(hikecBin, "emit-ir", srcPath)
	cmd.Dir = tmpDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		t.Fatalf("コンパイルエラーが発生しませんでした\n[標準出力]: %s", stdout.String())
	}

	errorOutput := stderr.String()
	if strings.TrimSpace(errorOutput) == "" {
		t.Fatalf("コンパイルエラーが標準エラー出力に出力されていません\n[標準出力]: %s", stdout.String())
	}

	if tc.ExpectedError != "" && !strings.Contains(errorOutput, tc.ExpectedError) {
		t.Errorf("期待したエラーメッセージが見つかりませんでした\n[期待値]: %s\n[実際]: %s", tc.ExpectedError, errorOutput)
	}
	for _, expectedLine := range tc.ExpectedErrorLines {
		if !strings.Contains(errorOutput, expectedLine) {
			t.Errorf("期待したエラー情報が見つかりませんでした\n[期待値]: %s\n[実際]: %s", expectedLine, errorOutput)
		}
	}
}
