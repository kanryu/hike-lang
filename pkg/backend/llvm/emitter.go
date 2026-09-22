package llvm

import (
	"fmt"
	"runtime"
	"strings"

	"hikec-go/pkg/debug"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/logger"
	"hikec-go/pkg/sema"
)

type asyncThunk struct {
	name        string
	retLLVMType string
}

type Emitter struct {
	prog            *hir.Program
	semaCtx         *sema.Context
	targetTriple    string
	b               strings.Builder
	regCount        int
	asyncThunks     map[string]*asyncThunk
	declaredSymbols map[string]bool
	userSymbols     map[string]string
	debugMgr        *debug.DebugManager
	currentFn       *hir.Function
	currentFnID     int
	panicTermID     int
	currentIsMain   bool
}

// panicLabel returns an emitter-owned label for a function's defer chain.
// HIR deliberately stores only defer order; LLVM spelling stays local to this
// backend and cannot collide with source labels.
func (e *Emitter) panicLabel(functionID, deferID int, suffix string) string {
	return fmt.Sprintf("panic.%s.%d.%d", suffix, functionID, deferID)
}

func (e *Emitter) SetVerboseLevel(level int) {
	logger.SetLevel(level)
}

func defaultTargetTriple() string {
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "arm64" {
			return "aarch64-pc-windows-msvc"
		}
		return "x86_64-pc-windows-msvc"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "arm64-apple-macosx"
		}
		return "x86_64-apple-macosx"
	default:
		if runtime.GOARCH == "arm64" {
			return "aarch64-unknown-linux-gnu"
		}
		return "x86_64-unknown-linux-gnu"
	}
}

func New(prog *hir.Program, semaCtx *sema.Context, targetTriple, sourcePath string, debugEnabled bool) *Emitter {
	if targetTriple == "" {
		targetTriple = defaultTargetTriple()
	}
	e := &Emitter{
		prog:            prog,
		semaCtx:         semaCtx,
		targetTriple:    targetTriple,
		asyncThunks:     make(map[string]*asyncThunk),
		declaredSymbols: make(map[string]bool),
		userSymbols:     make(map[string]string),
		debugMgr:        debug.NewDebugManager(sourcePath, debugEnabled),
	}

	for sym := range RuntimeLLVMSymbols {
		e.declaredSymbols[sym] = true
	}
	return e
}

func (e *Emitter) functionSymbol(name string) string {
	if symbol, ok := e.userSymbols[name]; ok {
		return symbol
	}
	return name
}

func (e *Emitter) isWindowsTarget() bool {
	t := strings.ToLower(e.targetTriple)
	return strings.Contains(t, "windows") || strings.Contains(t, "win32") || strings.Contains(t, "msvc")
}

func (e *Emitter) isWasmTarget() bool {
	t := strings.ToLower(e.targetTriple)
	return strings.Contains(t, "wasm32") || strings.Contains(t, "wasm")
}

func (e *Emitter) nextTmp() string {
	e.regCount++
	return fmt.Sprintf("%%.b%d", e.regCount)
}

func (e *Emitter) Emit() string {
	e.b.Reset()
	e.collectUserSymbols()
	e.emitPrologue()
	e.emitTypeDefs()
	e.emitConstants()
	e.emitGlobals()
	e.emitItabs()
	e.emitFunctions()
	e.emitAsyncThunks()
	if e.debugMgr.Enabled() {
		e.b.WriteString(e.debugMgr.EmitMetadata())
	}
	return e.b.String()
}

func (e *Emitter) collectUserSymbols() {
	for _, fn := range e.prog.Functions {
		if !fn.IsExtern && RuntimeLLVMSymbols[fn.Name] {
			e.userSymbols[fn.Name] = "__hike_user_" + fn.Name
		}
	}
}

func (e *Emitter) emitPrologue() {
	e.b.WriteString(fmt.Sprintf("; ModuleID = '%s'\n", e.prog.ModuleName))
	e.b.WriteString(fmt.Sprintf("source_filename = \"%s.hike\"\n", e.prog.ModuleName))
	e.b.WriteString(fmt.Sprintf("target triple = \"%s\"\n\n", e.targetTriple))
	if e.debugMgr.Enabled() {
		e.b.WriteString("declare void @llvm.dbg.declare(metadata, metadata, metadata)\n\n")
	}
	// ターゲットトリプルに応じた適切なランタイムIRを出力
	e.b.WriteString(GetRuntimeIR(e.targetTriple))
	e.b.WriteString("\n\n")
}

func (e *Emitter) programHasPanic() bool {
	for _, fn := range e.prog.Functions {
		if len(fn.PanicSites) > 0 {
			return true
		}
	}
	return false
}

func (e *Emitter) emitTypeDefs() {
	if e.semaCtx == nil {
		return
	}
	for _, st := range e.semaCtx.Structs {
		if st.IsGeneric() {
			continue
		}
		fields := make([]string, len(st.Fields))
		for i, f := range st.Fields {
			fields[i] = f.Type.LLVMType()
		}
		e.b.WriteString(fmt.Sprintf("%%struct.%s = type { %s }\n", st.Name, strings.Join(fields, ", ")))
	}
	e.b.WriteString("\n")
}

func (e *Emitter) emitConstants() {
	for _, sc := range e.prog.StringConstants {
		escaped := encodeLLVMString(sc.Raw)
		header := encodeStringHeader(uint32(sc.Length - 1))
		e.b.WriteString(fmt.Sprintf("@%s = private unnamed_addr constant [%d x i8] c\"%s%s\", align 1\n",
			sc.Label, sc.Length+8, header, escaped))
	}
	if len(e.prog.StringConstants) > 0 {
		e.b.WriteString("\n")
	}
}

func (e *Emitter) emitGlobals() {
	align := sema.PointerSize
	for _, g := range e.prog.Globals {
		storage := "global"
		if g.MemoryClass == hir.GlobalMemoryThreadable {
			storage = "thread_local global"
		}
		e.b.WriteString(fmt.Sprintf("@%s = %s %s zeroinitializer, align %d\n", g.Name, storage, g.Typ.LLVMType(), align))
		if g.MemoryClass == hir.GlobalMemoryConcurrent && llvmAtomicGlobalType(g.Typ) {
			e.b.WriteString(fmt.Sprintf("; concurrent global: %s (atomic accesses)\n", g.Name))
		}
	}
	if len(e.prog.Globals) > 0 {
		e.b.WriteString("\n")
	}
}

func llvmAtomicGlobalType(t sema.Type) bool {
	switch t.(type) {
	case *sema.BasicType, *sema.PointerType:
		return true
	default:
		return false
	}
}

func (e *Emitter) concurrentGlobal(v hir.Value) (*hir.GlobalVar, bool) {
	g, ok := v.(*hir.GlobalVar)
	if !ok || g.MemoryClass != hir.GlobalMemoryConcurrent || !llvmAtomicGlobalType(g.Typ) {
		return nil, false
	}
	return g, true
}

