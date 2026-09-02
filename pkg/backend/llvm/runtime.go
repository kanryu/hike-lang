package llvm

import (
	_ "embed" // embedパッケージをブランクインポート（ディレクティブ有効化のため）
)

// runtime/runtime.ll の内容をコンパイル時に文字列として埋め込む
//
//go:embed runtime/runtime.ll
var builtinRuntimeIR string

// 必要に応じて外部から取得できるように公開関数を用意するか、
// 同一パッケージ内の emitter.go から builtinRuntimeIR を直接参照します
func GetBuiltinRuntimeIR() string {
	return builtinRuntimeIR
}
