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
	p              *hir.Program
	b              strings.Builder
	stringOffsets  map[string]int
	itabOffsets    map[string]int
	functionIndex  map[string]int
	indirectTypes  map[string]string
	nextType       int
	nextDataOffset int
}

func New(p *hir.Program, _ *sema.Context) *Emitter {
	return &Emitter{p: p, stringOffsets: make(map[string]int), itabOffsets: make(map[string]int), functionIndex: make(map[string]int), indirectTypes: make(map[string]string)}
}

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

func memoryOp(t sema.Type, load bool) string {
	prefix := watType(t)
	if prefix == "i32" {
		if t != nil {
			switch t.LLVMType() {
			case "i1", "i8":
				if load {
					return "i32.load8_u"
				}
				return "i32.store8"
			case "i16":
				if load {
					return "i32.load16_s"
				}
				return "i32.store16"
			}
		}
	}
	if load {
		return prefix + ".load"
	}
	return prefix + ".store"
}

func isIntegerType(t sema.Type) bool {
	if t == nil {
		return false
	}
	s := t.LLVMType()
	return strings.HasPrefix(s, "i")
}

func castExpr(from, to sema.Type, expression string) string {
	fromType, toType := watType(from), watType(to)
	if fromType == toType {
		return expression
	}
	switch {
	case fromType == "i64" && toType == "i32":
		return "(i32.wrap_i64 " + expression + ")"
	case fromType == "i32" && toType == "i64":
		if unsignedInteger(from) {
			return "(i64.extend_i32_u " + expression + ")"
		}
		return "(i64.extend_i32_s " + expression + ")"
	case fromType == "f64" && toType == "i32":
		return "(i32.trunc_f64_s " + expression + ")"
	case fromType == "i32" && toType == "f64":
		if unsignedInteger(from) {
			return "(f64.convert_i32_u " + expression + ")"
		}
		return "(f64.convert_i32_s " + expression + ")"
	case fromType == "i64" && toType == "f64":
		if unsignedInteger(from) {
			return "(f64.convert_i64_u " + expression + ")"
		}
		return "(f64.convert_i64_s " + expression + ")"
	case fromType == "f64" && toType == "i64":
		return "(i64.trunc_f64_s " + expression + ")"
	case isIntegerType(from) && isIntegerType(to):
		if fromType == "i32" && toType == "i64" {
			return "(i64.extend_i32_s " + expression + ")"
		}
		if fromType == "i64" && toType == "i32" {
			return "(i32.wrap_i64 " + expression + ")"
		}
	}
	return expression
}
func (e *Emitter) val(v hir.Value) string {
	if v == nil {
		return "i32.const 0"
	}
	switch x := v.(type) {
	case *hir.Reg:
		return "(local.get $" + reg(x) + ")"
	case *hir.GlobalVar:
		if index, ok := e.functionIndex[x.Name]; ok {
			return fmt.Sprintf("(i32.const %d)", index)
		}
		return "(global.get $" + x.Name + ")"
	case *hir.ConstInt:
		return fmt.Sprintf("(%s.const %d)", watType(x.Typ), x.Val)
	case *hir.ConstBool:
		if x.Val {
			return "(i32.const 1)"
		}
		return "(i32.const 0)"
	case *hir.ConstFloat:
		return fmt.Sprintf("(%s.const %s)", watType(x.Typ), strconv.FormatFloat(x.Val, 'g', -1, 64))
	case *hir.ConstString:
		return fmt.Sprintf("(i32.const %d)", e.stringOffsets[x.Label])
	default:
		return "(i32.const 0)"
	}
}

func dataBytes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, "\\%02x", s[i])
	}
	return b.String()
}

func typeSize(t sema.Type) int {
	if t == nil || t.Size() <= 0 {
		return 1
	}
	return t.Size()
}

func (e *Emitter) emitData() {
	offset := 0
	for _, sc := range e.p.StringConstants {
		e.stringOffsets[sc.Label] = offset
		// HIR string constants use a NUL-terminated view at the Wasm boundary.
		fmt.Fprintf(&e.b, "  (data (i32.const %d) \"%s\\00\")\n", offset, dataBytes(sc.Raw))
		offset += len(sc.Raw) + 1
	}
	e.nextDataOffset = offset
}

