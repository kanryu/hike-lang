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
	// callArgTotal/callArgUsed track string literal temporaries while emitting
	// one call.  The WAT expressions for call arguments are evaluated after all
	// argument setup instructions have run, so `(sp - size)` alone would make
	// every temporary refer to the last allocation.
	callArgTotal     int
	callArgUsed      int
	taskEnvs         map[*hir.Reg]bool
	taskCalls        map[*hir.Reg]taskCallSignature
	asyncTypes       map[string]int
	asyncSigs        []taskCallSignature
	concurrent       bool
	debugInfo        bool
	runtimeFunctions int
}

type taskCallSignature struct {
	params  []sema.Type
	results []sema.Type
}

func New(p *hir.Program, _ *sema.Context) *Emitter {
	return &Emitter{p: p, stringOffsets: make(map[string]int), itabOffsets: make(map[string]int), functionIndex: make(map[string]int), indirectTypes: make(map[string]string), taskEnvs: make(map[*hir.Reg]bool), taskCalls: make(map[*hir.Reg]taskCallSignature), asyncTypes: make(map[string]int)}
}

// SetConcurrent enables the shared-memory/host-worker ABI used by the
// concurrent Wasm build mode. Normal Wasm keeps the synchronous future ABI.
func (e *Emitter) SetConcurrent(enabled bool) { e.concurrent = enabled }

// SetDebugInfo enables WAT markers used to recover HIR instruction offsets
// after wat2wasm has assembled the module. The markers are harmless blocks and
// are omitted entirely from normal builds.
func (e *Emitter) SetDebugInfo(enabled bool) { e.debugInfo = enabled }

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
	case fromType == "f32" && toType == "f64":
		return "(f64.promote_f32 " + expression + ")"
	case fromType == "f64" && toType == "f32":
		return "(f32.demote_f64 " + expression + ")"
	case fromType == "i64" && toType == "i32":
		return "(i32.wrap_i64 " + expression + ")"
	case fromType == "i32" && toType == "i64":
		if unsignedInteger(from) {
			return "(i64.extend_i32_u " + expression + ")"
		}
		return "(i64.extend_i32_s " + expression + ")"
	case fromType == "f64" && toType == "i32":
		return "(i32.trunc_f64_s " + expression + ")"
	case fromType == "f32" && toType == "i32":
		return "(i32.trunc_f32_s " + expression + ")"
	case fromType == "i32" && toType == "f64":
		if unsignedInteger(from) {
			return "(f64.convert_i32_u " + expression + ")"
		}
		return "(f64.convert_i32_s " + expression + ")"
	case fromType == "i32" && toType == "f32":
		if unsignedInteger(from) {
			return "(f32.convert_i32_u " + expression + ")"
		}
		return "(f32.convert_i32_s " + expression + ")"
	case fromType == "i64" && toType == "f64":
		if unsignedInteger(from) {
			return "(f64.convert_i64_u " + expression + ")"
		}
		return "(f64.convert_i64_s " + expression + ")"
	case fromType == "i64" && toType == "f32":
		if unsignedInteger(from) {
			return "(f32.convert_i64_u " + expression + ")"
		}
		return "(f32.convert_i64_s " + expression + ")"
	case fromType == "f64" && toType == "i64":
		return "(i64.trunc_f64_s " + expression + ")"
	case fromType == "f32" && toType == "i64":
		return "(i64.trunc_f32_s " + expression + ")"
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
		name := globalVarName(x)
		if index, ok := e.functionIndex[name]; ok {
			return fmt.Sprintf("(i32.const %d)", index)
		}
		return "(global.get $" + name + ")"
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
		return fmt.Sprintf("(i32.const %d)", e.stringOffsets[constStringLabel(x)])
	default:
		return "(i32.const 0)"
	}
}

// valAs renders a value in the WebAssembly type required by the consuming
// instruction. HIR constants are sometimes deliberately represented with the
// language's default int type, while the surrounding operation is pointer/i32
// sized. WAT requires both operands to have exactly the same stack type.
func (e *Emitter) valAs(v hir.Value, target sema.Type) string {
	if v == nil {
		return fmt.Sprintf("(%s.const 0)", watType(target))
	}
	return castExpr(v.Type(), target, e.val(v))
}

// Keep field access outside the interface type-switch. Go-Hike's reduced
// checker otherwise may infer the switch variable as an integer value.
func globalVarName(g *hir.GlobalVar) string       { return g.Name }
func constStringLabel(s *hir.ConstString) string  { return s.Label }
func itabTargetName(m hir.ItabMethodEntry) string { return m.TargetFnName }
func boxItabName(x *hir.InstrBoxInterface) string { return x.ItabName }

func dataBytes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, "\\%02x", s[i])
	}
	return b.String()
}

func typeSize(t sema.Type) int {
	if t == nil || sema.SizeOf(t) <= 0 {
		return 1
	}
	return sema.SizeOf(t)
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
		// A type-info table is a read-only record shared by all interface
		// values of this concrete/interface pair:
		//   +0: concrete TypeID
		//   +4: function-table index for method 0
		//   +8: function-table index for method 1, ...
		// Keep every record 4-byte aligned because all fields are i32.
		e.nextDataOffset = (e.nextDataOffset + 3) &^ 3
		e.itabOffsets[itab.GlobalName] = e.nextDataOffset
		var raw strings.Builder
		typeID := uint32(itab.TypeID)
		fmt.Fprintf(&raw, "\\%02x\\%02x\\%02x\\%02x", byte(typeID), byte(typeID>>8), byte(typeID>>16), byte(typeID>>24))
		for _, method := range itab.Methods {
			name := itabTargetName(method)
			fmt.Fprintf(&raw, "\\%02x\\%02x\\%02x\\%02x", e.functionIndex[name]&255, (e.functionIndex[name]>>8)&255, (e.functionIndex[name]>>16)&255, (e.functionIndex[name]>>24)&255)
		}
		ifaceName := ""
		if itab.InterfaceType != nil {
			ifaceName = itab.InterfaceType.Name
		}
		fmt.Fprintf(&e.b, "  ;; typeinfo %s for %s, typeid=%d, methods=%d\n", itab.GlobalName, ifaceName, typeID, len(itab.Methods))
		fmt.Fprintf(&e.b, "  (data (i32.const %d) \"%s\")\n", e.nextDataOffset, raw.String())
		e.nextDataOffset += 4 + len(itab.Methods)*4
	}
}

