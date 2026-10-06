package llvm

import (
	"bytes"
	_ "embed"
	"strings"
	"text/template"
)

//go:replace text/template => ../../std/text/template

//go:embed runtime/runtime_common.ll
var builtinRuntimeCommonIR string

//go:embed runtime/runtime_windows.ll.tmpl
var builtinRuntimeWindowsTemplate string

//go:embed runtime/runtime_linux.ll.tmpl
var builtinRuntimeLinuxTemplate string

//go:embed runtime/runtime_wasm32.ll
var builtinRuntimeWasm32IR string

//go:embed runtime/runtime_common32.ll
var builtinRuntimeCommon32IR string

type RuntimeABI struct {
	PointerBits     int
	PointerType     string
	SizeType        string
	ThreadIDType    string
	PointerAlign    int
	LinuxMutexBytes int
	LinuxCondBytes  int
	LinuxEventBytes int
}

func runtimeABI(pointerBits int) RuntimeABI {
	if pointerBits == 32 {
		return RuntimeABI{
			PointerBits:     32,
			PointerType:     "i8*",
			SizeType:        "i32",
			ThreadIDType:    "i32",
			PointerAlign:    4,
			LinuxMutexBytes: 24,
			LinuxCondBytes:  48,
			LinuxEventBytes: 80,
		}
	}
	return RuntimeABI{
		PointerBits:     64,
		PointerType:     "i8*",
		SizeType:        "i64",
		ThreadIDType:    "i64",
		PointerAlign:    8,
		LinuxMutexBytes: 40,
		LinuxCondBytes:  48,
		LinuxEventBytes: 96,
	}
}

