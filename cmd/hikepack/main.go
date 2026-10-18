package main

import (
	"flag"
	"fmt"
	"os"

	"hikec-go/pkg/pack"
	"hikec-go/pkg/target"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		usage()
		if len(os.Args) < 2 {
			os.Exit(1)
		}
		return
	}
	if os.Args[1] != "syso" {
		fmt.Fprintf(os.Stderr, "hikepack: unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	flags := flag.NewFlagSet("syso", flag.ExitOnError)
	targetName := target.DefaultTarget().Name
	flags.StringVar(&targetName, "target", targetName, "target platform")
	verbose := flags.Bool("v", false, "enable verbose logging")
	verboseVery := flags.Bool("vv", false, "enable verbose logging")
	_ = flags.Parse(os.Args[2:])
	if *verboseVery {
		*verbose = true
	}

	moduleDir := "."
	if flags.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "hikepack syso: expected at most one module directory")
		os.Exit(2)
	}
	if flags.NArg() == 1 {
		moduleDir = flags.Arg(0)
	}
	if err := pack.BuildSyso(moduleDir, targetName, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "hikepack: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Usage: hikepack syso [-target <name>] [-v] [module-directory]")
	fmt.Println("\nReads hike.mod and builds Hike packages plus external static archives into Go .syso files.")
}