func resultTypes(r *hir.Reg) []sema.Type {
	if r == nil {
		return nil
	}
	if t, ok := r.Typ.(*sema.TupleType); ok {
		// WABT lowers multi-value function returns to a pointer to a packed
		// result area. Indirect calls must use that ABI as well; emitting the
		// source tuple here would make the call_indirect type return multiple
		// values while the surrounding HIR register expects one pointer.
		_ = t
		return []sema.Type{sema.TypeUint32}
	}
	return []sema.Type{r.Typ}
}

func multiValueSize(types []sema.Type) int {
	size := 0
	for _, typ := range types {
		size += typeSize(typ)
	}
	return size
}

func indirectCallResults(x *hir.InstrCallIndirect) []sema.Type { return resultTypes(x.Dst) }
func ifaceCallResults(x *hir.InstrCallIface) []sema.Type       { return resultTypes(x.Dst) }
func hirFunctionExtern(fn *hir.Function) bool                  { return fn.IsExtern }

func hasEnvironment(v hir.Value) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case *hir.ConstNil, *hir.ConstZero:
		return false
	case *hir.ConstInt:
		return x.Val != 0
	default:
		return true
	}
}

func taskSignatureKey(sig taskCallSignature) string {
	parts := make([]string, 0, len(sig.params)+len(sig.results)+1)
	for _, p := range sig.params {
		parts = append(parts, "p:"+watType(p))
	}
	parts = append(parts, "r")
	for _, r := range sig.results {
		parts = append(parts, "r:"+watType(r))
	}
	return strings.Join(parts, ",")
}

func (e *Emitter) asyncSignature(fn *hir.InstrAsync) int {
	results := fn.RetTypes
	if len(results) > 1 {
		results = []sema.Type{sema.TypeUint32}
	}
	sig := taskCallSignature{results: results}
	if hasEnvironment(fn.EnvPtr) {
		sig.params = []sema.Type{&sema.PointerType{Base: sema.TypeByte}}
	}
	key := taskSignatureKey(sig)
	if id, ok := e.asyncTypes[key]; ok {
		return id
	}
	id := len(e.asyncSigs)
	e.asyncTypes[key] = id
	e.asyncSigs = append(e.asyncSigs, sig)
	return id
}