func (e *Emitter) emitItabs() {
	emittedTypes := make(map[string]bool)
	intLLVM := sema.TypeInt32.LLVMType()

	for _, itab := range e.prog.Itabs {
		if !emittedTypes[itab.ItabStructName] {
			methodSigs := []string{intLLVM}
			for _, m := range itab.Methods {
				retTypeStr := "void"
				if len(m.MethodType.ReturnTypes) == 1 {
					retTypeStr = m.MethodType.ReturnTypes[0].LLVMType()
				} else if len(m.MethodType.ReturnTypes) > 1 {
					types := make([]string, len(m.MethodType.ReturnTypes))
					for idx, rt := range m.MethodType.ReturnTypes {
						types[idx] = rt.LLVMType()
					}
					retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
				}
				paramTypes := []string{"i8*"}
				for _, pt := range m.MethodType.ParamTypes {
					paramTypes = append(paramTypes, pt.LLVMType())
				}
				methodSigs = append(methodSigs, fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(paramTypes, ", ")))
			}
			e.b.WriteString(fmt.Sprintf("%%struct.%s = type { %s }\n", itab.ItabStructName, strings.Join(methodSigs, ", ")))
			emittedTypes[itab.ItabStructName] = true
		}

		fieldValues := []string{fmt.Sprintf("%s %d", sema.TypeInt32.LLVMType(), itab.TypeID)}
		sName := strings.TrimPrefix(itab.ConcreteType.TypeName(), "*")

		for _, m := range itab.Methods {
			retTypeStr := "void"
			if len(m.MethodType.ReturnTypes) == 1 {
				retTypeStr = m.MethodType.ReturnTypes[0].LLVMType()
			} else if len(m.MethodType.ReturnTypes) > 1 {
				types := make([]string, len(m.MethodType.ReturnTypes))
				for idx, rt := range m.MethodType.ReturnTypes {
					types[idx] = rt.LLVMType()
				}
				retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
			}
			rawParams := []string{"i8*"}
			for _, pt := range m.MethodType.ParamTypes {
				rawParams = append(rawParams, pt.LLVMType())
			}
			rawSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(rawParams, ", "))

			// 実体関数の正確なLLVMシグネチャを取得
			var targetFn *hir.Function
			for _, fn := range e.prog.Functions {
				if fn.Name == m.TargetFnName {
					targetFn = fn
					break
				}
			}

			concreteRet := retTypeStr
			var concreteParams []string

			if targetFn != nil {
				if len(targetFn.ReturnTypes) == 1 {
					concreteRet = targetFn.ReturnTypes[0].LLVMType()
				} else if len(targetFn.ReturnTypes) > 1 {
					types := make([]string, len(targetFn.ReturnTypes))
					for idx, rt := range targetFn.ReturnTypes {
						types[idx] = rt.LLVMType()
					}
					concreteRet = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
				}
				for _, p := range targetFn.Params {
					concreteParams = append(concreteParams, p.Typ.LLVMType())
				}
			} else {
				concreteRecv := fmt.Sprintf("%%struct.%s*", sName)
				if e.semaCtx != nil {
					if st, _ := e.semaCtx.LookupStruct(sName); st == nil {
						concreteRecv = itab.ConcreteType.LLVMType()
					}
				}
				concreteParams = []string{concreteRecv}
				for _, pt := range m.MethodType.ParamTypes {
					concreteParams = append(concreteParams, pt.LLVMType())
				}
			}

			concreteSig := fmt.Sprintf("%s (%s)*", concreteRet, strings.Join(concreteParams, ", "))
			if concreteSig == rawSig {
				fieldValues = append(fieldValues, fmt.Sprintf("%s @%s", rawSig, e.functionSymbol(m.TargetFnName)))
			} else {
				fieldValues = append(fieldValues, fmt.Sprintf("%s bitcast (%s @%s to %s)", rawSig, concreteSig, e.functionSymbol(m.TargetFnName), rawSig))
			}
		}

		e.b.WriteString(fmt.Sprintf("@%s = constant %%struct.%s { %s }\n",
			itab.GlobalName, itab.ItabStructName, strings.Join(fieldValues, ", ")))
	}

	if len(e.prog.Itabs) > 0 {
		e.b.WriteString("\n")
	}
}

func (e *Emitter) emitFunctions() {
	referencedExterns := make(map[string]bool)
	for _, fn := range e.prog.Functions {
		for _, bb := range e.blocksForEmission(fn) {
			for _, inst := range bb.Instructions {
				if call, ok := inst.(*hir.InstrCallStatic); ok {
					if llvmIntrinsicName(call.CalleeName) == "" {
						referencedExterns[call.CalleeName] = true
					}
				}
			}
		}
	}

	// itab定数から参照されている関数も外部宣言対象に登録
	for _, itab := range e.prog.Itabs {
		for _, m := range itab.Methods {
			referencedExterns[m.TargetFnName] = true
		}
	}

	for fnID, fn := range e.prog.Functions {
		if fn.IsExtern {
			if llvmIntrinsicName(fn.Name) != "" {
				continue
			}
			if e.declaredSymbols[fn.Name] {
				continue
			}

			if fn.IsCFunc && fn.CFuncTarget != "" && !referencedExterns[fn.Name] && !referencedExterns[fn.CFuncTarget] {
				continue
			}

			e.declaredSymbols[fn.Name] = true

			retTypeStr := "void"
			if len(fn.ReturnTypes) == 1 {
				retTypeStr = e.externalABIType(fn.ReturnTypes[0])
			} else if len(fn.ReturnTypes) > 1 {
				types := make([]string, len(fn.ReturnTypes))
				for i, rt := range fn.ReturnTypes {
					types[i] = rt.LLVMType()
				}
				retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
			}

			paramTypes := make([]string, len(fn.Params))
			for i, p := range fn.Params {
				paramTypes[i] = e.externalABIType(p.Typ)
			}
			if fn.IsVariadic {
				paramTypes = append(paramTypes, "...")
			}
			e.b.WriteString(fmt.Sprintf("declare %s @%s(%s)\n", retTypeStr, fn.Name, strings.Join(paramTypes, ", ")))
			continue
		}

		e.declaredSymbols[fn.Name] = true
		e.emitFunction(fn, fnID)
	}
}

// blocksForEmission is the single CFG view used by LLVM-side prepasses. For
// structured functions it performs the same HIR control lowering as
// emitFunction, so extern discovery uses the same temporary CFG.
func (e *Emitter) blocksForEmission(fn *hir.Function) []*basicBlock {
	if fn == nil {
		return nil
	}
	blocks, err := lowerStructuredBody(fn)
	if err != nil {
		logger.LogVerbose2("[Verbose2] structured HIR prepass fallback for @%s: %v\n", fn.Name, err)
		return nil
	}
	return blocks
}

// External functions use the C representation for strings. Hike functions
// keep the length-aware pair internally, while extern/cfunc declarations cross
// the ABI as a single NUL-terminated pointer.
func (e *Emitter) externalABIType(t sema.Type) string {
	if t == sema.TypeString {
		return sema.TypeCString.LLVMType()
	}
	return t.LLVMType()
}

// llvmIntrinsicName maps the portable scalar math operations exposed by
// std/math to LLVM intrinsics. LLVM lowers these to the best target-native
// instruction or runtime sequence without requiring a libc symbol.
func llvmIntrinsicName(name string) string {
	switch name {
	case "sqrt":
		return "llvm.sqrt.f64"
	case "fabs":
		return "llvm.fabs.f64"
	case "floor":
		return "llvm.floor.f64"
	case "ceil":
		return "llvm.ceil.f64"
	case "rdrand32":
		return "llvm.x86.rdrand.32"
	case "rdrand64":
		return "llvm.x86.rdrand.64"
	case "rdseed32":
		return "llvm.x86.rdseed.32"
	case "rdseed64":
		return "llvm.x86.rdseed.64"
	case "aesenc":
		return "llvm.x86.aesni.aesenc"
	case "aesenclast":
		return "llvm.x86.aesni.aesenclast"
	case "aesdec":
		return "llvm.x86.aesni.aesdec"
	case "aesdeclast":
		return "llvm.x86.aesni.aesdeclast"
	case "pclmulqdq":
		return "llvm.x86.pclmulqdq"
	case "sha256rnds2":
		return "llvm.x86.sha256rnds2"
	case "sha256msg1":
		return "llvm.x86.sha256msg1"
	case "sha256msg2":
		return "llvm.x86.sha256msg2"
	case "llvm.trap":
		return "llvm.trap"
	default:
		return ""
	}
}

func llvmIntrinsicFeature(name string) string {
	switch name {
	case "rdrand32", "rdrand64":
		return "+rdrnd"
	case "rdseed32", "rdseed64":
		return "+rdseed"
	case "aesenc", "aesenclast", "aesdec", "aesdeclast":
		return "+aes"
	case "pclmulqdq":
		return "+pclmul"
	case "sha256rnds2", "sha256msg1", "sha256msg2":
		return "+sha"
	default:
		return ""
	}
}

func (e *Emitter) intrinsicFeatures(fn *hir.Function) string {
	features := make(map[string]bool)
	for _, bb := range e.blocksForEmission(fn) {
		for _, inst := range bb.Instructions {
			if call, ok := inst.(*hir.InstrCallStatic); ok {
				if feature := llvmIntrinsicFeature(call.CalleeName); feature != "" {
					features[feature] = true
				}
			}
		}
	}
	if len(features) == 0 {
		return ""
	}
	ordered := []string{"+aes", "+pclmul", "+rdrnd", "+rdseed", "+sha"}
	selected := make([]string, 0, len(features))
	for _, feature := range ordered {
		if features[feature] {
			selected = append(selected, feature)
		}
	}
	return strings.Join(selected, ",")
}

