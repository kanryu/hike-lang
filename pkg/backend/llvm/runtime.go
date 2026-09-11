package llvm

import (
	_ "embed"
	"strings"
)

// runtime/runtime.ll (Native 64-bit / Windows) の内容をコンパイル時に埋め込む
//
//go:embed runtime/runtime.ll
var builtinRuntimeIR string

// runtime/runtime_wasm32.ll (WASM32 / POSIX 互換) の内容をコンパイル時に埋め込む
//
//go:embed runtime/runtime_wasm32.ll
var builtinRuntimeWasm32IR string

// GetBuiltinRuntimeIR はデフォルト (Native 64-bit) のランタイム IR を返します
func GetBuiltinRuntimeIR() string {
	return builtinRuntimeIR
}

// GetBuiltinRuntimeWasm32IR は wasm32 ターゲット向けのランタイム IR を返します
func GetBuiltinRuntimeWasm32IR() string {
	return builtinRuntimeWasm32IR
}

// GetRuntimeIR はターゲットトリプルを判定し、適切なランタイム IR を返します
func GetRuntimeIR(targetTriple string) string {
	if strings.HasPrefix(targetTriple, "wasm32") {
		return builtinRuntimeWasm32IR
	}
	return builtinRuntimeIR
}

// RuntimeLLVMSymbols は各 runtime.ll 内で既に宣言・定義されているシンボル群
var RuntimeLLVMSymbols = map[string]bool{
	// 外部 C 標準アロケータ (declare)
	"malloc": true, "calloc": true, "free": true,

	// 純粋メモリ & 文字列操作内部実装 (64-bit) (define internal)
	"memcpy": true, "memcmp": true, "strlen": true, "strcmp": true,

	// 純粋メモリ & 文字列操作内部実装 (32-bit / wasm32) (define internal)
	"memcpy32": true, "memcmp32": true, "strlen32": true, "strcmp32": true,

	// --- Windows Native API (declare) ---
	"QueueUserWorkItem": true, "CreateEventA": true, "SetEvent": true,
	"WaitForSingleObject": true, "CloseHandle": true, "Sleep": true,
	"GetTickCount64": true,

	// --- POSIX / WASM32 抽象スレッド同期 API (declare) ---
	"hike_thread_spawn":  true,
	"hike_event_create":  true,
	"hike_event_signal":  true,
	"hike_event_wait":    true,
	"hike_event_destroy": true,
	"hike_sleep_ms":      true,
	"hike_now_ns":        true,

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

	// 文字列ランタイム (64-bit) (define internal)
	"hike_streq":          true,
	"hike_substr":         true,
	"hike_strcat":         true,
	"__hike_slice_to_str": true,

	// 文字列ランタイム (32-bit / wasm32) (define internal)
	"hike_streq32":          true,
	"hike_substr32":         true,
	"hike_strcat32":         true,
	"__hike_slice_to_str32": true,

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