func renderRuntimeTemplate(source string, abi RuntimeABI) string {
	tmpl, err := template.New("runtime").Parse(source)
	if err != nil {
		panic(err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, abi); err != nil {
		panic(err)
	}
	return rendered.String()
}

// GetBuiltinRuntimeIR はデフォルト (Native 64-bit) のランタイム IR を返します
func GetBuiltinRuntimeIR() string {
	abi := runtimeABI(64)
	return builtinRuntimeCommonIR + "\n" + renderRuntimeTemplate(builtinRuntimeWindowsTemplate, abi)
}

// GetBuiltinRuntimeWasm32IR は wasm32 ターゲット向けのランタイム IR を返します
func GetBuiltinRuntimeWasm32IR() string {
	return builtinRuntimeWasm32IR
}

// GetBuiltinRuntimeCommon32IR returns the native 32-bit runtime. It uses the
// same 32-bit value ABI as the wasm32 runtime, but is selected independently
// so native 32-bit targets do not depend on the wasm target name.
func GetBuiltinRuntimeCommon32IR() string {
	return builtinRuntimeCommon32IR
}

// GetRuntimeIR はターゲットトリプルを判定し、適切なランタイム IR を返します
func GetRuntimeIR(targetTriple string, pointerBits ...int) string {
	t := strings.ToLower(targetTriple)
	if strings.Contains(t, "wasm32") || strings.Contains(t, "wasm") {
		return builtinRuntimeWasm32IR
	}

	processorIR := builtinRuntimeCommonIR
	if len(pointerBits) > 0 && pointerBits[0] == 32 {
		processorIR = builtinRuntimeCommon32IR
	}

	var osIR string
	if strings.Contains(t, "windows") || strings.Contains(t, "msvc") {
		osIR = renderRuntimeTemplate(builtinRuntimeWindowsTemplate, runtimeABI(pointerWidth(pointerBits)))
	} else {
		osIR = renderRuntimeTemplate(builtinRuntimeLinuxTemplate, runtimeABI(pointerWidth(pointerBits)))
	}
	return processorIR + "\n" + osIR
}

func pointerWidth(pointerBits []int) int {
	if len(pointerBits) > 0 && pointerBits[0] == 32 {
		return 32
	}
	return 64
}

// IsRuntimeSymbol は指定されたシンボル名がランタイム IR 内で定義・宣言済みであるかを判定します
func IsRuntimeSymbol(name string) bool {
	return RuntimeLLVMSymbols[name]
}

// RuntimeLLVMSymbols は各 runtime.ll 内で既に宣言・定義されているシンボル群
var RuntimeLLVMSymbols = map[string]bool{
	"__hike_panic_set": true, "__hike_panic_get": true, "__hike_panic_cause": true, "__hike_panic_site": true, "__hike_panic_is_active": true, "__hike_panic_fatal": true,
	"__hike_stderr_write": true,
	// 外部 C 標準アロケータ (declare)
	"malloc": true, "calloc": true, "free": true,
	"__hike_region_begin": true, "__hike_region_alloc": true, "__hike_region_end": true,
	"__hike_region_begin32": true, "__hike_region_alloc32": true, "__hike_region_end32": true,
	"__hike_area_begin": true, "__hike_area_alloc": true, "__hike_area_end": true,
	"__hike_area_begin32": true, "__hike_area_alloc32": true, "__hike_area_end32": true,
	"__hike_region_active_count": true, "__hike_region_begin_count": true, "__hike_region_end_count": true,
	"__hike_region_allocated_bytes": true, "__hike_region_released_bytes": true,

	// 純粋メモリ & 文字列操作内部実装 (64-bit) (define internal)
	"memcpy": true, "memcmp": true, "strlen": true, "strcmp": true,

	// 純粋メモリ & 文字列操作内部実装 (32-bit / wasm32) (define internal)
	"memcpy32": true, "memcmp32": true, "strlen32": true, "strcmp32": true,

	// --- Windows Native API (declare) ---
	"QueueUserWorkItem":   true,
	"CreateEventA":        true,
	"SetEvent":            true,
	"WaitForSingleObject": true,
	"CloseHandle":         true,
	"Sleep":               true,
	"GetTickCount64":      true,
	"GetStdHandle":        true,
	"WriteFile":           true,

	// --- Native POSIX output API ---
	"write": true,

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
	"hike_streq":           true,
	"hike_streq_len":       true,
	"hike_strcmp_len":      true,
	"__hike_string_retain": true, "__hike_string_release": true, "__hike_string_writable": true, "__hike_string_append": true,
	"__hike_sort_strings": true,
	"hike_substr":         true,
	"hike_strcat":         true,
	"hike_strcat_len":     true,
	"__hike_slice_to_str": true,
	"__hike_slice_alloc":  true, "__hike_slice_cap": true,
	"__hike_slice_retain": true, "__hike_slice_release": true,

	// 文字列ランタイム (32-bit / wasm32) (define internal)
	"hike_streq32":           true,
	"hike_streq_len32":       true,
	"hike_strcmp_len32":      true,
	"__hike_string_retain32": true, "__hike_string_release32": true, "__hike_string_writable32": true, "__hike_string_append32": true,
	"hike_substr32":         true,
	"hike_strcat32":         true,
	"hike_strcat_len32":     true,
	"__hike_slice_to_str32": true,
	"__hike_slice_alloc32":  true, "__hike_slice_cap32": true,
	"__hike_slice_retain32": true, "__hike_slice_release32": true,

	// マップランタイム (define internal)
	"__hike_hash_str":   true,
	"__hike_map_key_eq": true,
	"__hike_map_create": true,
	"__hike_map_grow":   true,
	"__hike_map_set":    true,
	"__hike_map_get":    true, "__hike_map_get_boxed": true, "__hike_map_get_boxed_ok": true,
	"__hike_map_set_str": true, "__hike_map_get_str": true, "__hike_map_get_boxed_str": true, "__hike_map_get_boxed_str_ok": true,
	"__hike_map_delete_str": true,
	"__hike_string_key":   true,
	"__hike_map_key_ptr":  true, "__hike_map_key_len": true,
	"__hike_string_start": true,
	"__hike_string_start32": true,
	"__hike_iface_typeid": true,
	"__hike_map_delete":   true,
	"__hike_map_len":      true,
	"__hike_cdict_create": true, "__hike_cdict_state": true, "__hike_cdict_set": true, "__hike_cdict_get": true,
	"__hike_cdict_set_str": true, "__hike_cdict_set_str_hash": true,
	"__hike_cdict_get_str": true, "__hike_cdict_get_boxed": true, "__hike_cdict_get_boxed_ok": true,
	"__hike_cdict_get_boxed_str": true, "__hike_cdict_get_boxed_str_ok": true,
	"__hike_cdict_delete": true, "__hike_cdict_delete_str": true, "__hike_cdict_delete_str_hash": true,
	"__hike_cdict_len": true,
}
