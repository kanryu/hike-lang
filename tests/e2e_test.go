package e2e_test

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

var hikecBin string

func TestMain(m *testing.M) {
	// テスト用の一時ディレクトリに hikec バイナリを一度だけビルド
	tmpDir, err := os.MkdirTemp("", "hikec-test-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "一時ディレクトリ作成失敗: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	binName := "hikec"
	if runtime.GOOS == "windows" {
		binName = "hikec.exe"
	}
	hikecBin = filepath.Join(tmpDir, binName)

	buildCmd := exec.Command("go", "build", "-o", hikecBin, "../cmd/hikec")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "hikec のビルドに失敗しました: %v\n%s\n", err, string(out))
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}

// テストケース定義
type TestCase struct {
	Name         string
	HikeSource   string // 検証するHikeソースコード
	ExpectedOut  string // 期待する標準出力
	ExpectedExit int    // 期待する終了コード
}

func runTryRunTest(t *testing.T, tc TestCase) {
	t.Helper()

	// 1. 一時作業ディレクトリの作成
	tmpDir, err := os.MkdirTemp("", "hike-test-*")
	if err != nil {
		t.Fatalf("一時ディレクトリ作成失敗: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "main.hike")
	if err := os.WriteFile(srcPath, []byte(tc.HikeSource), 0644); err != nil {
		t.Fatalf("ソース書き込み失敗: %v", err)
	}

	// 2. ビルド済み hikec を直接呼び出し、標準出力・標準エラー出力をキャプチャ
	runCmd := exec.Command(hikecBin, "run", srcPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr

	err = runCmd.Run()

	actualExit := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			actualExit = exitErr.ExitCode()
		} else {
			t.Fatalf("バイナリ実行失敗: %v\n[Stderr]: %s", err, stderr.String())
		}
	}

	// 3. アサーション（終了コード検証）
	if actualExit != tc.ExpectedExit {
		t.Errorf("[%s] 終了コード不一致: 期待値 %d, 実際値 %d\n[Stderr]: %s", tc.Name, tc.ExpectedExit, actualExit, stderr.String())
	}

	// 4. アサーション（標準出力検証）
	actualOut := strings.TrimSpace(stdout.String())
	expectedOut := strings.TrimSpace(tc.ExpectedOut)
	if actualOut != expectedOut {
		t.Errorf("[%s] 出力不一致:\n期待値:\n%s\n実際値:\n%s", tc.Name, expectedOut, actualOut)
	}
}

func TestHikeFeatures(t *testing.T) {
	tests := []TestCase{
		{
			Name: "2-Pass Iterator Check",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    arr := [3]int{10, 20, 30}
    sum := 0
    for _, v := range arr {
        sum = sum + v
    }
    printf("SUM=%d\n", sum)
    return 0
}
`,
			ExpectedOut:  "SUM=60",
			ExpectedExit: 0,
		},
		{
			Name: "Exit Code Check",
			HikeSource: `
package main

func main() int {
    return 42
}
`,
			ExpectedOut:  "",
			ExpectedExit: 42,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			runTryRunTest(t, tc)
		})
	}
}