func (e *Emitter) prepareTypes() {
	for _, fn := range e.p.Functions {
		for _, bb := range fn.Blocks {
			for _, in := range bb.Instructions {
				switch x := in.(type) {
				case *hir.InstrAsync:
					e.taskEnvs[x.Dst] = hasEnvironment(x.EnvPtr)
					params := []sema.Type{}
					if e.taskEnvs[x.Dst] {
						params = append(params, &sema.PointerType{Base: sema.TypeByte})
					}
					results := x.RetTypes
					if len(results) > 1 {
						results = []sema.Type{sema.TypeUint32}
					}
					e.taskCalls[x.Dst] = taskCallSignature{params: params, results: results}
					e.asyncSignature(x)
				case *hir.InstrCallIndirect:
					params := make([]sema.Type, len(x.Args))
					for i, a := range x.Args {
						params[i] = a.Type()
					}
					e.registerType(params, indirectCallResults(x))
					if hasEnvironment(x.EnvPtr) {
						params = append([]sema.Type{&sema.PointerType{Base: sema.TypeByte}}, params...)
						e.registerType(params, indirectCallResults(x))
					}
				case *hir.InstrCallIface:
					params := make([]sema.Type, 1, len(x.Args)+1)
					params[0] = &sema.PointerType{Base: sema.TypeByte}
					for _, a := range x.Args {
						params = append(params, a.Type())
					}
					e.registerType(params, ifaceCallResults(x))
				case *hir.InstrTaskWait:
					var results []sema.Type
					if taskReg, ok := x.Task.(*hir.Reg); ok {
						if sig, exists := e.taskCalls[taskReg]; exists {
							results = sig.results
						}
					}
					if results == nil {
						future, _ := x.Task.Type().(*sema.FutureType)
						if future != nil {
							results = future.ReturnTypes
						}
						if len(results) > 1 {
							results = []sema.Type{sema.TypeUint32}
						}
					}
					e.registerType(nil, results)
					e.registerType([]sema.Type{&sema.PointerType{Base: sema.TypeByte}}, results)
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

// functionSymbol reserves the same system/runtime names as the LLVM backend.
// A user-defined function with one of those names gets a private WAT spelling.
func (e *Emitter) functionSymbol(name string) string {
	name = normalizeWabtRuntimeName(name)
	if _, exists := e.functionIndex[name]; !exists {
		if sep := strings.LastIndex(name, "_"); sep > 0 && sep+1 < len(name) {
			pointerMethod := name[:sep] + "_ptr_sema_" + name[sep+1:]
			if _, exists := e.functionIndex[pointerMethod]; exists {
				name = pointerMethod
			}
		}
	}
	if _, userDefined := e.functionIndex[name]; userDefined && IsWabtRuntimeSymbol(name) {
		return "$__hike_user_" + name
	}
	return "$" + name
}

func (e *Emitter) callReturnsValue(name string) bool {
	name = normalizeWabtRuntimeName(name)
	for _, fn := range e.p.Functions {
		if fn.Name == name {
			return len(fn.ReturnTypes) > 0
		}
	}
	switch name {
	case "malloc", "calloc", "memcpy", "memcmp", "strlen", "strcmp",
		"hike_streq", "hike_streq_len", "hike_strcat_len", "__hike_map_create",
		"__hike_map_len", "__hike_map_get", "__hike_string_less":
		return true
	}
	return false
}

// Emit produces a valid WAT module for the scalar HIR instructions. Complex
// runtime operations remain ordinary imports, allowing WABT to validate and
// assemble the module while the runtime supplies their implementation.
func (e *Emitter) Emit() string {
	index := 0
	for _, fn := range e.p.Functions {
		if !hirFunctionExtern(fn) {
			e.functionIndex[fn.Name] = index
			index++
		}
	}
	e.prepareTypes()
	e.b.WriteString("(module\n")
	if e.concurrent && len(e.asyncSigs) > 0 {
		e.b.WriteString("  (import \"env\" \"hike_thread_spawn\" (func $__hike_thread_spawn (param i32) (param i32) (param i32) (param i32)))\n")
		e.b.WriteString("  (import \"env\" \"hike_thread_pump\" (func $__hike_thread_pump))\n")
	}
	for _, fn := range e.p.Functions {
		if !hirFunctionExtern(fn) {
			continue
		}
		if isWasmRuntime(fn.Name) {
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
	// static data and the runtime heap. Concurrent mode uses a one-megabyte
	// shared-memory budget; the checker assigns the worker arena within it.
	if e.concurrent {
		e.b.WriteString("  (import \"env\" \"memory\" (memory 16 16 shared))\n")
		e.b.WriteString("  (export \"memory\" (memory 0))\n")
	} else {
		e.b.WriteString("  (memory (export \"memory\") 16)\n")
	}
	e.b.WriteString("  (global $__sp (mut i32) (i32.const 65536))\n")
	// The checker runs main for initialization and then invokes a user
	// function. Export the stack pointer so that runner-side initialization
	// can preserve stack-backed global views before the second call.
	e.b.WriteString("  (export \"__hike_sp\" (global $__sp))\n")
	// Leave room for the main stack and the concurrent worker arena.
	e.b.WriteString("  (global $__heap (mut i32) (i32.const 196608))\n")
	e.b.WriteString("  (export \"__hike_heap\" (global $__heap))\n")
	e.b.WriteString("  (global $__region_active (mut i32) (i32.const 0))\n")
	e.b.WriteString("  (global $__region_begin_count (mut i32) (i32.const 0))\n")
	e.b.WriteString("  (global $__region_end_count (mut i32) (i32.const 0))\n")
	e.b.WriteString("  (global $__region_released_bytes (mut i32) (i32.const 0))\n")
	for _, g := range e.p.Globals {
		fmt.Fprintf(&e.b, "  (global $%s (mut %s) (%s.const 0))\n", g.Name, watType(g.Typ), watType(g.Typ))
		if e.concurrent && (strings.HasPrefix(g.Name, "eventloop_") || g.Name == "testOutputBuffer" || g.Name == "mainDeviceID") {
			fmt.Fprintf(&e.b, "  (export \"__hike_global_%s\" (global $%s))\n", g.Name, g.Name)
		}
	}
	if len(e.functionIndex) > 0 {
		names := make([]string, 0, len(e.functionIndex))
		for _, fn := range e.p.Functions {
			if !hirFunctionExtern(fn) {
				names = append(names, e.functionSymbol(fn.Name))
			}
		}
		fmt.Fprintf(&e.b, "  (table funcref (elem %s))\n", strings.Join(names, " "))
	}
	e.emitData()
	e.emitItabData()
	e.emitTypeDefs()
	e.emitRuntime()
	if e.concurrent {
		e.emitAsyncDispatcher()
	}
	for _, fn := range e.p.Functions {
		e.function(fn)
	}
	if e.concurrent {
		e.emitConcurrentBootstrap()
	}
	e.b.WriteString(")\n")
	return e.b.String()
}

func (e *Emitter) emitConcurrentBootstrap() {
	for _, fn := range e.p.Functions {
		if fn.IsExtern || fn.Name != "main" {
			continue
		}
		e.b.WriteString("  (func $__hike_bootstrap\n")
		call := ""
		switch len(fn.Params) {
		case 0:
			call = fmt.Sprintf("(call %s)", e.functionSymbol(fn.Name))
		case 2:
			call = fmt.Sprintf("(call %s (i32.const 0) (i32.const 0))", e.functionSymbol(fn.Name))
		default:
			return
		}
		if len(fn.ReturnTypes) > 0 {
			fmt.Fprintf(&e.b, "    (drop %s)\n", call)
		} else {
			fmt.Fprintf(&e.b, "    %s\n", call)
		}
		e.b.WriteString("  )\n  (export \"__hike_bootstrap\" (func $__hike_bootstrap))\n")
		return
	}
}

func (e *Emitter) emitConcurrentAsync(x *hir.InstrAsync) {
	task := "(call $malloc (i32.const 16))"
	e.set(x.Dst, task)
	e.taskEnvs[x.Dst] = hasEnvironment(x.EnvPtr)
	params := []sema.Type{}
	if e.taskEnvs[x.Dst] {
		params = append(params, &sema.PointerType{Base: sema.TypeByte})
	}
	results := x.RetTypes
	if len(results) > 1 {
		results = []sema.Type{sema.TypeUint32}
	}
	e.taskCalls[x.Dst] = taskCallSignature{params: params, results: results}
	taskBase := e.val(x.Dst)
	e.b.WriteString(fmt.Sprintf("    (i32.store %s %s)\n", taskBase, e.valAs(x.FnPtr, sema.TypeUint32)))
	env := "(i32.const 0)"
	if x.EnvPtr != nil {
		env = e.valAs(x.EnvPtr, sema.TypeUint32)
	}
	e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) %s)\n", taskBase, env))
	e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 8)) (i32.const 0))\n", taskBase))
	e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 12)) (i32.const 0))\n", taskBase))
	fmt.Fprintf(&e.b, "    (call $__hike_thread_spawn %s %s %s (i32.const %d))\n", e.val(x.FnPtr), env, taskBase, e.asyncSignature(x))
}