func (e *Emitter) signature(params []sema.Type, results []sema.Type) string {
	parts := make([]string, 0, len(params)+len(results)+1)
	for _, p := range params {
		parts = append(parts, "p:"+watType(p))
	}
	for _, r := range results {
		parts = append(parts, "r:"+watType(r))
	}
	return strings.Join(parts, ",")
}

func (e *Emitter) registerType(params []sema.Type, results []sema.Type) string {
	key := e.signature(params, results)
	if name, ok := e.indirectTypes[key]; ok {
		return name
	}
	name := fmt.Sprintf("$indirect_%d", e.nextType)
	e.nextType++
	e.indirectTypes[key] = name
	return name
}

func (e *Emitter) emitTypeDefs() {
	for key, name := range e.indirectTypes {
		var params, results []string
		for _, part := range strings.Split(key, ",") {
			if strings.HasPrefix(part, "p:") {
				params = append(params, "(param "+strings.TrimPrefix(part, "p:")+")")
			}
			if strings.HasPrefix(part, "r:") {
				results = append(results, "(result "+strings.TrimPrefix(part, "r:")+")")
			}
		}
		fmt.Fprintf(&e.b, "  (type %s (func %s))\n", name, strings.Join(append(params, results...), " "))
	}
}

func (e *Emitter) emitItabData() {
	for _, itab := range e.p.Itabs {
		e.itabOffsets[itab.GlobalName] = e.nextDataOffset
		var raw strings.Builder
		for _, method := range itab.Methods {
			fmt.Fprintf(&raw, "\\%02x\\%02x\\%02x\\%02x", e.functionIndex[method.TargetFnName]&255, (e.functionIndex[method.TargetFnName]>>8)&255, (e.functionIndex[method.TargetFnName]>>16)&255, (e.functionIndex[method.TargetFnName]>>24)&255)
		}
		fmt.Fprintf(&e.b, "  (data (i32.const %d) \"%s\")\n", e.nextDataOffset, raw.String())
		e.nextDataOffset += len(itab.Methods) * 4
	}
}

func resultTypes(r *hir.Reg) []sema.Type {
	if r == nil {
		return nil
	}
	if t, ok := r.Typ.(*sema.TupleType); ok {
		return t.Types
	}
	return []sema.Type{r.Typ}
}

func hasEnvironment(v hir.Value) bool {
	_, nilEnv := v.(*hir.ConstNil)
	return v != nil && !nilEnv
}

