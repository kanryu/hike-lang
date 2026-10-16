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

func TestMappedMemoryBlockModifierMetadata(t *testing.T) {
	program := parser.New(lexer.New(`package main
var concurrent(64, mapped(state)) { value int }
`)).ParseProgram()
	ctx, err := Analyze(program)
	if err != nil {
		t.Fatalf("mapped memory block analysis failed: %v", err)
	}
	if ctx.GlobalMemoryMapNames["value"] != "state" {
		t.Fatalf("unexpected mapped variable name: %q", ctx.GlobalMemoryMapNames["value"])
	}
	if owner, ok := ctx.MappedHandleOwner("state"); !ok || owner != "value" {
		t.Fatalf("unexpected mapped handle owner: %q, %v", owner, ok)
	}
	handleType, ok := ctx.MappedHandleType("state")
	if !ok || handleType.TypeName() != "mapped<state>" || handleType.Owner != "value" {
		t.Fatalf("unexpected mapped handle type: %#v, %v", handleType, ok)
	}
}
