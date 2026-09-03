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

var (
	hikecBin     string
	projectRoot  string
	testCaseBase string
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

// パッケージ全体の事前準備と後片付け
func TestMain(m *testing.M) {
	projectRoot = findProjectRoot()

	// カレントディレクトリ（テスト実行場所）に .test_case を作成
	wd, err := os.Getwd()
	if err != nil {
		wd = projectRoot
	}
	testCaseBase = filepath.Join(wd, ".test_case")
	_ = os.MkdirAll(testCaseBase, 0755)

	binName := "hikec"
	if runtime.GOOS == "windows" {
		binName = "hikec.exe"
	}
	hikecBin = filepath.Join(testCaseBase, binName)

	// テスト対象となる hikec 自体を 1 度だけビルド
	buildCmd := exec.Command("go", "build", "-o", hikecBin, filepath.Join(projectRoot, "cmd", "hikec"))
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "hikec のビルドに失敗しました: %v\n%s\n", err, string(out))
		os.Exit(1)
	}

	code := m.Run()

	// 全テスト完了後に .test_case をクリーンアップ
	_ = os.RemoveAll(testCaseBase)
	os.Exit(code)
}

type HikeTestCase struct {
	Source       string
	ExpectedOut  string
	ExpectedExit int
}

// 各テスト関数から呼び出される共通実行ヘルパー
func RunHikeCase(t *testing.T, tc HikeTestCase) {
	t.Helper()

	// .test_case 配下にランダムな一時ディレクトリを掘る（並列実行時の衝突防止）
	tmpDir, err := os.MkdirTemp(testCaseBase, "case-*")
	if err != nil {
		t.Fatalf("一時ディレクトリ作成失敗: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// main.hike 書き込み
	srcPath := filepath.Join(tmpDir, "main.hike")
	if err := os.WriteFile(srcPath, []byte(tc.Source), 0644); err != nil {
		t.Fatalf("ソース書き込み失敗: %v", err)
	}

	// hike.mod の自動解決
	stdDir := filepath.Join(projectRoot, "std")
	relStd, err := filepath.Rel(tmpDir, stdDir)
	if err != nil {
		relStd = stdDir
	}
	relStdSlash := filepath.ToSlash(relStd)

	var modBuilder strings.Builder
	modBuilder.WriteString("module test-runner\n\nhike 0.1.0\n\n")
	modBuilder.WriteString(fmt.Sprintf("replace std => %s\n", relStdSlash))

	if entries, err := os.ReadDir(stdDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				modBuilder.WriteString(fmt.Sprintf("replace std/%s => %s/%s\n", e.Name(), relStdSlash, e.Name()))
			}
		}
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "hike.mod"), []byte(modBuilder.String()), 0644); err != nil {
		t.Fatalf("hike.mod 書き込み失敗: %v", err)
	}

	// 実行
	runCmd := exec.Command(hikecBin, "run", srcPath)
	runCmd.Dir = tmpDir
	var stdout, stderr bytes.Buffer
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr

	err = runCmd.Run()

	actualExit := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			actualExit = exitErr.ExitCode()
		} else {
			t.Fatalf("実行失敗: %v\n[Stderr]: %s", err, stderr.String())
		}
	}

	if actualExit != tc.ExpectedExit {
		t.Errorf("終了コード不一致: 期待値 %d, 実際値 %d\n[Stderr]: %s", tc.ExpectedExit, actualExit, stderr.String())
	}

	normalize := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimRight(line, " \t\r")
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}

	actualOut := normalize(stdout.String())
	expectedOut := normalize(tc.ExpectedOut)
	if actualOut != expectedOut {
		t.Errorf("出力不一致:\n[期待値]:\n%s\n[実際値]:\n%s\n[Stderr]: %s", expectedOut, actualOut, stderr.String())
	}
}
