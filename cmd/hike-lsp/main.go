package main

import (
	"hikec-go/pkg/lsp"
	"os"
)

func main() {
	if err := lsp.New(os.Stdin, os.Stdout).Run(); err != nil {
		os.Exit(1)
	}
}