func (e *Emitter) emitFunction(fn *hir.Function, functionID int) {
	blocks := e.blocksForEmission(fn)
	logger.LogVerbose2("[Verbose2] --- Emit Function: @%s (blocks=%d) ---\n", fn.Name, len(blocks))
	isMain := (fn.Name == "main")
	retTypeStr := "void"
	if isMain {
		retTypeStr = "i32"
	} else if len(fn.ReturnTypes) == 1 {
		retTypeStr = fn.ReturnTypes[0].LLVMType()
	} else if len(fn.ReturnTypes) > 1 {
		types := make([]string, len(fn.ReturnTypes))
		for i, rt := range fn.ReturnTypes {
			types[i] = rt.LLVMType()
		}
		retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
	}

	params := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		params[i] = fmt.Sprintf("%s %s", p.Typ.LLVMType(), p)
	}
	if fn.IsVariadic {
		params = append(params, "...")
	}

	storageClass := ""
	if fn.IsCFunc && e.isWindowsTarget() {
		storageClass = "dllexport "
	}

	featureAttr := ""
	if features := e.intrinsicFeatures(fn); features != "" {
		featureAttr = fmt.Sprintf(" \"target-features\"=\"%s\"", features)
	}
	debugTag := ""
	if e.debugMgr.Enabled() {
		spID := e.debugMgr.StartFunction(fn.Name, fn.Location.Line)
		debugTag = fmt.Sprintf(" !dbg !%d", spID)
	}
	e.b.WriteString(fmt.Sprintf("define %s%s @%s(%s)%s%s {\n", storageClass, retTypeStr, e.functionSymbol(fn.Name), strings.Join(params, ", "), featureAttr, debugTag))
	e.currentFn = fn
	e.currentFnID = functionID
	e.currentIsMain = isMain
	e.panicTermID = 0
	for _, bb := range blocks {
		logger.LogVerbose2("[Verbose2]   Block: %s (insts=%d)\n", bb.Label, len(bb.Instructions))
		e.b.WriteString(fmt.Sprintf("%s:\n", bb.Label))
		for _, inst := range bb.Instructions {
			e.emitInstruction(inst)
		}
		if bb.Terminator != nil {
			e.emitTerminator(bb.Terminator, isMain)
		} else {
			e.emitDefaultReturn(fn, isMain)
		}
	}
	e.b.WriteString("}\n\n")
	e.currentFn = nil
}

func (e *Emitter) emitDefaultReturn(fn *hir.Function, isMain bool) {
	if isMain {
		e.b.WriteString("  ret i32 0\n")
		return
	}
	if len(fn.ReturnTypes) == 0 {
		e.b.WriteString("  ret void\n")
		return
	}
	if len(fn.ReturnTypes) > 1 {
		types := make([]string, len(fn.ReturnTypes))
		for i, typ := range fn.ReturnTypes {
			types[i] = typ.LLVMType()
		}
		e.b.WriteString(fmt.Sprintf("  ret { %s } zeroinitializer\n", strings.Join(types, ", ")))
		return
	}
	typ := fn.ReturnTypes[0].LLVMType()
	zero := "0"
	if strings.HasPrefix(typ, "i") {
		zero = "0"
	} else if strings.HasPrefix(typ, "f") {
		zero = "0.0"
	} else if strings.HasSuffix(typ, "*") {
		zero = "null"
	} else {
		zero = "zeroinitializer"
	}
	e.b.WriteString(fmt.Sprintf("  ret %s %s\n", typ, zero))
}

func (e *Emitter) isVariadicFunc(name string) (bool, string) {
	var fnType *sema.FuncType
	if e.semaCtx != nil {
		fnType, _ = e.semaCtx.LookupFunction(name)
	}
	if fnType != nil && fnType.IsVariadic {
		paramTypes := make([]string, len(fnType.ParamTypes))
		for idx, pt := range fnType.ParamTypes {
			if fnType.IsExtern {
				paramTypes[idx] = e.externalABIType(pt)
			} else {
				paramTypes[idx] = pt.LLVMType()
			}
		}
		paramTypes = append(paramTypes, "...")
		return true, fmt.Sprintf("(%s)", strings.Join(paramTypes, ", "))
	}
	return false, ""
}

func (e *Emitter) getRetSize(retTypes []sema.Type) int64 {
	if len(retTypes) == 0 {
		return 0
	}
	if len(retTypes) == 1 {
		sz := int64(retTypes[0].Size())
		if sz <= 0 {
			sz = int64(sema.PointerSize)
		}
		return sz
	}
	sz := int64(0)
	for _, rt := range retTypes {
		s := int64(rt.Size())
		if s <= 0 {
			s = int64(sema.PointerSize)
		}
		sz += s
	}
	return sz
}

func (e *Emitter) getRetLLVMType(retTypes []sema.Type) string {
	if len(retTypes) == 0 {
		return "void"
	}
	if len(retTypes) == 1 {
		return retTypes[0].LLVMType()
	}
	types := make([]string, len(retTypes))
	for i, rt := range retTypes {
		types[i] = rt.LLVMType()
	}
	return fmt.Sprintf("{ %s }", strings.Join(types, ", "))
}

func (e *Emitter) getOrCreateAsyncThunk(retLLVMType string) string {
	if thunk, ok := e.asyncThunks[retLLVMType]; ok {
		return thunk.name
	}
	name := fmt.Sprintf("__hike_async_thunk_%d", len(e.asyncThunks)+1)
	e.asyncThunks[retLLVMType] = &asyncThunk{
		name:        name,
		retLLVMType: retLLVMType,
	}
	return name
}

func (e *Emitter) emitAsyncThunks() {
	if len(e.asyncThunks) == 0 {
		return
	}
	e.b.WriteString("; --- Async Worker Thunks ---\n")
	for _, thunk := range e.asyncThunks {
		e.b.WriteString(fmt.Sprintf("define internal void @%s(i8* %%wrapper_env, i8* %%buf) {\n", thunk.name))
		e.b.WriteString("entry:\n")
		e.b.WriteString("  %env_arr = bitcast i8* %wrapper_env to i8**\n")
		e.b.WriteString("  %p_fn = getelementptr inbounds i8*, i8** %env_arr, i32 0\n")
		e.b.WriteString("  %fn_raw = load i8*, i8** %p_fn\n")
		e.b.WriteString("  %p_env = getelementptr inbounds i8*, i8** %env_arr, i32 1\n")
		e.b.WriteString("  %real_env = load i8*, i8** %p_env\n")
		e.b.WriteString("  %cond = icmp eq i8* %real_env, null\n")
		e.b.WriteString("  br i1 %cond, label %call_plain, label %call_closure\n\n")

		e.b.WriteString("call_plain:\n")
		if thunk.retLLVMType == "void" {
			e.b.WriteString("  %plain_fn_v = bitcast i8* %fn_raw to void ()*\n")
			e.b.WriteString("  call void %plain_fn_v()\n")
			e.b.WriteString("  br label %cleanup\n\n")
		} else {
			e.b.WriteString(fmt.Sprintf("  %%plain_fn = bitcast i8* %%fn_raw to %s ()*\n", thunk.retLLVMType))
			e.b.WriteString(fmt.Sprintf("  %%res_plain = call %s %%plain_fn()\n", thunk.retLLVMType))
			e.b.WriteString("  br label %store_res\n\n")
		}

		e.b.WriteString("call_closure:\n")
		if thunk.retLLVMType == "void" {
			e.b.WriteString("  %closure_fn_v = bitcast i8* %fn_raw to void (i8*)*\n")
			e.b.WriteString("  call void %closure_fn_v(i8* %real_env)\n")
			e.b.WriteString("  br label %cleanup\n\n")
		} else {
			e.b.WriteString(fmt.Sprintf("  %%closure_fn = bitcast i8* %%fn_raw to %s (i8*)*\n", thunk.retLLVMType))
			e.b.WriteString(fmt.Sprintf("  %%res_closure = call %s %%closure_fn(i8* %%real_env)\n", thunk.retLLVMType))
			e.b.WriteString("  br label %store_res\n\n")

			e.b.WriteString("store_res:\n")
			e.b.WriteString(fmt.Sprintf("  %%res = phi %s [ %%res_plain, %%call_plain ], [ %%res_closure, %%call_closure ]\n", thunk.retLLVMType))
			e.b.WriteString(fmt.Sprintf("  %%typed_buf = bitcast i8* %%buf to %s*\n", thunk.retLLVMType))
			e.b.WriteString(fmt.Sprintf("  store %s %%res, %s* %%typed_buf\n", thunk.retLLVMType, thunk.retLLVMType))
			e.b.WriteString("  br label %cleanup\n\n")
		}

		e.b.WriteString("cleanup:\n")
		e.b.WriteString("  call void @free(i8* %wrapper_env)\n")
		e.b.WriteString("  ret void\n")
		e.b.WriteString("}\n\n")
	}
}

func (e *Emitter) emitInstruction(inst hir.Instruction) {
	if inst == nil {
		logger.LogVerbose2("[Verbose2] emitInstruction: <nil>\n")
		return
	}
	start := e.b.Len()
	e.emitInstructionBody(inst)
	e.appendDebugLocation(start, inst)
}