// emitAsyncDispatcher is the Wasm-side half of the host worker ABI. The host
// receives the table index and task pointer through hike_thread_spawn, then
// calls this dispatcher. Keeping the signature selection in Wasm preserves
// call_indirect's type safety while allowing the host to remain type-agnostic.
func (e *Emitter) emitAsyncDispatcher() {
	if len(e.asyncSigs) == 0 {
		return
	}
	e.b.WriteString("  (func $__hike_worker_dispatch (param $fn i32) (param $env i32) (param $task i32) (param $sig i32)\n")
	for id, sig := range e.asyncSigs {
		fmt.Fprintf(&e.b, "    (if (i32.eq (local.get $sig) (i32.const %d)) (then\n", id)
		emitCall := func(indent, callType, args string) {
			call := fmt.Sprintf("(call_indirect (type %s) %s)", callType, args)
			if len(sig.results) > 0 {
				fmt.Fprintf(&e.b, "%s(%s (i32.add (local.get $task) (i32.const 8)) %s)\n", indent, memoryOp(sig.results[0], false), call)
			}
		}
		if len(sig.params) > 0 {
			withEnv := e.registerType(sig.params, sig.results)
			withoutEnv := e.registerType(nil, sig.results)
			e.b.WriteString("      (if (i32.eqz (local.get $env)) (then\n")
			emitCall("        ", withoutEnv, "(local.get $fn)")
			e.b.WriteString("      ) (else\n")
			emitCall("        ", withEnv, "(local.get $env) (local.get $fn)")
			e.b.WriteString("      ))\n")
		} else {
			callType := e.registerType(nil, sig.results)
			emitCall("      ", callType, "(local.get $fn)")
		}
		e.b.WriteString("      (i32.store (i32.add (local.get $task) (i32.const 12)) (i32.const 1))\n")
		e.b.WriteString("    ))\n")
	}
	e.b.WriteString("  )\n  (export \"__hike_worker_dispatch\" (func $__hike_worker_dispatch))\n")
}
func (e *Emitter) function(fn *hir.Function) {
	if hirFunctionExtern(fn) {
		return
	}
	e.b.WriteString("  (func ")
	e.b.WriteString(e.functionSymbol(fn.Name))
	for _, p := range fn.Params {
		fmt.Fprintf(&e.b, " (param $%s %s)", reg(p), watType(p.Typ))
	}
	if len(fn.ReturnTypes) > 1 {
		fmt.Fprint(&e.b, " (result i32)")
	} else {
		for _, rt := range fn.ReturnTypes {
			fmt.Fprintf(&e.b, " (result %s)", watType(rt))
		}
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
	if len(fn.ReturnTypes) > 1 {
		fmt.Fprintf(&e.b, " (local $__ret_multi i32)")
	}
	e.b.WriteString("\n")
	if e.concurrent && strings.HasSuffix(fn.Name, "eventloop_Run") {
		// The host must not start workers before the event loop is ready. Mark
		// the loop active and pump deferred workers at its entry point.
		e.b.WriteString("    (global.set $eventloop_running (i32.const 1))\n")
		e.b.WriteString("    (call $__hike_thread_pump)\n")
	}
	e.b.WriteString("    (local.set $frame_sp (global.get $__sp))\n")
	e.emitCFG(fn)
	e.b.WriteString("  )\n")
	// Export user functions as well as main/C ABI functions.  This gives the
	// Wasmtime test harness a stable entry point for string-returning test
	// functions (for example testOutput() string), without changing the Hike
	// source-level ABI.  External declarations are skipped by function().
	if !fn.IsExtern {
		exportName := fn.Name
		fmt.Fprintf(&e.b, "  (export %q (func %s))\n", exportName, e.functionSymbol(fn.Name))
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
			e.debugMarker()
			e.instruction(in)
		}
		if bb.Terminator != nil {
			// CFG terminators are backend control-flow plumbing, not source
			// statements.  Marking them makes stepping jump back to an if/for
			// header after a real statement (for example AppendText), even
			// though no user-level instruction is executing there.
			// A return is different: its marker is placed after the final
			// expression instructions, so return-value locals are observable
			// before control leaves the function.
			if _, ok := bb.Terminator.(*hir.InstrReturn); ok {
				e.debugMarker()
			}
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

func (e *Emitter) debugMarker() {
	if e.debugInfo {
		e.b.WriteString("        (block (nop))\n")
	}
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
			if len(x.Vals) > 1 {
				size := multiValueSize(fn.ReturnTypes)
				e.b.WriteString(fmt.Sprintf("          (local.set $__ret_multi (call $malloc (i32.const %d)))\n", size))
				offset := 0
				for i, value := range x.Vals {
					if i >= len(fn.ReturnTypes) {
						break
					}
					typ := fn.ReturnTypes[i]
					ptr := fmt.Sprintf("(i32.add (local.get $__ret_multi) (i32.const %d))", offset)
					if typ == sema.TypeString {
						if s, ok := value.(*hir.ConstString); ok {
							e.b.WriteString(fmt.Sprintf("          (i32.store %s (call $malloc (i32.const 12)))\n", ptr))
							view := fmt.Sprintf("(i32.load %s)", ptr)
							e.b.WriteString(fmt.Sprintf("          (i32.store %s %s)\n", view, e.val(s)))
							e.b.WriteString(fmt.Sprintf("          (i32.store (i32.add %s (i32.const 4)) (i32.const 0))\n", view))
							e.b.WriteString(fmt.Sprintf("          (i32.store (i32.add %s (i32.const 8)) (i32.const %d))\n", view, len(s.Raw)))
						} else {
							e.b.WriteString(fmt.Sprintf("          (i32.store %s %s)\n", ptr, e.valAs(value, typ)))
						}
					} else if aggregateType(typ) {
						// Aggregate tuple fields are represented by a pointer in the
						// packed multi-return area.  The caller's ExtractValue then
						// copies the aggregate from that pointer.  The callee frame is
						// released before returning, so the copy must live on the heap.
						storage := "(call $malloc (i32.const " + strconv.Itoa(typeSize(typ)) + "))"
						e.b.WriteString(fmt.Sprintf("          (i32.store %s %s)\n", ptr, storage))
						if _, zero := value.(*hir.ConstZero); zero {
							e.b.WriteString(fmt.Sprintf("          (memory.fill (i32.load %s) (i32.const 0) (i32.const %d))\n", ptr, typeSize(typ)))
						} else {
							e.b.WriteString(fmt.Sprintf("          (memory.copy (i32.load %s) %s (i32.const %d))\n", ptr, e.valAs(value, typ), typeSize(typ)))
						}
					} else {
						e.b.WriteString(fmt.Sprintf("          (%s %s %s)\n", memoryOp(typ, false), ptr, e.valAs(value, typ)))
					}
					offset += typeSize(typ)
				}
				e.b.WriteString("          (global.set $__sp (local.get $frame_sp))\n")
				e.b.WriteString("          (return (local.get $__ret_multi))\n")
				break
			}
			e.b.WriteString("          (global.set $__sp (local.get $frame_sp))\n")
			values := make([]string, len(x.Vals))
			for i, v := range x.Vals {
				if len(x.Vals) == 1 && fn.ReturnTypes[0] == sema.TypeString {
					if s, ok := v.(*hir.ConstString); ok {
						// A string literal is a data pointer while a Hike string
						// value is a pointer to its three-word view. Materialize
						// the view in the frame before returning it to the host.
						base := e.val(s)
						e.b.WriteString("          (i32.store (local.get $frame_sp) " + base + ")\n")
						e.b.WriteString("          (i32.store (i32.add (local.get $frame_sp) (i32.const 4)) (i32.const 0))\n")
						e.b.WriteString(fmt.Sprintf("          (i32.store (i32.add (local.get $frame_sp) (i32.const 8)) (i32.const %d))\n", len(s.Raw)))
						values[i] = "(local.get $frame_sp)"
						continue
					}
				}
				if aggregateType(fn.ReturnTypes[i]) {
					if _, zero := v.(*hir.ConstZero); zero {
						e.b.WriteString(fmt.Sprintf("          (memory.fill (local.get $frame_sp) (i32.const 0) (i32.const %d))\n", typeSize(fn.ReturnTypes[i])))
						values[i] = "(local.get $frame_sp)"
						continue
					}
				}
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
	case *sema.BasicType:
		if a == sema.TypeString {
			switch index {
			case 0:
				return &sema.PointerType{Base: sema.TypeByte}, 0, true
			case 1, 2:
				return sema.TypeUint32, index * 4, true
			}
		}
		return nil, 0, false
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
	case *sema.ArrayType:
		if index < 0 || index >= a.Len {
			return nil, 0, false
		}
		return a.Elem, index * typeSize(a.Elem), true
	case *sema.SliceType:
		if index < 0 || index >= 3 {
			return nil, 0, false
		}
		// wasm32 slices are represented as {data, len, cap}, all i32.
		if index == 0 {
			return &sema.PointerType{Base: sema.TypeByte}, 0, true
		}
		return sema.TypeUint32, index * 4, true
	case *sema.FuncType:
		if index < 0 || index >= 2 {
			return nil, 0, false
		}
		return &sema.PointerType{Base: sema.TypeByte}, index * sema.PointerSize, true
	case *sema.InterfaceType:
		if index < 0 || index >= 2 {
			return nil, 0, false
		}
		if a.IsAny() {
			if index == 0 {
				return sema.TypeInt32, 0, true
			}
			return &sema.PointerType{Base: sema.TypeByte}, 4, true
		}
		if index == 0 {
			return &sema.PointerType{Base: sema.TypeByte}, 0, true
		}
		return sema.TypeInt32, 4, true
	}
	return nil, 0, false
}

func aggregateType(t sema.Type) bool {
	if t == sema.TypeString {
		return true
	}
	switch t.(type) {
	case *sema.StructType, *sema.TupleType, *sema.ArrayType, *sema.SliceType, *sema.FuncType, *sema.InterfaceType:
		return true
	default:
		return false
	}
}

func (e *Emitter) emitBoxInterface(x *hir.InstrBoxInterface) {
	baseBefore := "(global.get $__sp)"
	e.set(x.Dst, baseBefore)
	e.advanceSP(8)
	data := e.val(x.Val)
	dataSize := 0
	if s, ok := x.Val.(*hir.ConstString); ok {
		dataSize = typeSize(sema.TypeString)
		e.advanceSP(dataSize)
		data = fmt.Sprintf("(i32.sub (global.get $__sp) (i32.const %d))", dataSize)
		e.b.WriteString(fmt.Sprintf("    (i32.store %s %s)\n", data, e.val(s)))
		e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) (i32.const 0))\n", data))
		e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 8)) (i32.const %d))\n", data, len(s.Raw)))
	} else if x.Val.Type() == sema.TypeString || x.Val.Type().TypeName() == "string" {
		// LLVM boxes an aggregate string by storing the complete string
		// view in an alloca and using its address as the any data pointer.
		// Copy all three wasm32 words here; copying only the first word
		// loses offset/length and corrupts variadic %s formatting.
		dataSize = typeSize(sema.TypeString)
		e.advanceSP(dataSize)
		data = fmt.Sprintf("(i32.sub (global.get $__sp) (i32.const %d))", dataSize)
		e.b.WriteString(fmt.Sprintf("    (memory.copy %s %s (i32.const %d))\n", data, e.val(x.Val), dataSize))
	} else if aggregateType(x.Val.Type()) {
		// Aggregate HIR values are represented by pointers in WABT. Keep
		// the existing value address as any's data pointer; storing that
		// address into a temporary slot would introduce an extra indirection.
		data = e.val(x.Val)
	} else if _, ok := x.Val.Type().(*sema.PointerType); !ok {
		dataSize = typeSize(x.Val.Type())
		e.advanceSP(dataSize)
		data = fmt.Sprintf("(i32.sub (global.get $__sp) (i32.const %d))", dataSize)
		e.b.WriteString("    (" + memoryOp(x.Val.Type(), false) + " " + data + " " + e.val(x.Val) + ")\n")
	}
	base := fmt.Sprintf("(i32.sub (global.get $__sp) (i32.const %d))", 8+dataSize)
	typeValue := e.itabOffsets[boxItabName(x)]
	if x.Iface != nil && x.Iface.IsAny() {
		typeValue = int(x.TypeID)
		e.b.WriteString(fmt.Sprintf("    (i32.store %s (i32.const %d))\n", base, typeValue))
		e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) %s)\n", base, data))
	} else {
		e.b.WriteString("    (i32.store " + base + " " + data + ")\n")
		e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) (i32.const %d))\n", base, typeValue))
	}
}

