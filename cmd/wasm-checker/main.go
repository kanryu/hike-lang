// Command wasm-checker loads a WebAssembly module and calls one exported function.
//
// Usage:
//
//	wasm-checker <module.wasm> <function> [options]
package main

/*
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>
typedef struct wasm_config_t wasm_config_t;
typedef struct wasmtime_sharedmemory wasmtime_sharedmemory_t;
typedef struct wasm_engine_t wasm_engine_t;
typedef struct wasmtime_linker_t wasmtime_linker_t;
typedef struct wasmtime_context_t wasmtime_context_t;
typedef struct wasm_memorytype_t wasm_memorytype_t;
typedef struct wasmtime_error_t wasmtime_error_t;
typedef struct {
	uint8_t kind;
	union { void *sharedmemory; } of;
} hike_extern_t;
extern void wasmtime_config_shared_memory_set(wasm_config_t *, bool);
extern wasmtime_error_t *wasmtime_memorytype_new(uint64_t, bool, uint64_t, bool, bool, uint8_t, wasm_memorytype_t **);
extern wasmtime_error_t *wasmtime_sharedmemory_new(const wasm_engine_t *, const wasm_memorytype_t *, wasmtime_sharedmemory_t **);
extern wasmtime_error_t *wasmtime_linker_define(wasmtime_linker_t *, wasmtime_context_t *, const char *, size_t, const char *, size_t, const hike_extern_t *);
extern void wasm_memorytype_delete(wasm_memorytype_t *);
extern void *wasmtime_sharedmemory_data(const wasmtime_sharedmemory_t *);
extern size_t wasmtime_sharedmemory_data_size(const wasmtime_sharedmemory_t *);
static void hike_enable_shared_memory(void *config) {
	wasmtime_config_shared_memory_set((wasm_config_t *)config, true);
}
static wasmtime_sharedmemory_t *hike_shared_memory;
static int hike_define_shared_memory(void *engine, void *linker, void *context) {
	wasm_memorytype_t *type = NULL;
	if (hike_shared_memory == NULL) {
		if (wasmtime_memorytype_new(16, true, 16, false, true, 16, &type) != NULL) return 0;
		if (wasmtime_sharedmemory_new((const wasm_engine_t *)engine, type, &hike_shared_memory) != NULL) return 0;
		wasm_memorytype_delete(type);
	}
	hike_extern_t item;
	memset(&item, 0, sizeof(item));
	item.kind = 4;
	item.of.sharedmemory = hike_shared_memory;
	if (wasmtime_linker_define((wasmtime_linker_t *)linker, (wasmtime_context_t *)context, "env", 3, "memory", 6, &item) != NULL) return 0;
	return 1;
}
static int hike_shared_memory_bytes(void *raw_extern, void **data, size_t *size) {
	uint8_t kind = *(const uint8_t *)raw_extern;
	void *shared = NULL;
	if (kind != 4) return 0;
	memcpy(&shared, (const uint8_t *)raw_extern + 8, sizeof(shared));
	if (shared == NULL) return 0;
	*data = wasmtime_sharedmemory_data((const wasmtime_sharedmemory_t *)shared);
	*size = wasmtime_sharedmemory_data_size((const wasmtime_sharedmemory_t *)shared);
	return *data != NULL;
}
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	wasmtime "github.com/bytecodealliance/wasmtime-go/v48"
)

const (
	concurrentArenaBase  = 512 * 1024
	concurrentArenaSize  = 256 * 1024
	concurrentHeapOffset = 128 * 1024
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wasm-checker <module.wasm> <function> [--mode=normal|concurrent] [--workers=N] [--source=path] [--wat-output=path] [--string|--string-info] [--dump-memory=path]")
}

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}
	mode := "normal"
	stringResult := false
	stringInfo := false
	dumpPath := ""
	sourcePath := ""
	watOutputPath := ""
	workerCount := 1
	for _, arg := range os.Args[3:] {
		switch {
		case arg == "--string":
			stringResult = true
		case arg == "--string-info":
			stringResult = true
			stringInfo = true
		case strings.HasPrefix(arg, "--mode="):
			mode = strings.TrimPrefix(arg, "--mode=")
		case strings.HasPrefix(arg, "--dump-memory="):
			dumpPath = strings.TrimPrefix(arg, "--dump-memory=")
		case strings.HasPrefix(arg, "--source="):
			sourcePath = strings.TrimPrefix(arg, "--source=")
		case strings.HasPrefix(arg, "--workers="):
			parsed, parseErr := strconv.Atoi(strings.TrimPrefix(arg, "--workers="))
			if parseErr != nil || parsed < 1 {
				fmt.Fprintf(os.Stderr, "wasm-checker: invalid worker count %q\n", arg)
				os.Exit(2)
			}
			workerCount = parsed
		case strings.HasPrefix(arg, "--wat-output="):
			watOutputPath = strings.TrimPrefix(arg, "--wat-output=")
		default:
			usage()
			os.Exit(2)
		}
	}
	if mode != "normal" && mode != "concurrent" {
		fmt.Fprintf(os.Stderr, "wasm-checker: invalid mode %q\n", mode)
		os.Exit(2)
	}
	if mode != "concurrent" && workerCount != 1 {
		fmt.Fprintln(os.Stderr, "wasm-checker: --workers requires --mode=concurrent")
		os.Exit(2)
	}
	var sourceText string
	if sourcePath != "" {
		if sourcePath == "-" {
			data, readErr := io.ReadAll(os.Stdin)
			if readErr != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker: read source from stdin: %v\n", readErr)
				os.Exit(1)
			}
			sourceText = string(data)
		} else {
			data, readErr := os.ReadFile(sourcePath)
			if readErr != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker: read source %s: %v\n", sourcePath, readErr)
				os.Exit(1)
			}
			sourceText = string(data)
		}
	}

	wasm, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: read %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	config := wasmtime.NewConfig()
	config.SetWasmThreads(true)
	// wasmtime-go/v48 exposes wasm threads but not the separate shared-memory
	// switch. Pass the wrapper's C config pointer to the corresponding Wasmtime
	// API until the Go binding exposes this knob.
	configValue := reflect.ValueOf(config).Elem().FieldByName("_ptr")
	C.hike_enable_shared_memory(unsafe.Pointer(configValue.Pointer()))
	engine := wasmtime.NewEngineWithConfig(config)
	defer engine.Close()
	module, err := wasmtime.NewModule(engine, wasm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: compile module: %v\n", err)
		os.Exit(1)
	}
	defer module.Close()

	store := wasmtime.NewStore(engine)
	defer store.Close()
	linker := wasmtime.NewLinker(engine)
	enginePtr := reflect.ValueOf(engine).Elem().FieldByName("_ptr").Pointer()
	linkerPtr := reflect.ValueOf(linker).Elem().FieldByName("_ptr").Pointer()
	contextPtr := reflect.ValueOf(store.Context()).Pointer()
	if mode == "concurrent" && C.hike_define_shared_memory(unsafe.Pointer(enginePtr), unsafe.Pointer(linkerPtr), unsafe.Pointer(contextPtr)) == 0 {
		fmt.Fprintln(os.Stderr, "wasm-checker: failed to create shared memory")
		os.Exit(1)
	}
	type pendingTask struct {
		fn   int32
		env  int32
		task int32
		sig  int32
	}
	var pending []pendingTask
	var pendingMu sync.Mutex
	type workerRuntime struct {
		store    *wasmtime.Store
		instance *wasmtime.Instance
	}
	var workers []workerRuntime
	var instance *wasmtime.Instance
	var generatedWAT string
	const sourceObjectBase = 8 * 1024 * 1024
	writeSource := func(caller *wasmtime.Caller) int32 {
		memoryExtern := caller.GetExport("memory")
		if memoryExtern == nil || memoryExtern.Memory() == nil {
			return 0
		}
		memory := memoryExtern.Memory()
		required := sourceObjectBase + 12 + len(sourceText) + 1
		data := memory.UnsafeData(caller)
		if len(data) < required {
			pages := uint64((required - len(data) + 65535) / 65536)
			if _, err := memory.Grow(caller, pages); err != nil {
				return 0
			}
			data = memory.UnsafeData(caller)
		}
		payload := sourceObjectBase + 12
		binary.LittleEndian.PutUint32(data[sourceObjectBase:], uint32(payload))
		binary.LittleEndian.PutUint32(data[sourceObjectBase+4:], 0)
		binary.LittleEndian.PutUint32(data[sourceObjectBase+8:], uint32(len(sourceText)))
		copy(data[payload:], []byte(sourceText))
		data[payload+len(sourceText)] = 0
		return int32(sourceObjectBase)
	}
	readCallerString := func(caller *wasmtime.Caller, ptr int32) string {
		memoryExtern := caller.GetExport("memory")
		if memoryExtern == nil || memoryExtern.Memory() == nil {
			return ""
		}
		info, readErr := readHikeStringInfo(memoryExtern.Memory(), caller, ptr)
		if readErr != nil {
			return ""
		}
		return info.text
	}
	if err := linker.DefineFunc(store, "env", "__hike_js_wasm_hikec_source", writeSource); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: define Hike source bridge: %v\n", err)
		os.Exit(1)
	}
	if err := linker.DefineFunc(store, "env", "__hike_js_wasm_hikec_runtime_init", func(_ *wasmtime.Caller) int32 { return 1 }); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: define Hike runtime init bridge: %v\n", err)
		os.Exit(1)
	}
	if err := linker.DefineFunc(store, "env", "__hike_js_wasm_hikec_echo_source_enabled", func(_ *wasmtime.Caller) int32 { return 0 }); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: define Hike echo bridge: %v\n", err)
		os.Exit(1)
	}
	if err := linker.DefineFunc(store, "env", "__hike_js_wasm_hikec_debug_phase", func(_ *wasmtime.Caller, phase int32) int32 { return phase }); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: define Hike debug bridge: %v\n", err)
		os.Exit(1)
	}
	if err := linker.DefineFunc(store, "env", "printf", func(_ *wasmtime.Caller, _ int32) int32 { return 0 }); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: define printf bridge: %v\n", err)
		os.Exit(1)
	}
	if err := linker.DefineFunc(store, "env", "__hike_js_wasm_hikec_publish_wat", func(caller *wasmtime.Caller, watPtr int32) int32 {
		generatedWAT = readCallerString(caller, watPtr)
		return int32(len(generatedWAT))
	}); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: define Hike WAT bridge: %v\n", err)
		os.Exit(1)
	}
	// LLVM's wasm32 runtime notifies the host through hike_thread_spawn when
	// Async creates a task. WABT uses the same ABI; the dispatcher remains in
	// Wasm so the host only forwards the table index and task metadata.
	if err := linker.DefineFunc(store, "env", "hike_thread_spawn", func(caller *wasmtime.Caller, fn, env, task, sig int32) {
		pendingMu.Lock()
		pending = append(pending, pendingTask{fn: fn, env: env, task: task, sig: sig})
		pendingMu.Unlock()
	}); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: define worker runtime: %v\n", err)
		os.Exit(1)
	}
	if err := linker.DefineFunc(store, "env", "hike_thread_pump", func(caller *wasmtime.Caller) {
		pendingMu.Lock()
		tasks := append([]pendingTask(nil), pending...)
		pending = nil
		pendingMu.Unlock()
		if mode == "concurrent" && instance != nil && len(workers) > 0 {
			for _, name := range []string{"eventloop_queue", "eventloop_running"} {
				mainGlobal := instance.GetExport(store, "__hike_global_"+name)
				for _, worker := range workers {
					workerGlobal := worker.instance.GetExport(worker.store, "__hike_global_"+name)
					if mainGlobal != nil && workerGlobal != nil && mainGlobal.Global() != nil && workerGlobal.Global() != nil {
						if err := workerGlobal.Global().Set(worker.store, mainGlobal.Global().Get(store)); err != nil {
							fmt.Fprintf(os.Stderr, "wasm-checker: synchronize worker global %s: %v\n", name, err)
						}
					}
				}
			}
		}
		for index, task := range tasks {
			worker := workers[index%len(workers)]
			if running := worker.instance.GetExport(worker.store, "__hike_global_eventloop_running"); running != nil && running.Global() != nil {
				_ = running.Global().Set(worker.store, wasmtime.ValI32(1))
			}
			dispatch := worker.instance.GetFunc(worker.store, "__hike_worker_dispatch")
			if dispatch == nil {
				fmt.Fprintln(os.Stderr, "wasm-checker: worker dispatcher is missing")
				continue
			}
			if _, callErr := dispatch.Call(worker.store, task.fn, task.env, task.task, task.sig); callErr != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker: worker failed: %v\n", callErr)
			}
		}
	}); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: define worker pump: %v\n", err)
		os.Exit(1)
	}
	if mode == "concurrent" {
		for workerID := 0; workerID < workerCount; workerID++ {
			workerStore := wasmtime.NewStore(engine)
			workerLinker := wasmtime.NewLinker(engine)
			workerLinkerPtr := reflect.ValueOf(workerLinker).Elem().FieldByName("_ptr").Pointer()
			if C.hike_define_shared_memory(unsafe.Pointer(enginePtr), unsafe.Pointer(workerLinkerPtr), unsafe.Pointer(reflect.ValueOf(workerStore.Context()).Pointer())) == 0 {
				fmt.Fprintln(os.Stderr, "wasm-checker: failed to connect worker shared memory")
				os.Exit(1)
			}
			if err := workerLinker.DefineFunc(workerStore, "env", "hike_thread_spawn", func(_ *wasmtime.Caller, fn, env, task, sig int32) {
				pendingMu.Lock()
				pending = append(pending, pendingTask{fn: fn, env: env, task: task, sig: sig})
				pendingMu.Unlock()
			}); err != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker: define worker spawn: %v\n", err)
				os.Exit(1)
			}
			if err := workerLinker.DefineFunc(workerStore, "env", "hike_thread_pump", func(_ *wasmtime.Caller) {}); err != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker: define worker pump: %v\n", err)
				os.Exit(1)
			}
			workerInstance, workerErr := workerLinker.Instantiate(workerStore, module)
			if workerErr != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker: instantiate worker: %v\n", workerErr)
				os.Exit(1)
			}
			if workerMain := workerInstance.GetFunc(workerStore, "main"); workerMain != nil {
				if _, err := workerMain.Call(workerStore, int32(0), int32(0)); err != nil {
					fmt.Fprintf(os.Stderr, "wasm-checker: initialize worker globals: %v\n", err)
					os.Exit(1)
				}
			}
			workerBase := concurrentArenaBase + workerID*concurrentArenaSize
			if workerStack := workerInstance.GetExport(workerStore, "__hike_sp"); workerStack != nil && workerStack.Global() != nil {
				if err := workerStack.Global().Set(workerStore, wasmtime.ValI32(int32(workerBase))); err != nil {
					fmt.Fprintf(os.Stderr, "wasm-checker: relocate worker stack: %v\n", err)
					os.Exit(1)
				}
			}
			if workerHeap := workerInstance.GetExport(workerStore, "__hike_heap"); workerHeap != nil && workerHeap.Global() != nil {
				if err := workerHeap.Global().Set(workerStore, wasmtime.ValI32(int32(workerBase+concurrentHeapOffset))); err != nil {
					fmt.Fprintf(os.Stderr, "wasm-checker: relocate worker heap: %v\n", err)
					os.Exit(1)
				}
			}
			workers = append(workers, workerRuntime{store: workerStore, instance: workerInstance})
			workerLinker.Close()
		}
		defer func() {
			for _, worker := range workers {
				worker.store.Close()
			}
		}()
	}
	instance, err = linker.Instantiate(store, module)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: instantiate module: %v\n", err)
		os.Exit(1)
	}
	// String-returning test functions are invoked directly, but Hike global
	// initializers are emitted in main. Run that entry point first so the
	// function observes the same initialized program state as a normal run.
	if stringResult && os.Args[2] != "main" {
		initName := "main"
		if mode == "concurrent" {
			if bootstrap := instance.GetFunc(store, "__hike_bootstrap"); bootstrap != nil {
				initName = "__hike_bootstrap"
			}
		}
		if initFunc := instance.GetFunc(store, initName); initFunc != nil {
			initParams := initFunc.Type(store).Params()
			switch {
			case len(initParams) == 0:
				if _, err := initFunc.Call(store); err != nil {
					fmt.Fprintf(os.Stderr, "wasm-checker: initialize globals: %v\n", err)
					os.Exit(1)
				}
			case len(initParams) == 2 &&
				initParams[0].Kind() == wasmtime.KindI32 &&
				initParams[1].Kind() == wasmtime.KindI32:
				if _, err := initFunc.Call(store, int32(0), int32(0)); err != nil {
					fmt.Fprintf(os.Stderr, "wasm-checker: initialize globals: %v\n", err)
					os.Exit(1)
				}
			}
			if stackExtern := instance.GetExport(store, "__hike_sp"); stackExtern != nil && stackExtern.Global() != nil {
				if err := stackExtern.Global().Set(store, wasmtime.ValI32(131072)); err != nil {
					fmt.Fprintf(os.Stderr, "wasm-checker: relocate stack: %v\n", err)
					os.Exit(1)
				}
			}
		}
	}
	if mode == "concurrent" && len(workers) > 0 && stringResult {
		for _, name := range []string{"eventloop_queue", "eventloop_running", "testOutputBuffer", "mainDeviceID"} {
			mainGlobal := instance.GetExport(store, "__hike_global_"+name)
			for _, worker := range workers {
				workerGlobal := worker.instance.GetExport(worker.store, "__hike_global_"+name)
				if mainGlobal != nil && workerGlobal != nil && mainGlobal.Global() != nil && workerGlobal.Global() != nil {
					if err := workerGlobal.Global().Set(worker.store, mainGlobal.Global().Get(store)); err != nil {
						fmt.Fprintf(os.Stderr, "wasm-checker: synchronize worker global %s: %v\n", name, err)
						os.Exit(1)
					}
				}
			}
		}
	}

	function := instance.GetFunc(store, os.Args[2])
	if function == nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: exported function %q not found\n", os.Args[2])
		os.Exit(1)
	}
	params := function.Type(store).Params()
	var result interface{}
	if len(params) == 0 {
		result, err = function.Call(store)
	} else if os.Args[2] == "main" && len(params) == 2 &&
		params[0].Kind() == wasmtime.KindI32 && params[1].Kind() == wasmtime.KindI32 {
		// Hike's WABT entry point uses the conventional argc/argv ABI even
		// when the source-level main function has no parameters.
		result, err = function.Call(store, int32(0), int32(0))
	} else {
		fmt.Fprintf(os.Stderr, "wasm-checker: function %q requires unsupported arguments\n", os.Args[2])
		os.Exit(1)
	}
	if err != nil {
		if dumpPath != "" {
			dumpMemory(instance.GetExport(store, "memory"), store, dumpPath)
		}
		fmt.Fprintf(os.Stderr, "wasm-checker: call %q: %v\n", os.Args[2], err)
		os.Exit(1)
	}
	if sourcePath != "" {
		if generatedWAT == "" {
			fmt.Fprintln(os.Stderr, "wasm-checker: Hike compiler did not publish WAT")
			os.Exit(1)
		}
		if watOutputPath != "" {
			if writeErr := os.WriteFile(watOutputPath, []byte(generatedWAT), 0644); writeErr != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker: write WAT %s: %v\n", watOutputPath, writeErr)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "wasm-checker: wrote WAT %s (%d bytes)\n", watOutputPath, len(generatedWAT))
		} else {
			fmt.Print(generatedWAT)
		}
		return
	}
	if stringResult {
		memoryExtern := instance.GetExport(store, "memory")
		if dumpPath != "" {
			dumpMemory(memoryExtern, store, dumpPath)
		}
		if memoryExtern == nil {
			fmt.Fprintln(os.Stderr, "wasm-checker: exported memory not found")
			os.Exit(1)
		}
		var info hikeStringInfo
		if memory := memoryExtern.Memory(); memory != nil {
			info, err = readHikeStringInfo(memory, store, result)
		} else {
			var ptr unsafe.Pointer
			var size C.size_t
			raw := reflect.ValueOf(memoryExtern).Elem().FieldByName("_ptr").Pointer()
			if C.hike_shared_memory_bytes(unsafe.Pointer(raw), &ptr, &size) == 0 {
				fmt.Fprintln(os.Stderr, "wasm-checker: unsupported shared memory export")
				os.Exit(1)
			}
			info, err = readHikeStringInfoBytes(unsafe.Slice((*byte)(ptr), int(size)), result)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "wasm-checker: read string result: %v\n", err)
			os.Exit(1)
		}
		if stringInfo {
			// CSV-like, machine-readable form: payload offset, byte length,
			// and a Go-quoted string so embedded newlines are unambiguous.
			fmt.Printf("%d,%d,%s\n", info.offset, info.length, strconv.Quote(info.text))
		} else {
			fmt.Print(info.text)
			if !strings.HasSuffix(info.text, "\n") {
				fmt.Println()
			}
		}
		return
	}
	fmt.Println(formatResult(result))
}

func dumpMemory(extern *wasmtime.Extern, store wasmtime.Storelike, path string) {
	if extern == nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: dump memory: export not found\n")
		return
	}
	var data []byte
	if memory := extern.Memory(); memory != nil {
		data = memory.UnsafeData(store)
	} else {
		var ptr unsafe.Pointer
		var size C.size_t
		raw := reflect.ValueOf(extern).Elem().FieldByName("_ptr").Pointer()
		if C.hike_shared_memory_bytes(unsafe.Pointer(raw), &ptr, &size) == 0 {
			fmt.Fprintf(os.Stderr, "wasm-checker: dump memory: unsupported memory export\n")
			return
		}
		data = unsafe.Slice((*byte)(ptr), int(size))
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: dump memory: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "wasm-checker: dumped %d bytes to %s\n", len(data), path)
}

type hikeStringInfo struct {
	offset uint32
	length uint32
	text   string
}

func readHikeStringInfo(memory *wasmtime.Memory, store wasmtime.Storelike, result interface{}) (hikeStringInfo, error) {
	return readHikeStringInfoBytes(memory.UnsafeData(store), result)
}

func readHikeStringInfoBytes(data []byte, result interface{}) (hikeStringInfo, error) {
	var object uint32
	switch value := result.(type) {
	case int32:
		object = uint32(value)
	case int64:
		object = uint32(value)
	default:
		return hikeStringInfo{}, fmt.Errorf("string result must be an integer pointer, got %T", result)
	}
	if uint64(object)+12 > uint64(len(data)) {
		return hikeStringInfo{}, fmt.Errorf("string object %d is outside linear memory", object)
	}
	base := binary.LittleEndian.Uint32(data[object:])
	offset := binary.LittleEndian.Uint32(data[object+4:])
	length := binary.LittleEndian.Uint32(data[object+8:])
	start := uint64(base) + uint64(offset)
	end := start + uint64(length)
	if end > uint64(len(data)) {
		return hikeStringInfo{}, fmt.Errorf("string payload [%d:%d] is outside linear memory", start, end)
	}
	return hikeStringInfo{offset: uint32(start), length: length, text: string(data[start:end])}, nil
}

func readHikeString(memory *wasmtime.Memory, store wasmtime.Storelike, result interface{}) (string, error) {
	info, err := readHikeStringInfo(memory, store, result)
	return info.text, err
}

func formatResult(result interface{}) interface{} {
	values, ok := result.([]wasmtime.Val)
	if !ok {
		return result
	}
	formatted := make([]interface{}, len(values))
	for i, value := range values {
		formatted[i] = value.Get()
	}
	return formatted
}