// emitInstructionBody emits the LLVM generated for one HIR instruction. The
// wrapper above attaches the source location to the final generated LLVM
// instruction, including instructions which expand to several LLVM lines.
func (e *Emitter) emitAsync(i *hir.InstrAsync, intLLVM string) {
	retLLVM := e.getRetLLVMType(i.RetTypes)
	retSize := e.getRetSize(i.RetTypes)
	thunkName := e.getOrCreateAsyncThunk(retLLVM)

	envSize := sema.PointerSize * 2
	rawEnv := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(%s %d)\n", rawEnv, intLLVM, envSize))
	arrEnv := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i8**\n", arrEnv, rawEnv))

	pFn := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8*, i8** %s, i32 0\n", pFn, arrEnv))
	fnVal := e.formatVal(i.FnPtr)
	if i.FnPtr != nil && i.FnPtr.Type() != nil && i.FnPtr.Type().LLVMType() != "i8*" {
		castFn := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castFn, i.FnPtr.Type().LLVMType(), fnVal))
		fnVal = castFn
	}
	e.b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", fnVal, pFn))

	pEnv := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8*, i8** %s, i32 1\n", pEnv, arrEnv))
	envVal := "null"
	if i.EnvPtr != nil {
		envVal = e.formatVal(i.EnvPtr)
		if i.EnvPtr.Type() != nil && i.EnvPtr.Type().LLVMType() != "i8*" {
			castEnv := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castEnv, i.EnvPtr.Type().LLVMType(), envVal))
			envVal = castEnv
		}
	}
	e.b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", envVal, pEnv))

	thunkPtr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast void (i8*, i8*)* @%s to i8*\n", thunkPtr, thunkName))
	taskPtr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = call %%struct.__hike_task* @__hike_async(i8* %s, i8* %s, %s %d)\n",
		taskPtr, thunkPtr, rawEnv, intLLVM, retSize))
	e.b.WriteString(fmt.Sprintf("  %s = bitcast %%struct.__hike_task* %s to i8*\n", i.Dst, taskPtr))
}

func (e *Emitter) emitCallStatic(i *hir.InstrCallStatic) {
	calleeName := i.CalleeName
	if intrinsic := llvmIntrinsicName(calleeName); intrinsic != "" {
		calleeName = intrinsic
	} else {
		calleeName = e.functionSymbol(calleeName)
	}
	args := make([]string, len(i.Args))
	for idx, a := range i.Args {
		if a == nil {
			logger.LogVerbose2("[Verbose2] WARNING: InstrCallStatic '%s' arg[%d] is nil!\n", i.CalleeName, idx)
			args[idx] = "i8* null"
			continue
		}
		if a.Type() == nil {
			logger.LogVerbose2("[Verbose2] WARNING: InstrCallStatic '%s' arg[%d] has nil Type()! Val=%v\n", i.CalleeName, idx, a)
			args[idx] = fmt.Sprintf("i64 %s", e.formatVal(a))
			continue
		}
		args[idx] = fmt.Sprintf("%s %s", a.Type().LLVMType(), e.formatVal(a))
	}

	isVar, varSig := e.isVariadicFunc(i.CalleeName)
	if isVar {
		if i.Dst != nil {
			e.b.WriteString(fmt.Sprintf("  %s = call %s %s @%s(%s)\n",
				i.Dst, i.Dst.Typ.LLVMType(), varSig, calleeName, strings.Join(args, ", ")))
		} else {
			e.b.WriteString(fmt.Sprintf("  call void %s @%s(%s)\n",
				varSig, calleeName, strings.Join(args, ", ")))
		}
	} else {
		if i.Dst != nil {
			e.b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n",
				i.Dst, i.Dst.Typ.LLVMType(), calleeName, strings.Join(args, ", ")))
		} else {
			e.b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", calleeName, strings.Join(args, ", ")))
		}
	}
}

func (e *Emitter) emitGetElemPtr(i *hir.InstrGetElemPtr, intLLVM string) {
	baseType := i.BasePtr.Type()
	idxLLVM := intLLVM
	if i.Index != nil && i.Index.Type() != nil {
		idxLLVM = i.Index.Type().LLVMType()
	}

	if pt, ok := baseType.(*sema.PointerType); ok {
		if ar, okArr := pt.Base.(*sema.ArrayType); okArr {
			e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s %s, i32 0, %s %s\n",
				i.Dst, ar.LLVMType(), baseType.LLVMType(), e.formatVal(i.BasePtr), idxLLVM, e.formatVal(i.Index)))
			return
		}
	}

	var elemLLVM string
	if pt, ok := i.Dst.Typ.(*sema.PointerType); ok {
		elemLLVM = pt.Base.LLVMType()
	} else if strings.HasSuffix(i.Dst.Typ.LLVMType(), "*") {
		elemLLVM = strings.TrimSuffix(i.Dst.Typ.LLVMType(), "*")
	} else {
		elemLLVM = "i8"
	}

	baseVal := e.formatVal(i.BasePtr)
	expectedBaseType := elemLLVM + "*"
	if baseType != nil && baseType.LLVMType() != expectedBaseType {
		castBase := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n",
			castBase, baseType.LLVMType(), baseVal, expectedBaseType))
		baseVal = castBase
	}

	e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s %s, %s %s\n",
		i.Dst, elemLLVM, expectedBaseType, baseVal, idxLLVM, e.formatVal(i.Index)))
}