func (e *Emitter) prepareTypes() {
	for _, fn := range e.p.Functions {
		for _, bb := range fn.Blocks {
			for _, in := range bb.Instructions {
				switch x := in.(type) {
				case *hir.InstrCallIndirect:
					params := make([]sema.Type, len(x.Args))
					for i, a := range x.Args {
						params[i] = a.Type()
					}
					e.registerType(params, resultTypes(x.Dst))
					if hasEnvironment(x.EnvPtr) {
						params = append([]sema.Type{&sema.PointerType{Base: sema.TypeByte}}, params...)
						e.registerType(params, resultTypes(x.Dst))
					}
				case *hir.InstrCallIface:
					params := make([]sema.Type, 1, len(x.Args)+1)
					params[0] = &sema.PointerType{Base: sema.TypeByte}
					for _, a := range x.Args {
						params = append(params, a.Type())
					}
					e.registerType(params, resultTypes(x.Dst))
				}
			}
		}
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
	index := 0
	for _, fn := range e.p.Functions {
		if !fn.IsExtern {
			e.functionIndex[fn.Name] = index
			index++
		}
	}
	e.prepareTypes()
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
		} else {
			for _, rt := range fn.ReturnTypes {
				fmt.Fprintf(&e.b, " (result %s)", watType(rt))
			}
		}
		e.b.WriteString("))\n")
	}
	// Keep the requested initial stack address valid while leaving room for
	// static data and the future runtime heap (malloc/memory.grow).
	e.b.WriteString("  (memory (export \"memory\") 16)\n")
	e.b.WriteString("  (global $__sp (mut i32) (i32.const 65536))\n")
	for _, g := range e.p.Globals {
		fmt.Fprintf(&e.b, "  (global $%s (mut %s) (%s.const 0))\n", g.Name, watType(g.Typ), watType(g.Typ))
	}
	if len(e.functionIndex) > 0 {
		names := make([]string, 0, len(e.functionIndex))
		for _, fn := range e.p.Functions {
			if !fn.IsExtern {
				names = append(names, "$"+fn.Name)
			}
		}
		fmt.Fprintf(&e.b, "  (table funcref (elem %s))\n", strings.Join(names, " "))
	}
	e.emitData()
	e.emitItabData()
	e.emitTypeDefs()
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
	for _, rt := range fn.ReturnTypes {
		fmt.Fprintf(&e.b, " (result %s)", watType(rt))
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
	fmt.Fprintf(&e.b, " (local $pc i32)")
	fmt.Fprintf(&e.b, " (local $frame_sp i32)")
	e.b.WriteString("\n")
	e.b.WriteString("    (local.set $frame_sp (global.get $__sp))\n")
	e.emitCFG(fn)
	e.b.WriteString("  )\n")
	if fn.Name == "main" {
		fmt.Fprintf(&e.b, "  (export \"main\" (func $%s))\n", fn.Name)
	}
}

// emitCFG uses a small program-counter dispatcher. Each HIR basic block is a
// WAT case selected by br_table; this handles arbitrary forward/back edges
// without requiring a fragile source-level if/loop reconstruction.
func (e *Emitter) emitCFG(fn *hir.Function) {
	if len(fn.Blocks) == 0 {
		e.b.WriteString("    (unreachable)\n")
		return
	}
	e.b.WriteString("    (block $cfg_exit\n      (loop $cfg_dispatch\n")
	for i := len(fn.Blocks) - 1; i >= 0; i-- {
		fmt.Fprintf(&e.b, "        (block $cfg_%d\n", i)
	}
	labels := make([]string, len(fn.Blocks)+1)
	for i := range fn.Blocks {
		labels[i] = fmt.Sprintf("$cfg_%d", i)
	}
	labels[len(fn.Blocks)] = "$cfg_exit"
	fmt.Fprintf(&e.b, "          (br_table %s (local.get $pc))\n", strings.Join(labels, " "))
	for _, bb := range fn.Blocks {
		e.b.WriteString("        )\n")
		fmt.Fprintf(&e.b, "        ;; %s\n", bb.Label)
		for _, in := range bb.Instructions {
			e.instruction(in)
		}
		if bb.Terminator != nil {
			e.cfgTerminator(bb.Terminator, fn, fn.Blocks)
		} else {
			e.defaultReturn(fn)
		}
	}
	e.b.WriteString("      )\n    )\n")
	// The dispatcher is intentionally non-fallthrough: every case either
	// returns or branches back to the loop. Tell the validator that the
	// structural loop cannot leave a value on the function stack.
	e.b.WriteString("    (unreachable)\n")
}

func (e *Emitter) defaultReturn(fn *hir.Function) {
	e.b.WriteString("          (global.set $__sp (local.get $frame_sp))\n")
	if fn.Name == "main" {
		e.b.WriteString("          (return (i32.const 0))\n")
	} else {
		e.b.WriteString("          (return)\n")
	}
}

