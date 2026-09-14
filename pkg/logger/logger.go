package logger

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	level     int
	levelInit sync.Once
)

func initLevel() {
	// コマンドライン引数からの自動検出
	for _, arg := range os.Args {
		if arg == "-vv" || arg == "--vv" || arg == "-v=2" || arg == "--verbose=2" {
			level = 2
			return
		}
	}
	for _, arg := range os.Args {
		if arg == "-v" || arg == "--verbose" || arg == "-v=1" || arg == "--verbose=1" {
			level = 1
			return
		}
	}

	// 環境変数からの自動検出
	v := strings.ToLower(os.Getenv("HIKEC_VERBOSE_LEVEL"))
	switch v {
	case "2", "vv", "-vv":
		level = 2
	case "1", "v", "-v":
		level = 1
	}
}

// SetLevel は明示的にログレベルを上書きする場合に使用する (0: 通常, 1: -v, 2: -vv)
func SetLevel(l int) {
	levelInit.Do(func() {}) // 自動検出をスキップ
	level = l
}

// GetLevel は現在のログレベルを取得する
func GetLevel() int {
	levelInit.Do(initLevel)
	return level
}

// IsVerbose は -v レベル以上かを判定する
func IsVerbose() bool {
	return GetLevel() >= 1
}

// IsVerbose2 は -vv レベル以上かを判定する
func IsVerbose2() bool {
	return GetLevel() >= 2
}

// LogVerbose は -v レベル以上の場合にのみフォーマット出力を行う
func LogVerbose(format string, a ...any) {
	if GetLevel() >= 1 {
		fmt.Printf(format, a...)
	}
}

// LogVerbose2 は -vv レベル以上の場合にのみフォーマット出力を行う
func LogVerbose2(format string, a ...any) {
	if GetLevel() >= 2 {
		fmt.Printf(format, a...)
	}
}