func (e *Emitter) emitStore(x *hir.InstrStore) {
	if global, ok := x.Ptr.(*hir.GlobalVar); ok {
		if x.Val.Type() == sema.TypeString || x.Val.Type().TypeName() == "string" {
			// Global string values must outlive main's temporary stack frame.
			// Allocate a stable 12-byte string view on the heap instead of
			// storing the address of a stack materialization in the global.
			stable := "(global.get $" + globalVarName(global) + ")"
			e.b.WriteString(fmt.Sprintf("    (global.set $%s (call $malloc (i32.const 12)))\n", globalVarName(global)))
			if s, isConst := x.Val.(*hir.ConstString); isConst {
				e.b.WriteString(fmt.Sprintf("    (i32.store %s %s)\n", stable, e.val(s)))
				e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) (i32.const 0))\n", stable))
				e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 8)) (i32.const %d))\n", stable, len(s.Raw)))
			} else {
				e.b.WriteString(fmt.Sprintf("    (memory.copy %s %s (i32.const 12))\n", stable, e.val(x.Val)))
			}
			e.b.WriteString(fmt.Sprintf("    (global.set $%s %s)\n", globalVarName(global), stable))
			return
		}
		e.b.WriteString(fmt.Sprintf("    (global.set $%s %s)\n", globalVarName(global), e.val(x.Val)))
		return
	}
	if aggregateType(x.Val.Type()) {
		if s, ok := x.Val.(*hir.ConstString); ok {
			base := e.val(x.Ptr)
			e.b.WriteString(fmt.Sprintf("    (i32.store %s %s)\n", base, e.val(s)))
			e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) (i32.const 0))\n", base))
			e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 8)) (i32.const %d))\n", base, len(s.Raw)))
			return
		}
		size := typeSize(x.Val.Type())
		if _, zero := x.Val.(*hir.ConstZero); zero {
			e.b.WriteString(fmt.Sprintf("    (memory.fill %s (i32.const 0) (i32.const %d))\n", e.val(x.Ptr), size))
		} else {
			e.b.WriteString(fmt.Sprintf("    (memory.copy %s %s (i32.const %d))\n", e.val(x.Ptr), e.val(x.Val), size))
		}
		return
	}
	e.b.WriteString("    (" + memoryOp(x.Val.Type(), false) + " " + e.val(x.Ptr) + " " + e.val(x.Val) + ")\n")
}