func (e *Emitter) cfgTerminator(t hir.Terminator, fn *hir.Function, blocks []*hir.BasicBlock) {
	switch x := t.(type) {
	case *hir.InstrJump:
		e.branchTo(x.Target, blocks)
	case *hir.InstrBranch:
		thenIdx, elseIdx := blockIndex(x.ThenTarget, blocks), blockIndex(x.ElseTarget, blocks)
		fmt.Fprintf(&e.b, "          (if %s (then (local.set $pc (i32.const %d)) (br $cfg_dispatch)) (else (local.set $pc (i32.const %d)) (br $cfg_dispatch)))\n", e.val(x.Cond), thenIdx, elseIdx)
	case *hir.InstrReturn:
		if len(x.Vals) == 0 {
			e.defaultReturn(fn)
		} else {
			e.b.WriteString("          (global.set $__sp (local.get $frame_sp))\n")
			values := make([]string, len(x.Vals))
			for i, v := range x.Vals {
				values[i] = e.val(v)
			}
			fmt.Fprintf(&e.b, "          (return %s)\n", strings.Join(values, " "))
		}
	case *hir.InstrUnreachable:
		e.b.WriteString("          (unreachable)\n")
	}
}

func unsignedInteger(t sema.Type) bool {
	if t == nil {
		return false
	}
	if b, ok := t.(*sema.BasicType); ok {
		switch b {
		case sema.TypeUint, sema.TypeUint64, sema.TypeUint32, sema.TypeUint16, sema.TypeUint8, sema.TypeUintptr, sema.TypeByte:
			return true
		}
	}
	return false
}

func blockIndex(label string, blocks []*hir.BasicBlock) int {
	for i, b := range blocks {
		if b.Label == label {
			return i
		}
	}
	return len(blocks)
}
func (e *Emitter) branchTo(label string, blocks []*hir.BasicBlock) {
	fmt.Fprintf(&e.b, "          (local.set $pc (i32.const %d))\n          (br $cfg_dispatch)\n", blockIndex(label, blocks))
}

func (e *Emitter) advanceSP(size int) {
	if size < 1 {
		size = 1
	}
	fmt.Fprintf(&e.b, "    (global.set $__sp (i32.add (global.get $__sp) (i32.const %d)))\n", size)
}

func aggregateField(t sema.Type, index int) (sema.Type, int, bool) {
	switch a := t.(type) {
	case *sema.PointerType:
		return aggregateField(a.Base, index)
	case *sema.StructType:
		if index < 0 || index >= len(a.Fields) {
			return nil, 0, false
		}
		offset := 0
		for i := 0; i < index; i++ {
			offset += typeSize(a.Fields[i].Type)
		}
		return a.Fields[index].Type, offset, true
	case *sema.TupleType:
		if index < 0 || index >= len(a.Types) {
			return nil, 0, false
		}
		offset := 0
		for i := 0; i < index; i++ {
			offset += typeSize(a.Types[i])
		}
		return a.Types[index], offset, true
	case *sema.FuncType:
		if index < 0 || index >= 2 {
			return nil, 0, false
		}
		return &sema.PointerType{Base: sema.TypeByte}, index * sema.PointerSize, true
	}
	return nil, 0, false
}

func aggregateType(t sema.Type) bool {
	_, _, ok := aggregateField(t, 0)
	return ok
}