func (e *Emitter) emitInstructionBody(inst hir.Instruction) {
	if inst == nil {
		logger.LogVerbose2("[Verbose2] emitInstruction: <nil>\n")
		return
	}
	logger.LogVerbose2("[Verbose2] emitInstruction: %T -> %+v\n", inst, inst)

	intLLVM := sema.TypeInt.LLVMType()

	switch i := inst.(type) {
	case *hir.InstrRegionBegin:
		name := "__hike_region_begin"
		if e.isWasmTarget() {
			name += "32"
		}
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @%s()\n", i.Dst, name))

	case *hir.InstrRegionAlloc:
		sizeLLVM := intLLVM
		if e.isWasmTarget() {
			sizeLLVM = "i32"
		}
		name := "__hike_region_alloc"
		if e.isWasmTarget() {
			name += "32"
		}
		raw := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @%s(i8* %s, %s %s)\n", raw, name, e.formatVal(i.Region), sizeLLVM, e.formatVal(i.Size)))
		e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", i.Dst, raw, i.AllocType.LLVMType()))

	case *hir.InstrRegionEnd:
		name := "__hike_region_end"
		if e.isWasmTarget() {
			name += "32"
		}
		e.b.WriteString(fmt.Sprintf("  call void @%s(i8* %s)\n", name, e.formatVal(i.Region)))

	case *hir.InstrAreaBegin:
		name := "__hike_area_begin"
		if e.isWasmTarget() {
			name += "32"
		}
		sizeType := intLLVM
		if e.isWasmTarget() {
			sizeType = "i32"
		}
		size := fmt.Sprintf("%s 0", sizeType)
		if i.Size != nil {
			size = fmt.Sprintf("%s %s", sizeType, e.formatVal(i.Size))
		}
		parent := "i8* null"
		if i.Parent != nil {
			parent = fmt.Sprintf("i8* %s", e.formatVal(i.Parent))
		}
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @%s(%s, %s)\n", i.Dst, name, size, parent))

	case *hir.InstrAreaAlloc:
		name := "__hike_area_alloc"
		if e.isWasmTarget() {
			name += "32"
		}
		sizeType := intLLVM
		if e.isWasmTarget() {
			sizeType = "i32"
		}
		raw := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @%s(i8* %s, %s %s)\n", raw, name, e.formatVal(i.Area), sizeType, e.formatVal(i.Size)))
		e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", i.Dst, raw, i.AllocType.LLVMType()))

	case *hir.InstrAreaEnd:
		name := "__hike_area_end"
		if e.isWasmTarget() {
			name += "32"
		}
		e.b.WriteString(fmt.Sprintf("  call void @%s(i8* %s)\n", name, e.formatVal(i.Area)))

	case *hir.InstrAlloca:
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s\n", i.Dst, i.AllocType.LLVMType()))
		e.emitLocalVariableDebug(i.Dst, i.AllocType, e.prog.InstructionLocations[hir.InstructionKey(inst)])

	case *hir.InstrAllocaDynamic:
		sizeLLVM := intLLVM
		if i.Size != nil && i.Size.Type() != nil {
			sizeLLVM = i.Size.Type().LLVMType()
		}
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s, %s %s, align %d\n",
			i.Dst, i.AllocType.LLVMType(), sizeLLVM, e.formatVal(i.Size), sema.PointerSize))
		e.emitLocalVariableDebug(i.Dst, i.AllocType, e.prog.InstructionLocations[hir.InstructionKey(inst)])

	case *hir.InstrHeapAlloc:
		sizeLLVM := intLLVM
		if i.Size != nil && i.Size.Type() != nil {
			sizeLLVM = i.Size.Type().LLVMType()
		}
		rawPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(%s %s)\n", rawPtr, sizeLLVM, e.formatVal(i.Size)))
		e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", i.Dst, rawPtr, i.AllocType.LLVMType()))
		e.emitLocalVariableDebug(i.Dst, i.AllocType, e.prog.InstructionLocations[hir.InstructionKey(inst)])

	case *hir.InstrLoad:
		if i.Ptr == nil {
			logger.LogVerbose2("[Verbose2] ERROR: InstrLoad has nil Ptr! Dst=%v\n", i.Dst)
			return
		}
		ptrVal := e.formatVal(i.Ptr)
		expectedPtrType := i.Dst.Typ.LLVMType() + "*"
		if i.Ptr.Type() != nil && i.Ptr.Type().LLVMType() != expectedPtrType {
			castPtr := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", castPtr, i.Ptr.Type().LLVMType(), ptrVal, expectedPtrType))
			ptrVal = castPtr
		}
		if _, atomic := e.concurrentGlobal(i.Ptr); atomic {
			e.b.WriteString(fmt.Sprintf("  %s = load atomic %s, %s %s unordered, align %d\n", i.Dst, i.Dst.Typ.LLVMType(), expectedPtrType, ptrVal, sema.PointerSize))
		} else {
			e.b.WriteString(fmt.Sprintf("  %s = load %s, %s %s\n", i.Dst, i.Dst.Typ.LLVMType(), expectedPtrType, ptrVal))
		}

	case *hir.InstrStore:
		if i.Ptr == nil || i.Val == nil {
			logger.LogVerbose2("[Verbose2] ERROR: InstrStore has nil Ptr or Val! (Ptr=%v, Val=%v)\n", i.Ptr, i.Val)
			return
		}
		ptrVal := e.formatVal(i.Ptr)
		expectedPtrType := i.Val.Type().LLVMType() + "*"
		if i.Ptr.Type() != nil && i.Ptr.Type().LLVMType() != expectedPtrType {
			castPtr := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", castPtr, i.Ptr.Type().LLVMType(), ptrVal, expectedPtrType))
			ptrVal = castPtr
		}
		if _, atomic := e.concurrentGlobal(i.Ptr); atomic {
			e.b.WriteString(fmt.Sprintf("  store atomic %s %s, %s %s unordered, align %d\n", i.Val.Type().LLVMType(), e.formatVal(i.Val), expectedPtrType, ptrVal, sema.PointerSize))
		} else {
			e.b.WriteString(fmt.Sprintf("  store %s %s, %s %s\n",
				i.Val.Type().LLVMType(), e.formatVal(i.Val),
				expectedPtrType, ptrVal))
		}
	case *hir.InstrLock:
		e.b.WriteString("  call void @__hike_lock()\n")
	case *hir.InstrUnlock:
		e.b.WriteString("  call void @__hike_unlock()\n")

	case *hir.InstrBinary:
		e.emitBinary(i)

	case *hir.InstrUnary:
		e.emitUnary(i)

	case *hir.InstrCast:
		e.emitCast(i)

	case *hir.InstrBoxInterface:
		e.emitBoxInterface(i)

	case *hir.InstrGetFieldPtr:
		var stName string
		if pt, ok := i.BasePtr.Type().(*sema.PointerType); ok {
			if st, okSt := pt.Base.(*sema.StructType); okSt {
				stName = st.Name
			}
		}
		if stName == "" {
			typeName := i.BasePtr.Type().TypeName()
			stName = strings.TrimPrefix(strings.TrimPrefix(typeName, "*"), "%struct.")
		}

		baseVal := e.formatVal(i.BasePtr)
		expectedBaseType := fmt.Sprintf("%%struct.%s*", stName)
		if i.BasePtr.Type() != nil && i.BasePtr.Type().LLVMType() != expectedBaseType {
			castBase := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n",
				castBase, i.BasePtr.Type().LLVMType(), baseVal, expectedBaseType))
			baseVal = castBase
		}

		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %s %s, i32 0, i32 %d\n",
			i.Dst, stName, expectedBaseType, baseVal, i.FieldIndex))

	case *hir.InstrGetElemPtr:
		e.emitGetElemPtr(i, intLLVM)

	case *hir.InstrCallStatic:
		e.emitCallStatic(i)

	case *hir.InstrInlineAsm:
		if e.isWasmTarget() {
			panic("inline assembly is not supported for WebAssembly targets")
		}
		lowerTemplate := strings.ToLower(i.Template)
		if strings.Contains(lowerTemplate, "aes") && !strings.Contains(strings.ToLower(e.targetTriple), "x86_64") {
			panic("AES inline assembly requires an x86_64 target and a matching build constraint")
		}
		constraints := i.OutputConstraints
		if constraints != "" && i.InputConstraints != "" {
			constraints += ","
		}
		constraints += i.InputConstraints
		if constraints != "" && i.ClobberConstraints != "" {
			constraints += ","
		}
		constraints += i.ClobberConstraints
		args := make([]string, len(i.Args))
		for idx, arg := range i.Args {
			args[idx] = fmt.Sprintf("%s %s", arg.Type().LLVMType(), e.formatVal(arg))
		}
		e.b.WriteString(fmt.Sprintf("  call void asm sideeffect \"%s\", \"%s\"(%s)\n", encodeLLVMAsmString(normalizeInlineAsmTemplate(i.Template)), encodeLLVMAsmString(constraints), strings.Join(args, ", ")))

	case *hir.InstrCallIndirect:
		e.emitCallIndirect(i)

	case *hir.InstrCallIface:
		e.emitCallIface(i)

	case *hir.InstrAsync:
		e.emitAsync(i, intLLVM)

	case *hir.InstrTaskWait:
		taskPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%struct.__hike_task*\n", taskPtr, e.formatVal(i.Task)))
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @__hike_task_wait(%%struct.__hike_task* %s)\n", i.Dst, taskPtr))

	case *hir.InstrChanMake:
		elemSize := int64(i.ElemType.Size())
		if elemSize <= 0 {
			elemSize = int64(sema.PointerSize)
		}
		capVal := e.formatVal(i.Cap)
		capLLVM := intLLVM
		if i.Cap != nil && i.Cap.Type() != nil {
			capLLVM = i.Cap.Type().LLVMType()
		}
		if i.Cap == nil {
			capVal = "0"
		}
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @__hike_chan_make(%s %d, %s %s)\n",
			i.Dst, intLLVM, elemSize, capLLVM, capVal))

	case *hir.InstrChanSend:
		valLLVM := i.Val.Type().LLVMType()
		valVal := e.formatVal(i.Val)

		tmpAlloca := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s\n", tmpAlloca, valLLVM))
		e.b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", valLLVM, valVal, valLLVM, tmpAlloca))
		rawPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", rawPtr, valLLVM, tmpAlloca))

		chVal := e.formatVal(i.Chan)
		if i.Chan.Type() != nil && i.Chan.Type().LLVMType() != "i8*" {
			castCh := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castCh, i.Chan.Type().LLVMType(), chVal))
			chVal = castCh
		}
		e.b.WriteString(fmt.Sprintf("  call void @__hike_chan_send(i8* %s, i8* %s)\n", chVal, rawPtr))

	case *hir.InstrChanRecv:
		elemLLVM := i.Dst.Typ.LLVMType()
		tmpAlloca := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s\n", tmpAlloca, elemLLVM))
		rawPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", rawPtr, elemLLVM, tmpAlloca))

		chVal := e.formatVal(i.Chan)
		if i.Chan.Type() != nil && i.Chan.Type().LLVMType() != "i8*" {
			castCh := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castCh, i.Chan.Type().LLVMType(), chVal))
			chVal = castCh
		}
		e.b.WriteString(fmt.Sprintf("  call void @__hike_chan_recv(i8* %s, i8* %s)\n", chVal, rawPtr))
		e.b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", i.Dst, elemLLVM, elemLLVM, tmpAlloca))

	case *hir.InstrChanClose:
		chVal := e.formatVal(i.Chan)
		if i.Chan.Type() != nil && i.Chan.Type().LLVMType() != "i8*" {
			castCh := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castCh, i.Chan.Type().LLVMType(), chVal))
			chVal = castCh
		}
		e.b.WriteString(fmt.Sprintf("  call void @__hike_chan_close(i8* %s)\n", chVal))

	case *hir.InstrExtractValue:
		e.b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, %d\n",
			i.Dst, i.Agg.Type().LLVMType(), e.formatVal(i.Agg), i.Index))

	case *hir.InstrInsertValue:
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, %d\n",
			i.Dst, i.Agg.Type().LLVMType(), e.formatVal(i.Agg),
			i.Val.Type().LLVMType(), e.formatVal(i.Val), i.Index))
	}
}