func (e *Emitter) emitTaskWait(x *hir.InstrTaskWait) {
	if !e.concurrent {
		future, _ := x.Task.Type().(*sema.FutureType)
		var retTypes []sema.Type
		if future != nil {
			retTypes = future.ReturnTypes
		}
		fnPtr := "(i32.load " + e.val(x.Task) + ")"
		envPtr := "(i32.load (i32.add " + e.val(x.Task) + " (i32.const 4)))"
		callResultTypes := retTypes
		if taskReg, ok := x.Task.(*hir.Reg); ok {
			if sig, exists := e.taskCalls[taskReg]; exists {
				callResultTypes = sig.results
			}
		}
		plainCall := fmt.Sprintf("(call_indirect (type %s) %s)", e.registerType(nil, callResultTypes), fnPtr)
		closureParams := []sema.Type{&sema.PointerType{Base: sema.TypeByte}}
		closureCall := fmt.Sprintf("(call_indirect (type %s) %s %s)", e.registerType(closureParams, callResultTypes), envPtr, fnPtr)
		call := ""
		if len(callResultTypes) == 0 {
			call = fmt.Sprintf("(if (i32.eqz %s) (then %s) (else %s))", envPtr, plainCall, closureCall)
		} else {
			call = fmt.Sprintf("(if (result %s) (i32.eqz %s) (then %s) (else %s))", watType(callResultTypes[0]), envPtr, plainCall, closureCall)
		}
		if len(retTypes) > 1 {
			e.set(x.Dst, call)
		} else {
			size := 1
			if len(retTypes) == 1 {
				size = typeSize(retTypes[0])
			}
			e.set(x.Dst, fmt.Sprintf("(call $malloc (i32.const %d))", size))
			if len(retTypes) == 1 {
				e.b.WriteString(fmt.Sprintf("    (%s %s %s)\n", memoryOp(retTypes[0], false), e.val(x.Dst), call))
			}
		}
		return
	}

	future, _ := x.Task.Type().(*sema.FutureType)
	var retTypes []sema.Type
	if future != nil {
		retTypes = future.ReturnTypes
	}
	resultLoad := "i32.load"
	if len(retTypes) == 1 {
		resultLoad = memoryOp(retTypes[0], true)
	}
	result := fmt.Sprintf("(%s (i32.add %s (i32.const 8)))", resultLoad, e.val(x.Task))
	if len(retTypes) > 1 {
		e.set(x.Dst, result)
	} else {
		size := 1
		if len(retTypes) == 1 {
			size = typeSize(retTypes[0])
		}
		e.set(x.Dst, fmt.Sprintf("(call $malloc (i32.const %d))", size))
		if len(retTypes) == 1 {
			e.b.WriteString(fmt.Sprintf("    (%s %s %s)\n", memoryOp(retTypes[0], false), e.val(x.Dst), result))
		}
	}
}

func (e *Emitter) emitCallIndirect(x *hir.InstrCallIndirect) {
	params := make([]sema.Type, len(x.Args))
	for i, a := range x.Args {
		params[i] = a.Type()
	}
	e.callArgTotal = 0
	e.callArgUsed = 0
	for _, a := range x.Args {
		if _, ok := a.(*hir.ConstString); ok {
			e.callArgTotal += typeSize(sema.TypeString)
		} else if _, ok := a.(*hir.ConstZero); ok && aggregateType(a.Type()) {
			e.callArgTotal += typeSize(a.Type())
		}
	}
	plainArgs := make([]string, 0, len(x.Args)+1)
	for _, a := range x.Args {
		plainArgs = append(plainArgs, e.callArg(a))
	}
	plainArgs = append(plainArgs, e.val(x.FnPtr))
	plainCall := fmt.Sprintf("(call_indirect (type %s) %s)", e.registerType(params, indirectCallResults(x)), strings.Join(plainArgs, " "))
	if !hasEnvironment(x.EnvPtr) {
		e.set(x.Dst, plainCall)
		e.callArgTotal = 0
		e.callArgUsed = 0
		return
	}
	closureParams := append([]sema.Type{&sema.PointerType{Base: sema.TypeByte}}, params...)
	closureArgs := append([]string{e.val(x.EnvPtr)}, plainArgs[:len(plainArgs)-1]...)
	closureArgs = append(closureArgs, e.val(x.FnPtr))
	closureCall := fmt.Sprintf("(call_indirect (type %s) %s)", e.registerType(closureParams, indirectCallResults(x)), strings.Join(closureArgs, " "))
	condition := "(i32.eqz " + e.val(x.EnvPtr) + ")"
	if x.Dst != nil {
		e.set(x.Dst, fmt.Sprintf("(if (result %s) %s (then %s) (else %s))", watType(x.Dst.Typ), condition, plainCall, closureCall))
	} else {
		e.b.WriteString(fmt.Sprintf("    (if %s (then %s) (else %s))\n", condition, plainCall, closureCall))
	}
	e.callArgTotal = 0
	e.callArgUsed = 0
}

