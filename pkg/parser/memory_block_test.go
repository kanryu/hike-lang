package parser

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
)

func TestMemoryBlockDeclarations(t *testing.T) {
	program := New(lexer.New(`package main

var threadable(64) {
    workerCount int
    workerName string
}

var concurrent(128) {
    total int
}
`)).ParseProgram()
	if len(program.Decls) != 2 {
		t.Fatalf("expected two memory blocks, got %d", len(program.Decls))
	}
	threadable, ok := program.Decls[0].(*ast.MemoryBlockDecl)
	if !ok || threadable.Kind != ast.ThreadableMemoryBlock || len(threadable.Vars) != 2 {
		t.Fatalf("unexpected threadable declaration: %#v", program.Decls[0])
	}
	concurrent, ok := program.Decls[1].(*ast.MemoryBlockDecl)
	if !ok || concurrent.Kind != ast.ConcurrentMemoryBlock || len(concurrent.Vars) != 1 {
		t.Fatalf("unexpected concurrent declaration: %#v", program.Decls[1])
	}
}
