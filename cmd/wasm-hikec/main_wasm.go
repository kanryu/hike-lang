// This file is intentionally Go-shaped source for -go-hike mode.
// HikeC reads .go files as Hike source in that mode; it is not a Go/js
// bootstrap and must not import syscall/js.
package main

import "hikec-go/pkg/ast"
import "hikec-go/pkg/backend/wabt"
import hir "hikec-go/pkg/hir"
import "hikec-go/pkg/lexer"
import "hikec-go/pkg/lower"
import "hikec-go/pkg/parser"
import "hikec-go/pkg/sema"
import "hikec-go/pkg/transform"

// Compile is the compiler implementation. This is ordinary Go syntax because
// Go-Hike mode reads this .go file as a Hike-compatible package source. The
// Hike entry point in main.hike calls it; this function never calls JavaScript.
func Compile(source string) int {
	// The browser compiler is intentionally a fixed wasm32/WAT compiler.
	// Avoid importing target.Target here: Go-Hike's reduced type checker does
	// not need the target registry and cannot reliably infer that struct value.
	sema.SetTargetArchitecture("wasm32-unknown-unknown")
	var parsed *ast.Program
	parsed = parser.New(lexer.New(source)).ParseProgram()
	var ctx *sema.Context
	var err error
	ctx, err = sema.AnalyzeMode(parsed, false)
	if err != nil || ctx == nil {
		return 0
	}
	var concrete *ast.Program
	concrete, err = transform.New(parsed, ctx).Transform()
	if err != nil || concrete == nil {
		return 0
	}
	var lowerer *lower.Lowerer
	lowerer = lower.New(concrete, ctx)
	lowerer.Set32Bit(true)
	lowerer.SetRecordLocations(false)
	var program *hir.Program
	program = lowerer.Lower()
	return wasm_hikec_publish_wat(wabt.New(program, ctx).Emit())
}
