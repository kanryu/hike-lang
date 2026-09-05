package llvm

import (
	"fmt"
	"strings"

	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

type asyncThunk struct {
	name        string
	retLLVMType string
}

type Emitter struct {
	prog         *hir.Program
	semaCtx      *sema.Context
	targetTriple string
	b            strings.Builder
	regCount     int
	asyncThunks  map[string]*asyncThunk // 戻り値型ごとのサンク関数キャッシュ
}

func New(prog *hir.Program, semaCtx *sema.Context, targetTriple string) *Emitter {
	if targetTriple == "" {
		targetTriple = "x86_64-unknown-linux-gnu"
	}
	return &Emitter{
		prog:         prog,
		semaCtx:      semaCtx,
		targetTriple: targetTriple,
		asyncThunks:  make(map[string]*asyncThunk),
	}
}

func (e *Emitter) nextTmp() string {
	e.regCount++
	return fmt.Sprintf("%%.b%d", e.regCount)
}

func (e *Emitter) Emit() string {
	e.b.Reset()
	e.emitPrologue()
	e.emitTypeDefs()
	e.emitConstants()
	e.emitGlobals()
	e.emitItabs()
	e.emitFunctions()
	e.emitAsyncThunks()
	return e.b.String()
}

func (e *Emitter) emitPrologue() {
	e.b.WriteString(fmt.Sprintf("; ModuleID = '%s'\n", e.prog.ModuleName))
	e.b.WriteString(fmt.Sprintf("source_filename = \"%s.hike\"\n", e.prog.ModuleName))
	e.b.WriteString(fmt.Sprintf("target triple = \"%s\"\n\n", e.targetTriple))

	e.b.WriteString(builtinRuntimeIR)
	e.b.WriteString("\n\n")
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
		e.b.WriteString(fmt.Sprintf("@%s = private unnamed_addr constant [%d x i8] c\"%s\", align 1\n",
			sc.Label, sc.Length, escaped))
	}
	if len(e.prog.StringConstants) > 0 {
		e.b.WriteString("\n")
	}
}

func (e *Emitter) emitGlobals() {
	for _, g := range e.prog.Globals {
		e.b.WriteString(fmt.Sprintf("@%s = global %s zeroinitializer, align 8\n", g.Name, g.Typ.LLVMType()))
	}
	if len(e.prog.Globals) > 0 {
		e.b.WriteString("\n")
	}
}