// emitLocalVariableDebug mirrors the old LLVM code generator's
// llvm.dbg.declare emission. HIR alloca registers retain the source variable
// name, so they can be associated with a DILocalVariable without re-reading
// the AST in the backend.
func (e *Emitter) emitLocalVariableDebug(reg *hir.Reg, typ sema.Type, loc hir.SourceLocation) {
	if !e.debugMgr.Enabled() || reg == nil || reg.Name == "" {
		return
	}
	if loc.Line <= 0 {
		return
	}
	varID, _ := e.debugMgr.RegisterLocalVariable(sourceVariableName(reg.Name), loc.Line, loc.Column, typ, false, 0)
	if varID == 0 {
		return
	}
	// The first operand is the pointer produced by alloca, not the allocated
	// value type. Using the register type also keeps this valid with LLVM's
	// opaque-pointer mode (where the operand is `ptr`).
	e.b.WriteString(fmt.Sprintf("  call void @llvm.dbg.declare(metadata %s %s, metadata !%d, metadata !DIExpression())\n", reg.Type().LLVMType(), reg, varID))
}

func sourceVariableName(name string) string {
	if dot := strings.LastIndexByte(name, '.'); dot > 0 && dot+1 < len(name) {
		for _, r := range name[dot+1:] {
			if r < '0' || r > '9' {
				return name
			}
		}
		return name[:dot]
	}
	return name
}

func (e *Emitter) appendDebugLocation(start int, inst hir.Instruction) {
	if !e.debugMgr.Enabled() || e.prog == nil || e.prog.InstructionLocations == nil || e.b.Len() <= start {
		return
	}
	loc, ok := e.prog.InstructionLocations[hir.InstructionKey(inst)]
	if !ok {
		return
	}
	tag := e.debugMgr.GetLocationTag(loc.Line, loc.Column)
	if tag == "" {
		return
	}
	text := e.b.String()
	end := len(text)
	if end > 0 && text[end-1] == '\n' {
		end--
	}
	e.b.Reset()
	e.b.WriteString(text[:end])
	e.b.WriteString(tag)
	e.b.WriteByte('\n')
	if end+1 < len(text) {
		e.b.WriteString(text[end+1:])
	}
}

func (e *Emitter) emitBinary(i *hir.InstrBinary) {
	typ := i.L.Type()
	isFloat := (typ == sema.TypeFloat64 || typ == sema.TypeFloat32)
	llvmT := typ.LLVMType()
	lVal := e.formatVal(i.L)
	rVal := e.formatVal(i.R)

	if i.Op == hir.OpShl || i.Op == hir.OpShr {
		if isFloat || !isShiftIntegerType(typ) {
			panic(fmt.Sprintf("[Emitter Panic] shift requires an integer operand, got '%s'", llvmT))
		}
		if i.R == nil || !isShiftIntegerType(i.R.Type()) {
			panic(fmt.Sprintf("[Emitter Panic] shift count requires an integer operand, got '%s'", i.R.Type().LLVMType()))
		}
	}

	if isFloat {
		var opStr string
		switch i.Op {
		case hir.OpAdd:
			opStr = "fadd"
		case hir.OpSub:
			opStr = "fsub"
		case hir.OpMul:
			opStr = "fmul"
		case hir.OpDiv:
			opStr = "fdiv"
		case hir.OpEq:
			opStr = "fcmp oeq"
		case hir.OpNeq:
			opStr = "fcmp one"
		case hir.OpLt:
			opStr = "fcmp olt"
		case hir.OpLe:
			opStr = "fcmp ole"
		case hir.OpGt:
			opStr = "fcmp ogt"
		case hir.OpGe:
			opStr = "fcmp oge"
		}
		e.b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", i.Dst, opStr, llvmT, lVal, rVal))
		return
	}

	// LLVM has separate arithmetic right shift (ashr) and logical right shift
	// (lshr) instructions.  Keep the signedness of the Hike integer type when
	// selecting the instruction; treating every integer as signed corrupts
	// high-bit values of uint/byte/uintptr.
	isUnsigned := false
	if basic, ok := typ.(*sema.BasicType); ok {
		switch basic {
		case sema.TypeUint, sema.TypeUint64, sema.TypeUint32, sema.TypeUint16, sema.TypeUint8, sema.TypeUintptr, sema.TypeByte:
			isUnsigned = true
		}
	}

	var opStr string
	switch i.Op {
	case hir.OpAdd:
		opStr = "add"
	case hir.OpSub:
		opStr = "sub"
	case hir.OpMul:
		opStr = "mul"
	case hir.OpDiv:
		opStr = "sdiv"
	case hir.OpRem:
		opStr = "srem"
	case hir.OpAnd:
		opStr = "and"
	case hir.OpOr:
		opStr = "or"
	case hir.OpXor:
		opStr = "xor"
	case hir.OpShl:
		opStr = "shl"
	case hir.OpShr:
		if i.LogicalShift || isUnsigned {
			opStr = "lshr"
		} else {
			opStr = "ashr"
		}
	case hir.OpEq:
		opStr = "icmp eq"
	case hir.OpNeq:
		opStr = "icmp ne"
	case hir.OpLt:
		opStr = "icmp slt"
	case hir.OpLe:
		opStr = "icmp sle"
	case hir.OpGt:
		opStr = "icmp sgt"
	case hir.OpGe:
		opStr = "icmp sge"
	}
	e.b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", i.Dst, opStr, llvmT, lVal, rVal))
}

func isIntegerLLVMType(llvmType string) bool {
	return strings.HasPrefix(llvmType, "i") && len(llvmType) > 1
}

func isShiftIntegerType(typ sema.Type) bool {
	if typ == nil || !isIntegerLLVMType(typ.LLVMType()) || typ == sema.TypeBool {
		return false
	}
	_, ok := typ.(*sema.BasicType)
	return ok
}

func (e *Emitter) emitUnary(i *hir.InstrUnary) {
	typ := i.Val.Type()
	val := e.formatVal(i.Val)
	if i.Op == hir.OpNot {
		if typ.LLVMType() == "i1" {
			e.b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", i.Dst, val))
		} else {
			e.b.WriteString(fmt.Sprintf("  %s = xor %s %s, -1\n", i.Dst, typ.LLVMType(), val))
		}
	} else if i.Op == hir.OpNeg {
		if typ == sema.TypeFloat64 || typ == sema.TypeFloat32 {
			e.b.WriteString(fmt.Sprintf("  %s = fsub %s 0.0, %s\n", i.Dst, typ.LLVMType(), val))
		} else {
			e.b.WriteString(fmt.Sprintf("  %s = sub %s 0, %s\n", i.Dst, typ.LLVMType(), val))
		}
	}
}

func (e *Emitter) emitBoxInterface(i *hir.InstrBoxInterface) {
	fromLLVM := i.Val.Type().LLVMType()
	val := e.formatVal(i.Val)

	dataPtr := e.nextTmp()
	if strings.HasSuffix(fromLLVM, "*") {
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", dataPtr, fromLLVM, val))
	} else {
		allocaTmp := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s\n", allocaTmp, fromLLVM))
		e.b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fromLLVM, val, fromLLVM, allocaTmp))
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", dataPtr, fromLLVM, allocaTmp))
	}

	if i.Iface.IsAny() {
		typeID := i.Val.Type().TypeID(e.semaCtx)
		intLLVM := sema.TypeInt32.LLVMType()
		anyLLVM := fmt.Sprintf("{ %s, i8* }", intLLVM)
		t1 := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, %s %d, 0\n", t1, anyLLVM, intLLVM, typeID))
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i8* %s, 1\n", i.Dst, anyLLVM, t1, dataPtr))
		return
	}

	itabStructType := ""
	for _, itab := range e.prog.Itabs {
		if itab.GlobalName == i.ItabName {
			itabStructType = fmt.Sprintf("%%struct.%s*", itab.ItabStructName)
			break
		}
	}
	if itabStructType == "" {
		ifName := strings.ReplaceAll(i.Iface.Name, ".", "_")
		if ifName == "" {
			ifName = "anon_iface"
		}
		itabStructType = fmt.Sprintf("%%struct.__itab_%s*", ifName)
	}

	itabPtr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast %s @%s to i8*\n", itabPtr, itabStructType, i.ItabName))
	t1 := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } undef, i8* %s, 0\n", t1, dataPtr))
	e.b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } %s, i8* %s, 1\n", i.Dst, t1, itabPtr))
}

func intRank(llvm string) int {
	switch llvm {
	case "i64":
		return 64
	case "i32":
		return 32
	case "i16":
		return 16
	case "i8":
		return 8
	case "i1":
		return 1
	default:
		return 0
	}
}

func isFloatType(llvm string) bool {
	return llvm == "double" || llvm == "float"
}