func (e *Emitter) emitCallStatic(x *hir.InstrCallStatic) {
	aggregateResult := x.Dst != nil && aggregateType(x.Dst.Typ)
	if aggregateResult {
		// Reserve the destination before entering the callee. A returned
		// aggregate points into the callee's frame, so reserving this slot
		// first prevents the caller's copy from overlapping that frame.
		e.set(x.Dst, "(global.get $__sp)")
		e.advanceSP(typeSize(x.Dst.Typ))
	}
	e.callArgTotal = 0
	e.callArgUsed = 0
	for _, a := range x.Args {
		if _, ok := a.(*hir.ConstString); ok {
			e.callArgTotal += typeSize(sema.TypeString)
		} else if _, ok := a.(*hir.ConstZero); ok && aggregateType(a.Type()) {
			e.callArgTotal += typeSize(a.Type())
		}
	}
	args := make([]string, len(x.Args))
	for i, a := range x.Args {
		args[i] = e.callArg(a)
	}
	call := "(call " + e.functionSymbol(x.CalleeName)
	if len(args) > 0 {
		call += " " + strings.Join(args, " ")
	}
	call += ")"
	if aggregateResult {
		e.b.WriteString(fmt.Sprintf("    (memory.copy (local.get $%s) %s (i32.const %d))\n", reg(x.Dst), call, typeSize(x.Dst.Typ)))
	} else if x.Dst != nil {
		e.set(x.Dst, call)
	} else if e.callReturnsValue(x.CalleeName) {
		e.b.WriteString("    (drop " + call + ")\n")
	} else {
		e.b.WriteString("    " + call + "\n")
	}
	e.callArgTotal = 0
	e.callArgUsed = 0
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
		e.set(x.Dst, "("+prefix+"."+op+" "+e.valAs(x.L, x.L.Type())+" "+e.valAs(x.R, x.L.Type())+")")
	case *hir.InstrUnary:
		if x.Op == hir.OpNeg {
			e.set(x.Dst, "("+watType(x.Val.Type())+".sub ("+watType(x.Val.Type())+".const 0) "+e.val(x.Val)+")")
		} else {
			e.set(x.Dst, "(i32.eqz "+e.val(x.Val)+")")
		}
	case *hir.InstrCallStatic:
		e.emitCallStatic(x)
	case *hir.InstrCallIndirect:
		e.emitCallIndirect(x)
	case *hir.InstrCallIface:
		params := make([]sema.Type, 1, len(x.Args)+1)
		params[0] = &sema.PointerType{Base: sema.TypeByte}
		for _, a := range x.Args {
			params = append(params, a.Type())
		}
		e.callArgTotal = 0
		e.callArgUsed = 0
		for _, a := range x.Args {
			if _, ok := a.(*hir.ConstString); ok {
				e.callArgTotal += typeSize(sema.TypeString)
			}
		}
		itab := fmt.Sprintf("(i32.load (i32.add (i32.load (i32.add %s (i32.const 4))) (i32.const %d)))", e.val(x.IfaceVal), 4+x.MethodIndex*4)
		args := []string{"(i32.load " + e.val(x.IfaceVal) + ")"}
		for _, a := range x.Args {
			args = append(args, e.callArg(a))
		}
		args = append(args, itab)
		e.set(x.Dst, fmt.Sprintf("(call_indirect (type %s) %s)", e.registerType(params, ifaceCallResults(x)), strings.Join(args, " ")))
		e.callArgTotal = 0
		e.callArgUsed = 0
	case *hir.InstrAsync:
		if e.concurrent {
			e.emitConcurrentAsync(x)
			break
		}
		// Normal Wasm keeps futures local and executes them at TaskWait.
		task := "(call $malloc (i32.const 8))"
		e.set(x.Dst, task)
		e.taskEnvs[x.Dst] = hasEnvironment(x.EnvPtr)
		params := []sema.Type{}
		if e.taskEnvs[x.Dst] {
			params = append(params, &sema.PointerType{Base: sema.TypeByte})
		}
		results := x.RetTypes
		if len(results) > 1 {
			results = []sema.Type{sema.TypeUint32}
		}
		e.taskCalls[x.Dst] = taskCallSignature{params: params, results: results}
		taskBase := e.val(x.Dst)
		e.b.WriteString(fmt.Sprintf("    (i32.store %s %s)\n", taskBase, e.valAs(x.FnPtr, sema.TypeUint32)))
		env := "(i32.const 0)"
		if x.EnvPtr != nil {
			env = e.valAs(x.EnvPtr, sema.TypeUint32)
		}
		e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) %s)\n", taskBase, env))
		break
		/*
			// Match the LLVM/Wasm task ABI: the host is notified at Async creation
			// time and is responsible for starting the worker dispatcher.
			task := "(call $malloc (i32.const 16))"
			e.set(x.Dst, task)
			e.taskEnvs[x.Dst] = hasEnvironment(x.EnvPtr)
			params := []sema.Type{}
			if e.taskEnvs[x.Dst] {
				params = append(params, &sema.PointerType{Base: sema.TypeByte})
			}
			results := x.RetTypes
			if len(results) > 1 {
				results = []sema.Type{sema.TypeUint32}
			}
			e.taskCalls[x.Dst] = taskCallSignature{params: params, results: results}
			taskBase := e.val(x.Dst)
			e.b.WriteString(fmt.Sprintf("    (i32.store %s %s)\n", taskBase, e.valAs(x.FnPtr, sema.TypeUint32)))
			env := "(i32.const 0)"
			if x.EnvPtr != nil {
				env = e.valAs(x.EnvPtr, sema.TypeUint32)
			}
			e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) %s)\n", taskBase, env))
			e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 8)) (i32.const 0))\n", taskBase))
			e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 12)) (i32.const 0))\n", taskBase))
			fmt.Fprintf(&e.b, "    (call $__hike_thread_spawn %s %s %s (i32.const %d))\n", e.val(x.FnPtr), env, taskBase, e.asyncSignature(x))
		*/
	case *hir.InstrTaskWait:
		e.emitTaskWait(x)
	case *hir.InstrLoad:
		if global, ok := x.Ptr.(*hir.GlobalVar); ok {
			e.set(x.Dst, e.val(global))
			break
		}
		if aggregateType(x.Dst.Typ) {
			// Aggregate HIR values are represented by their storage address in
			// WAT. Loading one therefore preserves the address; copying the
			// bytes belongs to the corresponding aggregate store.
			e.set(x.Dst, e.valAs(x.Ptr, sema.TypeUint32))
			break
		}
		e.set(x.Dst, "("+memoryOp(x.Dst.Typ, true)+" "+e.val(x.Ptr)+")")
	case *hir.InstrStore:
		e.emitStore(x)
	case *hir.InstrCast:
		e.set(x.Dst, castExpr(x.Val.Type(), x.ToType, e.val(x.Val)))
	case *hir.InstrUnboxInterface:
		dataPtr := e.val(x.IfaceVal)
		if iface, ok := x.IfaceVal.Type().(*sema.InterfaceType); ok && iface.IsAny() {
			dataPtr = "(i32.add " + dataPtr + " (i32.const 4))"
		}
		data := "(i32.load " + dataPtr + ")"
		if _, ok := x.TargetType.(*sema.PointerType); ok || aggregateType(x.TargetType) {
			e.set(x.Dst, data)
		} else {
			e.set(x.Dst, "("+memoryOp(x.TargetType, true)+" "+data+")")
		}
	case *hir.InstrChanMake:
		e.emitChanMake(x)
	case *hir.InstrChanSend:
		e.emitChanSend(x)
	case *hir.InstrChanRecv:
		e.emitChanRecv(x)
	case *hir.InstrBoxInterface:
		e.emitBoxInterface(x)
	case *hir.InstrAlloca:
		e.set(x.Dst, "(global.get $__sp)")
		e.advanceSP(typeSize(x.AllocType))
	case *hir.InstrAllocaDynamic:
		e.set(x.Dst, "(global.get $__sp)")
		e.b.WriteString("    (global.set $__sp (i32.add (global.get $__sp) " + e.val(x.Size) + "))\n")
	case *hir.InstrHeapAlloc:
		e.set(x.Dst, "(call $malloc "+e.val(x.Size)+")")
	case *hir.InstrRegionBegin:
		e.set(x.Dst, "(call $__hike_region_begin)")
	case *hir.InstrRegionAlloc:
		e.set(x.Dst, "(call $__hike_region_alloc "+e.val(x.Region)+" "+e.val(x.Size)+")")
	case *hir.InstrRegionEnd:
		e.b.WriteString("    (call $__hike_region_end " + e.val(x.Region) + ")\n")
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
		e.set(x.Dst, fmt.Sprintf("(i32.add %s (i32.mul %s (i32.const %d)))", e.valAs(x.BasePtr, sema.TypeUint32), e.valAs(x.Index, sema.TypeUint32), elemSize))
	case *hir.InstrExtractValue:
		if s, ok := x.Agg.(*hir.ConstString); ok {
			switch x.Index {
			case 0:
				e.set(x.Dst, e.val(s))
			case 1:
				e.set(x.Dst, "(i32.const 0)")
			case 2:
				e.set(x.Dst, fmt.Sprintf("(i32.const %d)", len(s.Raw)))
			default:
				e.set(x.Dst, "(i32.const 0)")
			}
			break
		}
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