func (e *Emitter) emitItabs() {
	emittedTypes := make(map[string]bool)

	for _, itab := range e.prog.Itabs {
		if !emittedTypes[itab.ItabStructName] {
			methodSigs := []string{"i64"}
			for _, m := range itab.Methods {
				retTypeStr := "void"
				if len(m.MethodType.ReturnTypes) == 1 {
					retTypeStr = m.MethodType.ReturnTypes[0].LLVMType()
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

		fieldValues := []string{fmt.Sprintf("i64 %d", itab.TypeID)}
		sName := strings.TrimPrefix(itab.ConcreteType.TypeName(), "*")

		for _, m := range itab.Methods {
			retTypeStr := "void"
			if len(m.MethodType.ReturnTypes) == 1 {
				retTypeStr = m.MethodType.ReturnTypes[0].LLVMType()
			}
			rawParams := []string{"i8*"}

			// 構造体が存在する場合のみ %struct.xxx* とし、それ以外は LLVMType を採用
			concreteRecv := fmt.Sprintf("%%struct.%s*", sName)
			if e.semaCtx != nil {
				if st, _ := e.semaCtx.LookupStruct(sName); st == nil {
					concreteRecv = itab.ConcreteType.LLVMType()
				}
			}
			concreteParams := []string{concreteRecv}

			for _, pt := range m.MethodType.ParamTypes {
				rawParams = append(rawParams, pt.LLVMType())
				concreteParams = append(concreteParams, pt.LLVMType())
			}

			rawSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(rawParams, ", "))
			concreteSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(concreteParams, ", "))
			fieldValues = append(fieldValues, fmt.Sprintf("%s bitcast (%s @%s to %s)", rawSig, concreteSig, m.TargetFnName, rawSig))
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
		for _, bb := range fn.Blocks {
			for _, inst := range bb.Instructions {
				if call, ok := inst.(*hir.InstrCallStatic); ok {
					referencedExterns[call.CalleeName] = true
				}
			}
		}
	}

	for _, fn := range e.prog.Functions {
		if fn.IsExtern {
			// ランタイムで定義・提供されるシンボルは重複定義を避けるため declare をスキップ
			switch fn.Name {
			case "malloc", "free", "calloc", "strcmp", "strlen", "memcpy", "memcmp", "printf",
				"QueueUserWorkItem", "CreateEventA", "SetEvent", "WaitForSingleObject", "CloseHandle", "Sleep",
				"os_sleep_ms", "c_os_sleep_ms":
				continue
			}

			if fn.IsCFunc && !referencedExterns[fn.Name] {
				continue
			}

			retTypeStr := "void"
			if len(fn.ReturnTypes) == 1 {
				retTypeStr = fn.ReturnTypes[0].LLVMType()
			}
			paramTypes := make([]string, len(fn.Params))
			for i, p := range fn.Params {
				paramTypes[i] = p.Typ.LLVMType()
			}
			if fn.IsVariadic {
				paramTypes = append(paramTypes, "...")
			}
			e.b.WriteString(fmt.Sprintf("declare %s @%s(%s)\n", retTypeStr, fn.Name, strings.Join(paramTypes, ", ")))
			continue
		}
		e.emitFunction(fn)
	}
}

func (e *Emitter) emitFunction(fn *hir.Function) {
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

	e.b.WriteString(fmt.Sprintf("define %s @%s(%s) {\n", retTypeStr, fn.Name, strings.Join(params, ", ")))

	for _, bb := range fn.Blocks {
		e.b.WriteString(fmt.Sprintf("%s:\n", bb.Label))
		for _, inst := range bb.Instructions {
			e.emitInstruction(inst)
		}
		if bb.Terminator != nil {
			e.emitTerminator(bb.Terminator, isMain)
		} else {
			if isMain {
				e.b.WriteString("  ret i32 0\n")
			} else {
				e.b.WriteString("  ret void\n")
			}
		}
	}

	e.b.WriteString("}\n\n")
}

func (e *Emitter) isVariadicFunc(name string) (bool, string) {
	var fnType *sema.FuncType
	if e.semaCtx != nil {
		fnType, _ = e.semaCtx.LookupFunction(name)
	}
	if fnType != nil && fnType.IsVariadic {
		paramTypes := make([]string, len(fnType.ParamTypes))
		for idx, pt := range fnType.ParamTypes {
			paramTypes[idx] = pt.LLVMType()
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
			sz = 8
		}
		return sz
	}
	sz := int64(0)
	for _, rt := range retTypes {
		s := int64(rt.Size())
		if s <= 0 {
			s = 8
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
		e.b.WriteString("  %p_fn = getelementptr inbounds i8*, i8** %env_arr, i64 0\n")
		e.b.WriteString("  %fn_raw = load i8*, i8** %p_fn\n")
		e.b.WriteString("  %p_env = getelementptr inbounds i8*, i8** %env_arr, i64 1\n")
		e.b.WriteString("  %real_env = load i8*, i8** %p_env\n\n")

		if thunk.retLLVMType == "void" {
			e.b.WriteString("  %typed_fn = bitcast i8* %fn_raw to void (i8*)*\n")
			e.b.WriteString("  call void %typed_fn(i8* %real_env)\n")
		} else {
			e.b.WriteString(fmt.Sprintf("  %%typed_fn = bitcast i8* %%fn_raw to %s (i8*)*\n", thunk.retLLVMType))
			e.b.WriteString(fmt.Sprintf("  %%res = call %s %%typed_fn(i8* %%real_env)\n", thunk.retLLVMType))
			e.b.WriteString(fmt.Sprintf("  %%typed_buf = bitcast i8* %%buf to %s*\n", thunk.retLLVMType))
			e.b.WriteString(fmt.Sprintf("  store %s %%res, %s* %%typed_buf\n", thunk.retLLVMType, thunk.retLLVMType))
		}

		e.b.WriteString("\n  call void @free(i8* %wrapper_env)\n")
		e.b.WriteString("  ret void\n")
		e.b.WriteString("}\n\n")
	}
}

func (e *Emitter) emitInstruction(inst hir.Instruction) {
	switch i := inst.(type) {
	case *hir.InstrAlloca:
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s\n", i.Dst, i.AllocType.LLVMType()))

	case *hir.InstrAllocaDynamic:
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s, i64 %s, align 8\n", i.Dst, i.AllocType.LLVMType(), e.formatVal(i.Size)))

	case *hir.InstrHeapAlloc:
		rawPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", rawPtr, e.formatVal(i.Size)))
		e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", i.Dst, rawPtr, i.AllocType.LLVMType()))

	case *hir.InstrLoad:
		e.b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", i.Dst, i.Dst.Typ.LLVMType(), i.Dst.Typ.LLVMType(), e.formatVal(i.Ptr)))

	case *hir.InstrStore:
		e.b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n",
			i.Val.Type().LLVMType(), e.formatVal(i.Val),
			i.Val.Type().LLVMType(), e.formatVal(i.Ptr)))

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
		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %s %s, i32 0, i32 %d\n",
			i.Dst, stName, i.BasePtr.Type().LLVMType(), e.formatVal(i.BasePtr), i.FieldIndex))

	case *hir.InstrGetElemPtr:
		baseType := i.BasePtr.Type()

		if pt, ok := baseType.(*sema.PointerType); ok {
			if ar, okArr := pt.Base.(*sema.ArrayType); okArr {
				e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s %s, i64 0, i64 %s\n",
					i.Dst, ar.LLVMType(), baseType.LLVMType(), e.formatVal(i.BasePtr), e.formatVal(i.Index)))
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

		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s %s, i64 %s\n",
			i.Dst, elemLLVM, baseType.LLVMType(), e.formatVal(i.BasePtr), e.formatVal(i.Index)))

	case *hir.InstrCallStatic:
		args := make([]string, len(i.Args))
		for idx, a := range i.Args {
			args[idx] = fmt.Sprintf("%s %s", a.Type().LLVMType(), e.formatVal(a))
		}

		isVar, varSig := e.isVariadicFunc(i.CalleeName)
		if isVar {
			if i.Dst != nil {
				e.b.WriteString(fmt.Sprintf("  %s = call %s %s @%s(%s)\n",
					i.Dst, i.Dst.Typ.LLVMType(), varSig, i.CalleeName, strings.Join(args, ", ")))
			} else {
				e.b.WriteString(fmt.Sprintf("  call void %s @%s(%s)\n",
					varSig, i.CalleeName, strings.Join(args, ", ")))
			}
		} else {
			if i.Dst != nil {
				e.b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n",
					i.Dst, i.Dst.Typ.LLVMType(), i.CalleeName, strings.Join(args, ", ")))
			} else {
				e.b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", i.CalleeName, strings.Join(args, ", ")))
			}
		}

	case *hir.InstrCallIndirect:
		e.emitCallIndirect(i)

	case *hir.InstrCallIface:
		e.emitCallIface(i)

	// 追加: スレッドプールへの非同期タスク投入
	case *hir.InstrAsync:
		retLLVM := e.getRetLLVMType(i.RetTypes)
		retSize := e.getRetSize(i.RetTypes)
		thunkName := e.getOrCreateAsyncThunk(retLLVM)

		rawEnv := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 16)\n", rawEnv))
		arrEnv := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i8**\n", arrEnv, rawEnv))

		pFn := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8*, i8** %s, i64 0\n", pFn, arrEnv))
		fnVal := e.formatVal(i.FnPtr)
		if i.FnPtr.Type().LLVMType() != "i8*" {
			castFn := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castFn, i.FnPtr.Type().LLVMType(), fnVal))
			fnVal = castFn
		}
		e.b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", fnVal, pFn))

		pEnv := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8*, i8** %s, i64 1\n", pEnv, arrEnv))
		envVal := e.formatVal(i.EnvPtr)
		if i.EnvPtr.Type().LLVMType() != "i8*" {
			castEnv := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castEnv, i.EnvPtr.Type().LLVMType(), envVal))
			envVal = castEnv
		}
		e.b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", envVal, pEnv))

		thunkPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast void (i8*, i8*)* @%s to i8*\n", thunkPtr, thunkName))
		taskPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call %%struct.__hike_task* @__hike_async(i8* %s, i8* %s, i64 %d)\n",
			taskPtr, thunkPtr, rawEnv, retSize))
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %%struct.__hike_task* %s to i8*\n", i.Dst, taskPtr))

	// 追加: タスクの完了待機とバッファ取得
	case *hir.InstrTaskWait:
		taskPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%struct.__hike_task*\n", taskPtr, e.formatVal(i.Task)))
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @__hike_task_wait(%%struct.__hike_task* %s)\n", i.Dst, taskPtr))

	case *hir.InstrExtractValue:
		e.b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, %d\n",
			i.Dst, i.Agg.Type().LLVMType(), e.formatVal(i.Agg), i.Index))

	case *hir.InstrInsertValue:
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, %d\n",
			i.Dst, i.Agg.Type().LLVMType(), e.formatVal(i.Agg),
			i.Val.Type().LLVMType(), e.formatVal(i.Val), i.Index))
	}
}

