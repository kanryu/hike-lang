// Package wabt emits WebAssembly text directly from HIR.
package wabt

import (
	"fmt"
	"strconv"
	"strings"

	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

type Emitter struct {
	p *hir.Program
	b strings.Builder
}

func New(p *hir.Program, _ *sema.Context) *Emitter { return &Emitter{p: p} }

func watType(t sema.Type) string {
	if t == nil {
		return "i32"
	}
	s := t.LLVMType()
	switch s {
	case "double":
		return "f64"
	case "float":
		return "f32"
	case "i64":
		return "i64"
	default:
		return "i32"
	}
}
func val(v hir.Value) string {
	if v == nil {
		return "i32.const 0"
	}
	switch x := v.(type) {
	case *hir.Reg:
		return "(local.get $" + reg(x) + ")"
	case *hir.ConstInt:
		return fmt.Sprintf("(%s.const %d)", watType(x.Typ), x.Val)
	case *hir.ConstBool:
		if x.Val {
			return "(i32.const 1)"
		}
		return "(i32.const 0)"
	case *hir.ConstFloat:
		return fmt.Sprintf("(%s.const %s)", watType(x.Typ), strconv.FormatFloat(x.Val, 'g', -1, 64))
	default:
		return "(i32.const 0)"
	}
}
func reg(r *hir.Reg) string {
	if r.Name != "" {
		return r.Name
	}
	return fmt.Sprintf("v%d", r.ID)
}
func (e *Emitter) set(r *hir.Reg, expr string) {
	if r != nil {
		e.b.WriteString("    (local.set $" + reg(r) + " " + expr + ")\n")
	}
}

// Emit produces a valid WAT module for the scalar HIR instructions. Complex
// runtime operations remain ordinary imports, allowing WABT to validate and
// assemble the module while the runtime supplies their implementation.
func (e *Emitter) Emit() string {
	e.b.WriteString("(module\n")
	for _, fn := range e.p.Functions {
		if !fn.IsExtern {
			continue
		}
		fmt.Fprintf(&e.b, "  (import \"env\" \"%s\" (func $%s", fn.Name, fn.Name)
		for _, p := range fn.Params {
			fmt.Fprintf(&e.b, " (param %s)", watType(p.Typ))
		}
		if len(fn.ReturnTypes) == 1 {
			fmt.Fprintf(&e.b, " (result %s)", watType(fn.ReturnTypes[0]))
		}
		e.b.WriteString("))\n")
	}
	e.b.WriteString("  (memory (export \"memory\") 1)\n")
	for _, fn := range e.p.Functions {
		e.function(fn)
	}
	e.b.WriteString(")\n")
	return e.b.String()
}
func (e *Emitter) function(fn *hir.Function) {
	if fn.IsExtern {
		return
	}
	e.b.WriteString("  (func $")
	e.b.WriteString(fn.Name)
	for _, p := range fn.Params {
		fmt.Fprintf(&e.b, " (param $%s %s)", reg(p), watType(p.Typ))
	}
	if len(fn.ReturnTypes) == 1 {
		fmt.Fprintf(&e.b, " (result %s)", watType(fn.ReturnTypes[0]))
	}
	regs := map[string]bool{}
	for _, bb := range fn.Blocks {
		for _, in := range bb.Instructions {
			if r := in.Result(); r != nil {
				n := reg(r)
				if !regs[n] {
					fmt.Fprintf(&e.b, " (local $%s %s)", n, watType(r.Typ))
					regs[n] = true
				}
			}
		}
	}
	e.b.WriteString("\n")
	for _, bb := range fn.Blocks {
		fmt.Fprintf(&e.b, "    ;; %s\n", bb.Label)
		for _, in := range bb.Instructions {
			e.instruction(in)
		}
		if bb.Terminator != nil {
			e.terminator(bb.Terminator, fn)
		}
	}
	e.b.WriteString("  )\n")
	if fn.Name == "main" {
		fmt.Fprintf(&e.b, "  (export \"main\" (func $%s))\n", fn.Name)
	}
}
func (e *Emitter) instruction(in hir.Instruction) {
	switch x := in.(type) {
	case *hir.InstrBinary:
		op := map[hir.Opcode]string{hir.OpAdd: "add", hir.OpSub: "sub", hir.OpMul: "mul", hir.OpDiv: "div_s", hir.OpRem: "rem_s", hir.OpAnd: "and", hir.OpOr: "or", hir.OpXor: "xor", hir.OpShl: "shl", hir.OpShr: "shr_s"}[x.Op]
		if x.L.Type() != nil && (x.L.Type() == sema.TypeFloat32 || x.L.Type() == sema.TypeFloat64) {
			op = map[hir.Opcode]string{hir.OpAdd: "add", hir.OpSub: "sub", hir.OpMul: "mul", hir.OpDiv: "div"}[x.Op]
		}
		prefix := watType(x.L.Type())
		e.set(x.Dst, "("+prefix+"."+op+" "+val(x.L)+" "+val(x.R)+")")
	case *hir.InstrUnary:
		if x.Op == hir.OpNeg {
			e.set(x.Dst, "("+watType(x.Val.Type())+".sub ("+watType(x.Val.Type())+".const 0) "+val(x.Val)+")")
		} else {
			e.set(x.Dst, "(i32.eqz "+val(x.Val)+")")
		}
	case *hir.InstrCallStatic:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = val(a)
		}
		call := "(call $" + x.CalleeName
		if len(args) > 0 {
			call += " " + strings.Join(args, " ")
		}
		call += ")"
		e.set(x.Dst, call)
	case *hir.InstrLoad:
		e.set(x.Dst, "("+watType(x.Dst.Typ)+".load "+val(x.Ptr)+")")
	case *hir.InstrStore:
		e.b.WriteString("    (i32.store " + val(x.Ptr) + " " + val(x.Val) + ")\n")
	case *hir.InstrCast:
		e.set(x.Dst, val(x.Val))
	case *hir.InstrAlloca, *hir.InstrAllocaDynamic, *hir.InstrHeapAlloc:
		if r := in.Result(); r != nil {
			e.set(r, "(i32.const 0)")
		}
	case *hir.InstrExtractValue:
		e.set(x.Dst, "(i32.const 0)")
	case *hir.InstrInsertValue:
		e.set(x.Dst, val(x.Val))
	}
}
func (e *Emitter) terminator(t hir.Terminator, fn *hir.Function) {
	switch x := t.(type) {
	case *hir.InstrReturn:
		if len(x.Vals) == 0 {
			if fn.Name == "main" {
				e.b.WriteString("    i32.const 0\n")
			} else {
				e.b.WriteString("    return\n")
			}
		} else {
			e.b.WriteString("    " + val(x.Vals[0]) + "\n    return\n")
		}
	case *hir.InstrUnreachable:
		e.b.WriteString("    unreachable\n")
	}
}