func (e *Emitter) emitCast(i *hir.InstrCast) {
	fromLLVM := i.Val.Type().LLVMType()
	toLLVM := i.ToType.LLVMType()
	val := e.formatVal(i.Val)

	rFrom := intRank(fromLLVM)
	rTo := intRank(toLLVM)
	isFromPtr := strings.HasSuffix(fromLLVM, "*")
	isToPtr := strings.HasSuffix(toLLVM, "*")

	if fromLLVM == toLLVM {
		if strings.HasPrefix(fromLLVM, "{") || strings.HasPrefix(fromLLVM, "[") {
			panic(fmt.Sprintf("[Emitter Panic] invalid cast: cannot bitcast aggregate type '%s'", fromLLVM))
		}
		if isFloatType(fromLLVM) {
			e.b.WriteString(fmt.Sprintf("  %s = fadd %s %s, 0.0\n", i.Dst, fromLLVM, val))
		} else if rFrom > 0 {
			e.b.WriteString(fmt.Sprintf("  %s = or %s %s, 0\n", i.Dst, fromLLVM, val))
		} else if isFromPtr {
			elem := strings.TrimSuffix(fromLLVM, "*")
			e.b.WriteString(fmt.Sprintf("  %s = getelementptr %s, %s %s, i32 0\n", i.Dst, elem, fromLLVM, val))
		} else {
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		}
		return
	}
	// A Hike string is a length-aware aggregate whose first field is the
	// backing byte pointer.  Some variadic/FFI paths request its pointer view
	// directly; lower it as extractvalue instead of rejecting an aggregate
	// bitcast.
	if strings.HasPrefix(fromLLVM, "{") && !strings.HasSuffix(fromLLVM, "*") && isToPtr {
		aggregateVal := val
		e.b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", i.Dst, strings.TrimSuffix(fromLLVM, "*"), aggregateVal))
		return
	}
	if isFromPtr && toLLVM == "{ i8*, i32, i32 }" {
		base := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, %s %s, 0\n", base, toLLVM, fromLLVM, val))
		length := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call i32 @strlen(i8* %s)\n", length, val))
		withOffset := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i32 0, 1\n", withOffset, toLLVM, base))
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i32 %s, 2\n", i.Dst, toLLVM, withOffset, length))
		return
	}

	if isFloatType(fromLLVM) && rTo > 0 {
		e.b.WriteString(fmt.Sprintf("  %s = fptosi %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	if rFrom > 0 && isFloatType(toLLVM) {
		e.b.WriteString(fmt.Sprintf("  %s = sitofp %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	if fromLLVM == "double" && toLLVM == "float" {
		e.b.WriteString(fmt.Sprintf("  %s = fptrunc double %s to float\n", i.Dst, val))
		return
	}
	if fromLLVM == "float" && toLLVM == "double" {
		e.b.WriteString(fmt.Sprintf("  %s = fpext float %s to double\n", i.Dst, val))
		return
	}
	if rFrom > 0 && rTo > 0 {
		if rFrom > rTo {
			e.b.WriteString(fmt.Sprintf("  %s = trunc %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
			return
		}
		if rFrom < rTo {
			isUnsigned := false
			if rFrom == 1 {
				isUnsigned = true
			} else if i.Val.Type() != nil {
				tn := i.Val.Type().TypeName()
				if strings.HasPrefix(tn, "uint") || tn == "byte" || tn == "uintptr" {
					isUnsigned = true
				}
			}
			castOp := "sext"
			if isUnsigned {
				castOp = "zext"
			}
			e.b.WriteString(fmt.Sprintf("  %s = %s %s %s to %s\n", i.Dst, castOp, fromLLVM, val, toLLVM))
			return
		}
	}
	if isFromPtr && isToPtr {
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	if isFromPtr && rTo > 0 {
		e.b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	if rFrom > 0 && isToPtr {
		e.b.WriteString(fmt.Sprintf("  %s = inttoptr %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	if (rFrom == 64 && toLLVM == "double") || (fromLLVM == "double" && rTo == 64) ||
		(rFrom == 32 && toLLVM == "float") || (fromLLVM == "float" && rTo == 32) {
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}

	panic(fmt.Sprintf("[Emitter Panic] invalid cast operation: cannot cast '%s' to '%s' (val: %s, dst: %s)",
		fromLLVM, toLLVM, val, i.Dst))
}

func (e *Emitter) emitCallIndirect(i *hir.InstrCallIndirect) {
	retTypeStr := "void"
	if i.Dst != nil {
		retTypeStr = i.Dst.Typ.LLVMType()
	}

	fnVal := e.formatVal(i.FnPtr)
	if i.FnPtr != nil && i.FnPtr.Type() != nil && i.FnPtr.Type().LLVMType() != "i8*" {
		castFn := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castFn, i.FnPtr.Type().LLVMType(), fnVal))
		fnVal = castFn
	}

	var envVal string
	if i.EnvPtr != nil {
		envVal = e.formatVal(i.EnvPtr)
		if i.EnvPtr.Type() != nil && i.EnvPtr.Type().LLVMType() != "i8*" {
			castEnv := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castEnv, i.EnvPtr.Type().LLVMType(), envVal))
			envVal = castEnv
		}
	}

	plainParamTypes := make([]string, len(i.Args))
	plainArgs := make([]string, len(i.Args))
	for idx, a := range i.Args {
		plainParamTypes[idx] = a.Type().LLVMType()
		plainArgs[idx] = fmt.Sprintf("%s %s", a.Type().LLVMType(), e.formatVal(a))
	}
	plainSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(plainParamTypes, ", "))

	isNilConst := false
	if _, ok := i.EnvPtr.(*hir.ConstNil); ok {
		isNilConst = true
	}
	if i.EnvPtr == nil || isNilConst || envVal == "null" {
		typedFn := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", typedFn, fnVal, plainSig))
		if i.Dst != nil {
			e.b.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", i.Dst, retTypeStr, typedFn, strings.Join(plainArgs, ", ")))
		} else {
			e.b.WriteString(fmt.Sprintf("  call void %s(%s)\n", typedFn, strings.Join(plainArgs, ", ")))
		}
		return
	}

	closureParamTypes := []string{"i8*"}
	closureArgs := []string{fmt.Sprintf("i8* %s", envVal)}
	for _, a := range i.Args {
		closureParamTypes = append(closureParamTypes, a.Type().LLVMType())
		closureArgs = append(closureArgs, fmt.Sprintf("%s %s", a.Type().LLVMType(), e.formatVal(a)))
	}
	closureSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(closureParamTypes, ", "))

	e.regCount++
	id := e.regCount
	lblPlain := fmt.Sprintf("call.plain.%d", id)
	lblClosure := fmt.Sprintf("call.closure.%d", id)
	lblCont := fmt.Sprintf("call.cont.%d", id)

	cond := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = icmp eq i8* %s, null\n", cond, envVal))
	e.b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", cond, lblPlain, lblClosure))

	e.b.WriteString(fmt.Sprintf("%s:\n", lblPlain))
	typedFnPlain := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", typedFnPlain, fnVal, plainSig))
	var resPlain string
	if i.Dst != nil && retTypeStr != "void" {
		resPlain = e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", resPlain, retTypeStr, typedFnPlain, strings.Join(plainArgs, ", ")))
	} else {
		e.b.WriteString(fmt.Sprintf("  call void %s(%s)\n", typedFnPlain, strings.Join(plainArgs, ", ")))
	}
	e.b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCont))

	e.b.WriteString(fmt.Sprintf("%s:\n", lblClosure))
	typedFnClosure := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", typedFnClosure, fnVal, closureSig))
	var resClosure string
	if i.Dst != nil && retTypeStr != "void" {
		resClosure = e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", resClosure, retTypeStr, typedFnClosure, strings.Join(closureArgs, ", ")))
	} else {
		e.b.WriteString(fmt.Sprintf("  call void %s(%s)\n", typedFnClosure, strings.Join(closureArgs, ", ")))
	}
	e.b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCont))

	e.b.WriteString(fmt.Sprintf("%s:\n", lblCont))
	if i.Dst != nil && retTypeStr != "void" {
		e.b.WriteString(fmt.Sprintf("  %s = phi %s [ %s, %%%s ], [ %s, %%%s ]\n",
			i.Dst, retTypeStr, resPlain, lblPlain, resClosure, lblClosure))
	}
}

func (e *Emitter) emitCallIface(i *hir.InstrCallIface) {
	dataPtr := e.nextTmp()
	itabRaw := e.nextTmp()
	ifaceVal := e.formatVal(i.IfaceVal)
	ifaceType := "{ i8*, i8* }"
	if i.IfaceVal != nil && i.IfaceVal.Type() != nil {
		ifaceType = i.IfaceVal.Type().LLVMType()
	}

	e.b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtr, ifaceType, ifaceVal))
	e.b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", itabRaw, ifaceType, ifaceVal))

	retTypeStr := "void"
	if i.Dst != nil {
		retTypeStr = i.Dst.Typ.LLVMType()
	}

	paramTypes := []string{"i8*"}
	callArgs := []string{fmt.Sprintf("i8* %s", dataPtr)}
	for _, a := range i.Args {
		paramTypes = append(paramTypes, a.Type().LLVMType())
		callArgs = append(callArgs, fmt.Sprintf("%s %s", a.Type().LLVMType(), e.formatVal(a)))
	}
	rawFnSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(paramTypes, ", "))

	itabArr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i8**\n", itabArr, itabRaw))
	slotPtr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8*, i8** %s, i32 %d\n",
		slotPtr, itabArr, i.MethodIndex+1))

	fnRaw := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = load i8*, i8** %s\n", fnRaw, slotPtr))
	fnPtr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", fnPtr, fnRaw, rawFnSig))

	if i.Dst != nil {
		e.b.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", i.Dst, retTypeStr, fnPtr, strings.Join(callArgs, ", ")))
	} else {
		e.b.WriteString(fmt.Sprintf("  call void %s(%s)\n", fnPtr, strings.Join(callArgs, ", ")))
	}
}

