// hike-hike is a small command-line WABT/WAT frontend.
//
// It accepts one Hike source path, compiles it through the normal frontend,
// and writes WebAssembly text (WAT) to stdout or to -o's output path.
package main

import (
	"fmt"
	"os"
	"strings"

	"hikec-go/pkg/compiler"
	"hikec-go/pkg/target"
)

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: hike-hike [-o output.wat] [--wasm-mode=normal|concurrent] <source.hike> [source.hike ...]")
}

func main() {
	output := ""
	wasmMode := "normal"
	var sources []string

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-o", "--output":
			if i+1 >= len(os.Args) {
				usage()
				os.Exit(2)
			}
			output = os.Args[i+1]
			i++
		case "-h", "--help":
			usage()
			return
		default:
			if strings.HasPrefix(os.Args[i], "--wasm-mode=") {
				wasmMode = strings.TrimPrefix(os.Args[i], "--wasm-mode=")
				break
			}
			if len(os.Args[i]) > 0 && os.Args[i][0] == '-' {
				fmt.Fprintf(os.Stderr, "hike-hike: unknown option %q\n", os.Args[i])
				usage()
				os.Exit(2)
			}
			sources = append(sources, os.Args[i])
		}
	}

	if len(sources) == 0 {
		usage()
		os.Exit(2)
	}

	tgt := target.TargetWabt
	comp := compiler.New(&tgt)
	comp.SetWasmMode(wasmMode)
	wat, _, _, err := comp.CompileToWAT(sources...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hike-hike: compilation failed: %v\n", err)
		os.Exit(1)
	}

	if output == "" {
		fmt.Print(wat)
		return
	}
	if err := os.WriteFile(output, []byte(wat), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "hike-hike: cannot write %s: %v\n", output, err)
		os.Exit(1)
	}
}
