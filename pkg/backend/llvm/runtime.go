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

// RuntimeLLVMSymbols は runtime.ll 内で既に宣言・定義されているシンボル群
var RuntimeLLVMSymbols = map[string]bool{
	// 外部 C 標準アロケータ (declare)
	"malloc": true, "calloc": true, "free": true,

	// 純粋メモリ & 文字列操作内部実装 (define internal)
	"memcpy": true, "memcmp": true, "strlen": true, "strcmp": true,

	// OS ネイティブ API (declare)
	"QueueUserWorkItem": true, "CreateEventA": true, "SetEvent": true,
	"WaitForSingleObject": true, "CloseHandle": true, "Sleep": true,
	"GetTickCount64": true,

	// OS ネイティブバインディング実装 (define internal)
	"c_os_sleep_ms": true, "os_sleep_ms": true,
	"c_os_now_ns": true, "os_now_ns": true,

	// タスク & スレッドプールランタイム (define internal)
	"__hike_task_worker_thunk": true,
	"__hike_async":             true,
	"__hike_task_wait":         true,

	// チャネルランタイム (define internal)
	"__hike_chan_lock":   true,
	"__hike_chan_unlock": true,
	"__hike_chan_make":   true,
	"__hike_chan_send":   true,
	"__hike_chan_recv":   true,
	"__hike_chan_close":  true,

	// 文字列ランタイム (define internal)
	"hike_streq":          true,
	"hike_substr":         true,
	"hike_strcat":         true,
	"__hike_slice_to_str": true,

	// マップランタイム (define internal)
	"__hike_hash_str":   true,
	"__hike_map_key_eq": true,
	"__hike_map_create": true,
	"__hike_map_grow":   true,
	"__hike_map_set":    true,
	"__hike_map_get":    true,
	"__hike_map_delete": true,
	"__hike_map_len":    true,
}