func chanElemSize(t sema.Type) int {
	size := typeSize(t)
	if size < 1 {
		return 1
	}
	return size
}

func (e *Emitter) emitChanMake(x *hir.InstrChanMake) {
	elemSize := chanElemSize(x.ElemType)
	capVal := e.valAs(x.Cap, sema.TypeUint32)
	alloc := fmt.Sprintf("(call $malloc (i32.add (i32.const 12) (i32.mul %s (i32.const %d))))", capVal, elemSize)
	e.set(x.Dst, alloc)
	base := e.val(x.Dst)
	e.b.WriteString(fmt.Sprintf("    (i32.store %s (i32.const 0))\n", base))
	e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) (i32.const 0))\n", base))
	e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 8)) %s)\n", base, capVal))
}

func (e *Emitter) emitChanSend(x *hir.InstrChanSend) {
	base := e.val(x.Chan)
	tail := fmt.Sprintf("(i32.load (i32.add %s (i32.const 4)))", base)
	capVal := fmt.Sprintf("(i32.load (i32.add %s (i32.const 8)))", base)
	elemSize := chanElemSize(x.Val.Type())
	addr := fmt.Sprintf("(i32.add (i32.add %s (i32.const 12)) (i32.mul %s (i32.const %d)))", base, tail, elemSize)
	if aggregateType(x.Val.Type()) {
		e.b.WriteString(fmt.Sprintf("    (memory.copy %s %s (i32.const %d))\n", addr, e.val(x.Val), elemSize))
	} else {
		e.b.WriteString(fmt.Sprintf("    (%s %s %s)\n", memoryOp(x.Val.Type(), false), addr, e.val(x.Val)))
	}
	e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) (i32.rem_u (i32.add %s (i32.const 1)) %s))\n", base, tail, capVal))
}

func (e *Emitter) emitChanRecv(x *hir.InstrChanRecv) {
	base := e.val(x.Chan)
	head := fmt.Sprintf("(i32.load %s)", base)
	capVal := fmt.Sprintf("(i32.load (i32.add %s (i32.const 8)))", base)
	elemSize := chanElemSize(x.Dst.Type())
	addr := fmt.Sprintf("(i32.add (i32.add %s (i32.const 12)) (i32.mul %s (i32.const %d)))", base, head, elemSize)
	if aggregateType(x.Dst.Type()) {
		e.advanceSP(elemSize)
		value := fmt.Sprintf("(i32.sub (global.get $__sp) (i32.const %d))", elemSize)
		e.b.WriteString(fmt.Sprintf("    (memory.copy %s %s (i32.const %d))\n", value, addr, elemSize))
		e.set(x.Dst, value)
	} else {
		e.set(x.Dst, fmt.Sprintf("(%s %s)", memoryOp(x.Dst.Type(), true), addr))
	}
	e.b.WriteString(fmt.Sprintf("    (i32.store %s (i32.rem_u (i32.add %s (i32.const 1)) %s))\n", base, head, capVal))
	if x.OkDst != nil {
		e.set(x.OkDst, "(i32.const 1)")
	}
}

func (e *Emitter) callArg(v hir.Value) string {
	if s, ok := v.(*hir.ConstString); ok {
		size := typeSize(sema.TypeString)
		e.callArgUsed += size
		e.advanceSP(size)
		storeBase := fmt.Sprintf("(i32.sub (global.get $__sp) (i32.const %d))", size)
		remaining := e.callArgTotal - e.callArgUsed + size
		argBase := fmt.Sprintf("(i32.sub (global.get $__sp) (i32.const %d))", remaining)
		e.b.WriteString(fmt.Sprintf("    (i32.store %s %s)\n", storeBase, e.val(s)))
		e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 4)) (i32.const 0))\n", storeBase))
		e.b.WriteString(fmt.Sprintf("    (i32.store (i32.add %s (i32.const 8)) (i32.const %d))\n", storeBase, len(s.Raw)))
		return argBase
	}
	if _, ok := v.(*hir.ConstZero); ok && aggregateType(v.Type()) {
		size := typeSize(v.Type())
		e.callArgUsed += size
		e.advanceSP(size)
		remaining := e.callArgTotal - e.callArgUsed + size
		base := fmt.Sprintf("(i32.sub (global.get $__sp) (i32.const %d))", remaining)
		e.b.WriteString(fmt.Sprintf("    (memory.fill %s (i32.const 0) (i32.const %d))\n", base, size))
		return base
	}
	return e.val(v)
}

func (e *Emitter) aggregateAddress(agg hir.Value, offset, size int) string {
	if r, ok := agg.(*hir.Reg); ok && aggregateType(r.Typ) {
		return fmt.Sprintf("(i32.add (local.get $%s) (i32.const %d))", reg(r), offset)
	}
	e.advanceSP(size)
	// The stack pointer has already advanced. Reconstruct the address of the
	// allocation instead of re-evaluating the post-allocation $__sp value.
	return fmt.Sprintf("(i32.add (i32.sub (global.get $__sp) (i32.const %d)) (i32.const %d))", size, offset)
}
