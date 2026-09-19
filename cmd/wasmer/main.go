// Command wasmer loads a WebAssembly module and calls one exported function.
//
// Usage:
//
//	wasmer <module.wasm> <function>
package main

import (
	"fmt"
	"os"

	wasmtime "github.com/bytecodealliance/wasmtime-go/v48"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wasmer <module.wasm> <function>")
}

func main() {
	if len(os.Args) != 3 {
		usage()
		os.Exit(2)
	}

	wasm, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasmer: read %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	engine := wasmtime.NewEngine()
	defer engine.Close()
	module, err := wasmtime.NewModule(engine, wasm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasmer: compile module: %v\n", err)
		os.Exit(1)
	}
	defer module.Close()

	store := wasmtime.NewStore(engine)
	defer store.Close()
	instance, err := wasmtime.NewInstance(store, module, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasmer: instantiate module: %v\n", err)
		os.Exit(1)
	}

	function := instance.GetFunc(store, os.Args[2])
	if function == nil {
		fmt.Fprintf(os.Stderr, "wasmer: exported function %q not found\n", os.Args[2])
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
		fmt.Fprintf(os.Stderr, "wasmer: function %q requires unsupported arguments\n", os.Args[2])
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasmer: call %q: %v\n", os.Args[2], err)
		os.Exit(1)
	}
	fmt.Println(formatResult(result))
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
