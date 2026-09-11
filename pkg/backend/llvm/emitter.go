package llvm

import (
	"fmt"
	"runtime"
	"strings"

	"hikec-go/pkg/hir"
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

func New(prog *hir.Program, semaCtx *sema.Context, targetTriple string) *Emitter {
	if targetTriple == "" {
		targetTriple = defaultTargetTriple()
	}
	e := &Emitter{
		prog:            prog,
		semaCtx:         semaCtx,
		targetTriple:    targetTriple,
		asyncThunks:     make(map[string]*asyncThunk),
		declaredSymbols: make(map[string]bool),
	}

	for sym := range RuntimeLLVMSymbols {
		e.declaredSymbols[sym] = true
	}
	return e
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

	// 並列実装された組み込みランタイムIRをそのまま出力
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
	align := sema.PointerSize
	for _, g := range e.prog.Globals {
		e.b.WriteString(fmt.Sprintf("@%s = global %s zeroinitializer, align %d\n", g.Name, g.Typ.LLVMType(), align))
	}
	if len(e.prog.Globals) > 0 {
		e.b.WriteString("\n")
	}
}

func (e *Emitter) emitItabs() {
	emittedTypes := make(map[string]bool)
	intLLVM := sema.TypeInt.LLVMType()

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

		fieldValues := []string{fmt.Sprintf("%s %d", intLLVM, itab.TypeID)}
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

			// 実体関数の正確な LLVM シグネチャを取得
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

	// itab 定数から参照されている関数も外部宣言対象に登録
	for _, itab := range e.prog.Itabs {
		for _, m := range itab.Methods {
			referencedExterns[m.TargetFnName] = true
		}
	}

	for _, fn := range e.prog.Functions {
		if fn.IsExtern {
			if e.declaredSymbols[fn.Name] {
				continue
			}

			if fn.IsCFunc && fn.CFuncTarget != "" && !referencedExterns[fn.Name] && !referencedExterns[fn.CFuncTarget] {
				continue
			}

			e.declaredSymbols[fn.Name] = true

			retTypeStr := "void"
			if len(fn.ReturnTypes) == 1 {
				retTypeStr = fn.ReturnTypes[0].LLVMType()
			} else if len(fn.ReturnTypes) > 1 {
				types := make([]string, len(fn.ReturnTypes))
				for i, rt := range fn.ReturnTypes {
					types[i] = rt.LLVMType()
				}
				retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
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

		e.declaredSymbols[fn.Name] = true
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

	storageClass := ""
	if fn.IsCFunc && e.isWindowsTarget() {
		storageClass = "dllexport "
	}

	e.b.WriteString(fmt.Sprintf("define %s%s @%s(%s) {\n", storageClass, retTypeStr, fn.Name, strings.Join(params, ", ")))

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
	intLLVM := sema.TypeInt.LLVMType()

	switch i := inst.(type) {
	case *hir.InstrAlloca:
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s\n", i.Dst, i.AllocType.LLVMType()))

	case *hir.InstrAllocaDynamic:
		sizeLLVM := intLLVM
		if i.Size != nil && i.Size.Type() != nil {
			sizeLLVM = i.Size.Type().LLVMType()
		}
		e.b.WriteString(fmt.Sprintf("  %s = alloca %s, %s %s, align %d\n",
			i.Dst, i.AllocType.LLVMType(), sizeLLVM, e.formatVal(i.Size), sema.PointerSize))

	case *hir.InstrHeapAlloc:
		sizeLLVM := intLLVM
		if i.Size != nil && i.Size.Type() != nil {
			sizeLLVM = i.Size.Type().LLVMType()
		}
		rawPtr := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(%s %s)\n", rawPtr, sizeLLVM, e.formatVal(i.Size)))
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

		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s %s, %s %s\n",
			i.Dst, elemLLVM, baseType.LLVMType(), e.formatVal(i.BasePtr), idxLLVM, e.formatVal(i.Index)))

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

	case *hir.InstrAsync:
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
		if i.FnPtr.Type().LLVMType() != "i8*" {
			castFn := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castFn, i.FnPtr.Type().LLVMType(), fnVal))
			fnVal = castFn
		}
		e.b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", fnVal, pFn))

		pEnv := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8*, i8** %s, i32 1\n", pEnv, arrEnv))
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
		e.b.WriteString(fmt.Sprintf("  %s = call %%struct.__hike_task* @__hike_async(i8* %s, i8* %s, %s %d)\n",
			taskPtr, thunkPtr, rawEnv, intLLVM, retSize))
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %%struct.__hike_task* %s to i8*\n", i.Dst, taskPtr))

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
		if i.Chan.Type().LLVMType() != "i8*" {
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
		if i.Chan.Type().LLVMType() != "i8*" {
			castCh := e.nextTmp()
			e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castCh, i.Chan.Type().LLVMType(), chVal))
			chVal = castCh
		}
		e.b.WriteString(fmt.Sprintf("  call void @__hike_chan_recv(i8* %s, i8* %s)\n", chVal, rawPtr))
		e.b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", i.Dst, elemLLVM, elemLLVM, tmpAlloca))

	case *hir.InstrChanClose:
		chVal := e.formatVal(i.Chan)
		if i.Chan.Type().LLVMType() != "i8*" {
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
		intLLVM := sema.TypeInt.LLVMType()
		anyLLVM := fmt.Sprintf("{ i8*, %s }", intLLVM)
		t1 := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, anyLLVM, dataPtr))
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %d, 1\n", i.Dst, anyLLVM, t1, intLLVM, typeID))
		return
	}

	ifName := strings.ReplaceAll(i.Iface.Name, ".", "_")
	if ifName == "" {
		ifName = "anon_iface"
	}

	itabPtr := e.nextTmp()
	e.b.WriteString(fmt.Sprintf("  %s = bitcast %%struct.__itab_%s* @%s to i8*\n", itabPtr, ifName, i.ItabName))
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

	if fromLLVM == toLLVM {
		if strings.HasPrefix(fromLLVM, "{") || strings.HasPrefix(fromLLVM, "[") {
			panic(fmt.Sprintf("[Emitter Panic] invalid cast: cannot bitcast aggregate type '%s'", fromLLVM))
		}
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}

	rFrom := intRank(fromLLVM)
	rTo := intRank(toLLVM)
	isFromPtr := strings.HasSuffix(fromLLVM, "*")
	isToPtr := strings.HasSuffix(toLLVM, "*")

	// 1. 浮動小数点数 -> 整数 (fptosi)
	if isFloatType(fromLLVM) && rTo > 0 {
		e.b.WriteString(fmt.Sprintf("  %s = fptosi %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	// 2. 整数 -> 浮動小数点数 (sitofp)
	if rFrom > 0 && isFloatType(toLLVM) {
		e.b.WriteString(fmt.Sprintf("  %s = sitofp %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	// 3. 浮動小数点数間の変換
	if fromLLVM == "double" && toLLVM == "float" {
		e.b.WriteString(fmt.Sprintf("  %s = fptrunc double %s to float\n", i.Dst, val))
		return
	}
	if fromLLVM == "float" && toLLVM == "double" {
		e.b.WriteString(fmt.Sprintf("  %s = fpext float %s to double\n", i.Dst, val))
		return
	}
	// 4. 整数間の拡縮 (Trunc / ZExt)
	if rFrom > 0 && rTo > 0 {
		if rFrom > rTo {
			e.b.WriteString(fmt.Sprintf("  %s = trunc %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
			return
		}
		if rFrom < rTo {
			e.b.WriteString(fmt.Sprintf("  %s = zext %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
			return
		}
	}
	// 5. ポインタ同士の変換 (bitcast)
	if isFromPtr && isToPtr {
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	// 6. ポインタ -> 整数 (ptrtoint)
	if isFromPtr && rTo > 0 {
		e.b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	// 7. 整数 -> ポインタ (inttoptr)
	if rFrom > 0 && isToPtr {
		e.b.WriteString(fmt.Sprintf("  %s = inttoptr %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}
	// 8. 同一ビット幅の整数と浮動小数点の相互変換 (bitcast)
	if (rFrom == 64 && toLLVM == "double") || (fromLLVM == "double" && rTo == 64) ||
		(rFrom == 32 && toLLVM == "float") || (fromLLVM == "float" && rTo == 32) {
		e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
		return
	}

	// 9. 配列型をスカラー値として扱う (lower/型推論バグ: int式が誤って[N x i64]になる)
	if strings.HasPrefix(fromLLVM, "[") {
		if strings.HasPrefix(toLLVM, "[") {
			// 配列→配列: bitcastでなくそのまま (型名が異なる場合のみbitcast、同一ならコピー)
			if fromLLVM == toLLVM {
				e.b.WriteString(fmt.Sprintf("  %s = %s\n", i.Dst, val))
			} else {
				e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
			}
		} else {
			// 配列→整数: 先頭要素を抽出
			e.b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", i.Dst, fromLLVM, val))
		}
		return
	}
	if strings.HasPrefix(toLLVM, "[") && rFrom > 0 {
		// 整数→配列: ゼロ初期化配列の要素0に挿入。要素型は rFrom(ビット数)から決定。
		elemLLVM := "i32"
		if rFrom >= 64 {
			elemLLVM = "i64"
		}
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s zeroinitializer, %s %s, 0\n", i.Dst, toLLVM, elemLLVM, val))
		return
	}

	// 9. 配列型をスカラー値として扱う (lower/型推論バグ: int式が誤って[N x i64]になる)
	if strings.HasPrefix(fromLLVM, "[") {
		if strings.HasPrefix(toLLVM, "[") {
			// 配列→配列: bitcastでなくそのまま (型名が異なる場合のみbitcast、同一ならコピー)
			if fromLLVM == toLLVM {
				e.b.WriteString(fmt.Sprintf("  %s = %s\n", i.Dst, val))
			} else {
				e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
			}
		} else {
			// 配列→整数: 先頭要素を抽出
			e.b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", i.Dst, fromLLVM, val))
		}
		return
	}
	if strings.HasPrefix(toLLVM, "[") && rFrom > 0 {
		// 整数→配列: ゼロ初期化配列の要素0に挿入。要素型は rFrom(ビット数)から決定。
		elemLLVM := "i32"
		if rFrom >= 64 {
			elemLLVM = "i64"
		}
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s zeroinitializer, %s %s, 0\n", i.Dst, toLLVM, elemLLVM, val))
		return
	}

	// 9. 配列型をスカラー値として扱う (lower/型推論バグ: int式が誤って[N x i64]になる)
	if strings.HasPrefix(fromLLVM, "[") {
		if strings.HasPrefix(toLLVM, "[") {
			// 配列→配列: bitcastでなくそのまま (型名が異なる場合のみbitcast、同一ならコピー)
			if fromLLVM == toLLVM {
				e.b.WriteString(fmt.Sprintf("  %s = %s\n", i.Dst, val))
			} else {
				e.b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", i.Dst, fromLLVM, val, toLLVM))
			}
		} else {
			// 配列→整数: 先頭要素を抽出
			e.b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", i.Dst, fromLLVM, val))
		}
		return
	}
	if strings.HasPrefix(toLLVM, "[") && rFrom > 0 {
		// 整数→配列: ゼロ初期化配列の要素0に挿入。要素型は rFrom(ビット数)から決定。
		elemLLVM := "i32"
		if rFrom >= 64 {
			elemLLVM = "i64"
		}
		e.b.WriteString(fmt.Sprintf("  %s = insertvalue %s zeroinitializer, %s %s, 0\n", i.Dst, toLLVM, elemLLVM, val))
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
				if typStr == "i32" {
					e.b.WriteString(fmt.Sprintf("  ret i32 %s\n", val))
				} else {
					truncReg := e.nextTmp()
					e.b.WriteString(fmt.Sprintf("  %s = trunc %s %s to i32\n", truncReg, typStr, val))
					e.b.WriteString(fmt.Sprintf("  ret i32 %s\n", truncReg))
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
		return "0"
	}
	intLLVM := sema.TypeInt.LLVMType()
	switch val := v.(type) {
	case *hir.ConstZero:
		return "zeroinitializer"
	case *hir.ConstString:
		tmp := e.nextTmp()
		e.b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* @%s, %s 0, %s 0\n",
			tmp, val.Length, val.Length, val.Label, intLLVM, intLLVM))
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
