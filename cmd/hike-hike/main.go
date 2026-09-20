// hike-hike is a small command-line WABT/WAT frontend.
//
// It accepts one Hike source path, compiles it through the normal frontend,
// and writes WebAssembly text (WAT) to stdout or to -o's output path.
package main

import (
	"fmt"
	"os"

	"hikec-go/pkg/compiler"
	"hikec-go/pkg/target"
)

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: hike-hike [-o output.wat] <source.hike>")
}

func main() {
	output := ""
	source := ""

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
			if len(os.Args[i]) > 0 && os.Args[i][0] == '-' {
				fmt.Fprintf(os.Stderr, "hike-hike: unknown option %q\n", os.Args[i])
				usage()
				os.Exit(2)
			}
			if source != "" {
				fmt.Fprintln(os.Stderr, "hike-hike: exactly one source path is required")
				usage()
				os.Exit(2)
			}
			source = os.Args[i]
		}
	}

	if source == "" {
		usage()
		os.Exit(2)
	}

	tgt := target.TargetWabt
	comp := compiler.New(&tgt)
	wat, _, _, err := comp.CompileToWAT(source)
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