func (e *Emitter) emitTerminator(term hir.Terminator, isMain bool) {
	if term == nil {
		return
	}
	start := e.b.Len()
	e.emitTerminatorBody(term, isMain)
	e.appendDebugLocation(start, term)
}

func (e *Emitter) emitTerminatorBody(term hir.Terminator, isMain bool) {
	switch t := term.(type) {
	case *hir.InstrJump:
		e.b.WriteString(fmt.Sprintf("  br label %%%s\n", t.Target))

	case *hir.InstrBranch:
		e.b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n",
			e.formatVal(t.Cond), t.ThenTarget, t.ElseTarget))

	case *hir.InstrBrTable:
		indexType := t.Index.Type().LLVMType()
		var cases strings.Builder
		for value, target := range t.Targets {
			fmt.Fprintf(&cases, " %s %d, label %%%s", indexType, value, target)
		}
		e.b.WriteString(fmt.Sprintf("  switch %s %s, label %%%s [%s ]\n",
			indexType, e.formatVal(t.Index), t.DefaultTarget, cases.String()))

	case *hir.InstrUnreachable:
		e.b.WriteString("  unreachable\n")

	case *hir.InstrPanic:
		if e.currentFn != nil && e.currentFn.HasLocalRecover {
			active := e.nextTmp()
			fatalLabel := e.panicLabel(e.currentFnID, e.panicTermID, "fatal")
			recoveredLabel := e.panicLabel(e.currentFnID, e.panicTermID, "recovered")
			e.panicTermID++
			e.b.WriteString(fmt.Sprintf("  %s = call i1 @__hike_panic_is_active()\n", active))
			e.b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", active, fatalLabel, recoveredLabel))
			e.b.WriteString(fmt.Sprintf("%s:\n", fatalLabel))
			e.b.WriteString(fmt.Sprintf("  call void @__hike_panic_fatal(i32 %d)\n", t.SiteID))
			e.b.WriteString("  unreachable\n")
			e.b.WriteString(fmt.Sprintf("%s:\n", recoveredLabel))
			e.emitDefaultReturn(e.currentFn, e.currentIsMain)
			return
		}
		e.b.WriteString(fmt.Sprintf("  call void @__hike_panic_fatal(i32 %d)\n", t.SiteID))
		e.b.WriteString("  unreachable\n")

	case *hir.InstrReturn:
		if len(t.Vals) == 0 {
			if isMain {
				e.b.WriteString("  ret i32 0\n")
			} else {
				e.b.WriteString("  ret void\n")
			}
		} else if len(t.Vals) == 1 {
			val := e.formatVal(t.Vals[0])
			typStr := t.Vals[0].Type().LLVMType()
			if isMain {
				if typStr == "i32" {
					e.b.WriteString(fmt.Sprintf("  ret i32 %s\n", val))
				} else if r := intRank(typStr); r > 32 {
					truncReg := e.nextTmp()
					e.b.WriteString(fmt.Sprintf("  %s = trunc %s %s to i32\n", truncReg, typStr, val))
					e.b.WriteString(fmt.Sprintf("  ret i32 %s\n", truncReg))
				} else if r > 0 && r < 32 {
					extReg := e.nextTmp()
					e.b.WriteString(fmt.Sprintf("  %s = sext %s %s to i32\n", extReg, typStr, val))
					e.b.WriteString(fmt.Sprintf("  ret i32 %s\n", extReg))
				} else {
					e.b.WriteString("  ret i32 0\n")
				}
			} else {
				e.b.WriteString(fmt.Sprintf("  ret %s %s\n", typStr, val))
			}
		} else {
			types := make([]string, len(t.Vals))
			for i, v := range t.Vals {
				types[i] = v.Type().LLVMType()
			}
			aggType := fmt.Sprintf("{ %s }", strings.Join(types, ", "))

			curAgg := "undef"
			for i, v := range t.Vals {
				nextAgg := e.nextTmp()
				e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, %d\n",
					nextAgg, aggType, curAgg, v.Type().LLVMType(), e.formatVal(v), i))
				curAgg = nextAgg
			}
			e.b.WriteString(fmt.Sprintf("  ret %s %s\n", aggType, curAgg))
		}
	}
}

func (e *Emitter) formatVal(v hir.Value) string {
	if v == nil {
		logger.LogVerbose2("[Verbose2] WARNING: formatVal received nil Value\n")
		return "0"
	}
	intLLVM := sema.TypeInt.LLVMType()
	switch val := v.(type) {
	case *hir.ConstZero:
		return "zeroinitializer"
	case *hir.ConstString:
		ptr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* @%s, %s 0, %s 8\n",
			ptr, val.Length+8, val.Length+8, val.Label, intLLVM, intLLVM))
		if val.Typ == sema.TypeString {
			t1, t2, t3 := e.nextTmp(), e.nextTmp(), e.nextTmp()
			llvmType := val.Typ.LLVMType()
			e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, llvmType, ptr))
			e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i32 0, 1\n", t2, llvmType, t1))
			e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i32 %d, 2\n", t3, llvmType, t2, val.Length-1))
			return t3
		}
		return ptr
	case *hir.ConstNil:
		return "null"
	case *hir.GlobalVar:
		return "@" + e.functionSymbol(val.Name)
	default:
		return val.String()
	}
}

func encodeLLVMString(str string) string {
	var encoded strings.Builder
	for i := 0; i < len(str); i++ {
		b := str[i]
		switch b {
		case '\n':
			encoded.WriteString("\\0A")
		case '\t':
			encoded.WriteString("\\09")
		case '\r':
			encoded.WriteString("\\0D")
		case '"':
			encoded.WriteString("\\22")
		case '\\':
			encoded.WriteString("\\5C")
		default:
			if b < 32 || b > 126 {
				encoded.WriteString(fmt.Sprintf("\\%02X", b))
			} else {
				encoded.WriteByte(b)
			}
		}
	}
	encoded.WriteString("\\00")
	return encoded.String()
}

func encodeStringHeader(capacity uint32) string {
	// String literals live in static storage and must never be freed.  A
	// INT32_MIN marks such an immortal buffer; heap-created strings start at
	// one in the runtime constructors.  -1 remains available to expose a
	// reference-counting underflow instead of silently making it immortal.
	return fmt.Sprintf("\\%02X\\%02X\\%02X\\%02X\\00\\00\\00\\80",
		byte(capacity), byte(capacity>>8), byte(capacity>>16), byte(capacity>>24))
}

func encodeLLVMAsmString(str string) string {
	var encoded strings.Builder
	for i := 0; i < len(str); i++ {
		switch str[i] {
		case '\n':
			encoded.WriteString("\\0A")
		case '\t':
			encoded.WriteString("\\09")
		case '\r':
			encoded.WriteString("\\0D")
		case '"':
			encoded.WriteString("\\22")
		case '\\':
			encoded.WriteString("\\5C")
		default:
			if str[i] < 32 || str[i] > 126 {
				encoded.WriteString(fmt.Sprintf("\\%02X", str[i]))
			} else {
				encoded.WriteByte(str[i])
			}
		}
	}
	return encoded.String()
}

func normalizeInlineAsmTemplate(template string) string {
	var out strings.Builder
	for i := 0; i < len(template); i++ {
		if template[i] == '%' && i+1 < len(template) && template[i+1] >= '0' && template[i+1] <= '9' {
			// Hike uses %N for the Nth operand; LLVM inline asm uses $N.
			out.WriteByte('$')
		} else {
			out.WriteByte(template[i])
		}
	}
	return out.String()
}
