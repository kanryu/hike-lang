package sema

import (
	"testing"

	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

func TestMemoryBlockMetadata(t *testing.T) {
	program := parser.New(lexer.New(`package main

var threadable(64) { workerCount int }
var concurrent(128) { total int }
`)).ParseProgram()
	ctx, err := Analyze(program)
	if err != nil {
		t.Fatalf("memory block analysis failed: %v", err)
	}
	if ctx.GlobalMemoryBlocks["workerCount"] != 0 || ctx.GlobalMemorySizes["workerCount"] != 64*1024 {
		t.Fatalf("unexpected threadable metadata: class=%v size=%d", ctx.GlobalMemoryBlocks["workerCount"], ctx.GlobalMemorySizes["workerCount"])
	}
	if ctx.GlobalMemoryBlocks["total"] != 1 || ctx.GlobalMemorySizes["total"] != 128*1024 {
		t.Fatalf("unexpected concurrent metadata: class=%v size=%d", ctx.GlobalMemoryBlocks["total"], ctx.GlobalMemorySizes["total"])
	}
}
