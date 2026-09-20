// Command wasm-checker loads a WebAssembly module and calls one exported function.
//
// Usage:
//
//	wasm-checker <module.wasm> <function>
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"

	wasmtime "github.com/bytecodealliance/wasmtime-go/v48"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wasm-checker <module.wasm> <function> [--string|--string-info]")
}

func main() {
	if len(os.Args) < 3 || len(os.Args) > 4 {
		usage()
		os.Exit(2)
	}
	stringResult := len(os.Args) == 4 && (os.Args[3] == "--string" || os.Args[3] == "--string-info")
	stringInfo := len(os.Args) == 4 && os.Args[3] == "--string-info"
	if len(os.Args) == 4 && !stringResult {
		usage()
		os.Exit(2)
	}

	wasm, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: read %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	engine := wasmtime.NewEngine()
	defer engine.Close()
	module, err := wasmtime.NewModule(engine, wasm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: compile module: %v\n", err)
		os.Exit(1)
	}
	defer module.Close()

	store := wasmtime.NewStore(engine)
	defer store.Close()
	instance, err := wasmtime.NewInstance(store, module, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm-checker: instantiate module: %v\n", err)
		os.Exit(1)
	}
	// String-returning test functions are invoked directly, but Hike global
	// initializers are emitted in main. Run that entry point first so the
	// function observes the same initialized program state as a normal run.
	if stringResult && os.Args[2] != "main" {
		if initFunc := instance.GetFunc(store, "main"); initFunc != nil {
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
		fmt.Fprintf(os.Stderr, "wasm-checker: call %q: %v\n", os.Args[2], err)
		os.Exit(1)
	}
	if stringResult {
		memoryExtern := instance.GetExport(store, "memory")
		if memoryExtern == nil || memoryExtern.Memory() == nil {
			fmt.Fprintln(os.Stderr, "wasm-checker: exported memory not found")
			os.Exit(1)
		}
		info, err := readHikeStringInfo(memoryExtern.Memory(), store, result)
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

type hikeStringInfo struct {
	offset uint32
	length uint32
	text   string
}

func readHikeStringInfo(memory *wasmtime.Memory, store wasmtime.Storelike, result interface{}) (hikeStringInfo, error) {
	var object uint32
	switch value := result.(type) {
	case int32:
		object = uint32(value)
	case int64:
		object = uint32(value)
	default:
		return hikeStringInfo{}, fmt.Errorf("string result must be an integer pointer, got %T", result)
	}
	data := memory.UnsafeData(store)
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