func (e *Emitter) emitBinary(i *hir.InstrBinary) {
	typ := i.L.Type()
	isFloat := (typ == sema.TypeFloat64 || typ == sema.TypeFloat32)
	llvmT := typ.LLVMType()
	lVal := e.formatVal(i.L)
	rVal := e.formatVal(i.R)

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
		opStr = "ashr"
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

func (e *Emitter) emitUnary(i *hir.InstrUnary) {
	typ := i.Val.Type()
	val := e.formatVal(i.Val)
	if i.Op == hir.OpNot {
		e.b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", i.Dst, val))
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
		typeID := e.semaCtx.GetTypeID(i.Val.Type())
		t1 := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i64 } undef, i8* %s, 0\n", t1, dataPtr))
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i64 } %s, i64 %d, 1\n", i.Dst, t1, typeID))
		return
	}

	itabPtr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast %%struct.__itab_%s* @%s to i8*\n", itabPtr, i.Iface.Name, i.ItabName))
	t1 := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } undef, i8* %s, 0\n", t1, dataPtr))
	e.b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } %s, i8* %s, 1\n", i.Dst, t1, itabPtr))
}

func (e *Emitter) emitCast(i *hir.InstrCast) {
	fromLLVM := i.Val.Type().LLVMType()
	toLLVM := i.ToType.LLVMType()
	val := e.formatVal(i.Val)

	if (fromLLVM == "double" || fromLLVM == "float") && (toLLVM == "i64" || toLLVM == "i32") {
		e.b.WriteString(fmt.Sprintf("  %s = fptosi %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	if (fromLLVM == "i64" || fromLLVM == "i32") && (toLLVM == "double" || toLLVM == "float") {
		e.b.WriteString(fmt.Sprintf("  %s = sitofp %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	if fromLLVM == "i64" && (toLLVM == "i32" || toLLVM == "i8" || toLLVM == "i1") {
		e.b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to %s\n", i.Dst, val, toLLVM))
		return
	}
	if (fromLLVM == "i32" || fromLLVM == "i8" || fromLLVM == "i1") && toLLVM == "i64" {
		e.b.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", i.Dst, fromLLVM, val))
		return
	}
	if strings.HasSuffix(fromLLVM, "*") && strings.HasSuffix(toLLVM, "*") {
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	if strings.HasSuffix(fromLLVM, "*") && toLLVM == "i64" {
		e.b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", i.Dst, fromLLVM, val))
		return
	}
	if fromLLVM == "i64" && strings.HasSuffix(toLLVM, "*") {
		e.b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", i.Dst, val, toLLVM))
		return
	}

	e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
}

func (e *Emitter) emitCallIndirect(i *hir.InstrCallIndirect) {
	retTypeStr := "void"
	if i.Dst != nil {
		retTypeStr = i.Dst.Typ.LLVMType()
	}

	paramTypes := []string{"i8*"}
	args := []string{fmt.Sprintf("i8* %s", e.formatVal(i.EnvPtr))}
	for _, a := range i.Args {
		paramTypes = append(paramTypes, a.Type().LLVMType())
		args = append(args, fmt.Sprintf("%s %s", a.Type().LLVMType(), e.formatVal(a)))
	}

	rawSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(paramTypes, ", "))
	typedFn := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", typedFn, e.formatVal(i.FnPtr), rawSig))

	if i.Dst != nil {
		e.b.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", i.Dst, retTypeStr, typedFn, strings.Join(args, ", ")))
	} else {
		e.b.WriteString(fmt.Sprintf("  call void %s(%s)\n", typedFn, strings.Join(args, ", ")))
	}
}

func (e *Emitter) emitCallIface(i *hir.InstrCallIface) {
	dataPtr := e.nextTmp()
	itabRaw := e.nextTmp()
	ifaceVal := e.formatVal(i.IfaceVal)
	e.b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 0\n", dataPtr, ifaceVal))
	e.b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 1\n", itabRaw, ifaceVal))

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

	itabTyped := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s**\n", itabTyped, itabRaw, rawFnSig))
	gepMethod := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s*, %s** %s, i32 %d\n",
		gepMethod, rawFnSig, rawFnSig, itabTyped, i.MethodIndex+1))

	fnPtr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", fnPtr, rawFnSig, rawFnSig, gepMethod))

	if i.Dst != nil {
		e.b.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", i.Dst, retTypeStr, fnPtr, strings.Join(callArgs, ", ")))
	} else {
		e.b.WriteString(fmt.Sprintf("  call void %s(%s)\n", fnPtr, strings.Join(callArgs, ", ")))
	}
}

func (e *Emitter) emitTerminator(term hir.Terminator, isMain bool) {
	switch t := term.(type) {
	case *hir.InstrJump:
		e.b.WriteString(fmt.Sprintf("  br label %%%s\n", t.Target))

	case *hir.InstrBranch:
		e.b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n",
			e.formatVal(t.Cond), t.ThenTarget, t.ElseTarget))

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
				truncReg := e.nextTmp()
				e.b.WriteString(fmt.Sprintf("  %s = trunc %s %s to i32\n", truncReg, typStr, val))
				e.b.WriteString(fmt.Sprintf("  ret i32 %s\n", truncReg))
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
		return "0"
	}
	switch val := v.(type) {
	case *hir.ConstZero:
		return "zeroinitializer"
	case *hir.ConstString:
		tmp := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* @%s, i64 0, i64 0\n",
			tmp, val.Length, val.Length, val.Label))
		return tmp
	case *hir.ConstNil:
		return "null"
	case *hir.GlobalVar:
		return "@" + val.Name
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