func (e *Emitter) instruction(in hir.Instruction) {
	switch x := in.(type) {
	case *hir.InstrBinary:
		op := map[hir.Opcode]string{hir.OpAdd: "add", hir.OpSub: "sub", hir.OpMul: "mul", hir.OpDiv: "div_s", hir.OpRem: "rem_s", hir.OpAnd: "and", hir.OpOr: "or", hir.OpXor: "xor", hir.OpShl: "shl", hir.OpShr: "shr_s", hir.OpEq: "eq", hir.OpNeq: "ne", hir.OpLt: "lt_s", hir.OpLe: "le_s", hir.OpGt: "gt_s", hir.OpGe: "ge_s"}[x.Op]
		if unsignedInteger(x.L.Type()) || x.LogicalShift {
			switch x.Op {
			case hir.OpDiv:
				op = "div_u"
			case hir.OpRem:
				op = "rem_u"
			case hir.OpShr:
				op = "shr_u"
			case hir.OpLt:
				op = "lt_u"
			case hir.OpLe:
				op = "le_u"
			case hir.OpGt:
				op = "gt_u"
			case hir.OpGe:
				op = "ge_u"
			}
		}
		if x.L.Type() != nil && (x.L.Type() == sema.TypeFloat32 || x.L.Type() == sema.TypeFloat64) {
			op = map[hir.Opcode]string{hir.OpAdd: "add", hir.OpSub: "sub", hir.OpMul: "mul", hir.OpDiv: "div", hir.OpEq: "eq", hir.OpNeq: "ne", hir.OpLt: "lt", hir.OpLe: "le", hir.OpGt: "gt", hir.OpGe: "ge"}[x.Op]
		}
		prefix := watType(x.L.Type())
		e.set(x.Dst, "("+prefix+"."+op+" "+e.val(x.L)+" "+e.val(x.R)+")")
	case *hir.InstrUnary:
		if x.Op == hir.OpNeg {
			e.set(x.Dst, "("+watType(x.Val.Type())+".sub ("+watType(x.Val.Type())+".const 0) "+e.val(x.Val)+")")
		} else {
			e.set(x.Dst, "(i32.eqz "+e.val(x.Val)+")")
		}
	case *hir.InstrCallStatic:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = e.val(a)
		}
		call := "(call $" + x.CalleeName
		if len(args) > 0 {
			call += " " + strings.Join(args, " ")
		}
		call += ")"
		e.set(x.Dst, call)
	case *hir.InstrCallIndirect:
		params := make([]sema.Type, len(x.Args))
		for i, a := range x.Args {
			params[i] = a.Type()
		}
		plainArgs := make([]string, 0, len(x.Args)+1)
		for _, a := range x.Args {
			plainArgs = append(plainArgs, e.val(a))
		}
		plainArgs = append(plainArgs, e.val(x.FnPtr))
		plainCall := fmt.Sprintf("(call_indirect (type %s) %s)", e.registerType(params, resultTypes(x.Dst)), strings.Join(plainArgs, " "))
		if !hasEnvironment(x.EnvPtr) {
			e.set(x.Dst, plainCall)
			break
		}
		closureParams := append([]sema.Type{&sema.PointerType{Base: sema.TypeByte}}, params...)
		closureArgs := append([]string{e.val(x.EnvPtr)}, plainArgs[:len(plainArgs)-1]...)
		closureArgs = append(closureArgs, e.val(x.FnPtr))
		closureCall := fmt.Sprintf("(call_indirect (type %s) %s)", e.registerType(closureParams, resultTypes(x.Dst)), strings.Join(closureArgs, " "))
		condition := "(i32.eqz " + e.val(x.EnvPtr) + ")"
		if x.Dst != nil {
			e.set(x.Dst, fmt.Sprintf("(if (result %s) %s (then %s) (else %s))", watType(x.Dst.Typ), condition, plainCall, closureCall))
		} else {
			e.b.WriteString(fmt.Sprintf("    (if %s (then %s) (else %s))\n", condition, plainCall, closureCall))
		}
	case *hir.InstrCallIface:
		params := make([]sema.Type, 1, len(x.Args)+1)
		params[0] = &sema.PointerType{Base: sema.TypeByte}
		for _, a := range x.Args {
			params = append(params, a.Type())
		}
		itab := fmt.Sprintf("(i32.load (i32.add (i32.load (i32.add %s (i32.const 4))) (i32.const %d)))", e.val(x.IfaceVal), x.MethodIndex*4)
		args := []string{"(i32.load " + e.val(x.IfaceVal) + ")"}
		for _, a := range x.Args {
			args = append(args, e.val(a))
		}
		args = append(args, itab)
		e.set(x.Dst, fmt.Sprintf("(call_indirect (type %s) %s)", e.registerType(params, resultTypes(x.Dst)), strings.Join(args, " ")))
	case *hir.InstrLoad:
		e.set(x.Dst, "("+memoryOp(x.Dst.Typ, true)+" "+e.val(x.Ptr)+")")
	case *hir.InstrStore:
		e.b.WriteString("    (" + memoryOp(x.Val.Type(), false) + " " + e.val(x.Ptr) + " " + e.val(x.Val) + ")\n")
	case *hir.InstrCast:
		e.set(x.Dst, castExpr(x.Val.Type(), x.ToType, e.val(x.Val)))
	case *hir.InstrUnboxInterface:
		data := "(i32.load " + e.val(x.IfaceVal) + ")"
		if _, ok := x.TargetType.(*sema.PointerType); ok {
			e.set(x.Dst, data)
		} else {
			e.set(x.Dst, "("+memoryOp(x.TargetType, true)+" "+data+")")
		}
	case *hir.InstrBoxInterface:
		base := "(global.get $__sp)"
		e.set(x.Dst, base)
		e.advanceSP(8)
		data := e.val(x.Val)
		if _, ok := x.Val.Type().(*sema.PointerType); !ok {
			data = "(global.get $__sp)"
			e.advanceSP(typeSize(x.Val.Type()))
			e.b.WriteString("    (" + memoryOp(x.Val.Type(), false) + " " + data + " " + e.val(x.Val) + ")\n")
		}
		e.b.WriteString("    (i32.store " + base + " " + data + ")\n")
		itabOffset := e.itabOffsets[x.ItabName]
		e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) (i32.const %d))\n", base, itabOffset))
	case *hir.InstrAlloca:
		e.set(x.Dst, "(global.get $__sp)")
		e.advanceSP(typeSize(x.AllocType))
	case *hir.InstrAllocaDynamic:
		e.set(x.Dst, "(global.get $__sp)")
		e.b.WriteString("    (global.set $__sp (i32.add (global.get $__sp) " + e.val(x.Size) + "))\n")
	case *hir.InstrHeapAlloc:
		e.set(x.Dst, "(global.get $__sp)")
		e.b.WriteString("    (global.set $__sp (i32.add (global.get $__sp) " + e.val(x.Size) + "))\n")
	case *hir.InstrGetFieldPtr:
		offset := 0
		if p, ok := x.BasePtr.Type().(*sema.PointerType); ok {
			if st, ok := p.Base.(*sema.StructType); ok {
				for n := 0; n < x.FieldIndex && n < len(st.Fields); n++ {
					offset += typeSize(st.Fields[n].Type)
				}
			}
		}
		e.set(x.Dst, fmt.Sprintf("(i32.add %s (i32.const %d))", e.val(x.BasePtr), offset))
	case *hir.InstrGetElemPtr:
		elemSize := 1
		if p, ok := x.Dst.Typ.(*sema.PointerType); ok {
			elemSize = typeSize(p.Base)
		}
		e.set(x.Dst, fmt.Sprintf("(i32.add %s (i32.mul %s (i32.const %d)))", e.val(x.BasePtr), e.val(x.Index), elemSize))
	case *hir.InstrExtractValue:
		fieldType, offset, ok := aggregateField(x.Agg.Type(), x.Index)
		if !ok {
			e.set(x.Dst, "(i32.const 0)")
			break
		}
		e.set(x.Dst, fmt.Sprintf("(%s %s)", memoryOp(fieldType, true), e.aggregateAddress(x.Agg, offset, typeSize(x.Agg.Type()))))
	case *hir.InstrInsertValue:
		fieldType, offset, ok := aggregateField(x.Agg.Type(), x.Index)
		if !ok {
			e.set(x.Dst, "(i32.const 0)")
			break
		}
		base := e.aggregateAddress(x.Agg, 0, typeSize(x.Agg.Type()))
		e.set(x.Dst, base)
		e.b.WriteString(fmt.Sprintf("    (%s (i32.add %s (i32.const %d)) %s)\n", memoryOp(fieldType, false), base, offset, e.val(x.Val)))
	}
}

func (e *Emitter) aggregateAddress(agg hir.Value, offset, size int) string {
	if r, ok := agg.(*hir.Reg); ok && aggregateType(r.Typ) {
		return fmt.Sprintf("(i32.add (local.get $%s) (i32.const %d))", reg(r), offset)
	}
	base := "(global.get $__sp)"
	e.advanceSP(size)
	return fmt.Sprintf("(i32.add %s (i32.const %d))", base, offset)
}
