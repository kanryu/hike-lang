package llvm

import (
	"fmt"
	"strings"

	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

type Emitter struct {
	prog         *hir.Program
	semaCtx      *sema.Context
	targetTriple string
	b            strings.Builder
	regCount     int
}

func New(prog *hir.Program, semaCtx *sema.Context, targetTriple string) *Emitter {
	if targetTriple == "" {
		targetTriple = "x86_64-unknown-linux-gnu"
	}
	return &Emitter{
		prog:         prog,
		semaCtx:      semaCtx,
		targetTriple: targetTriple,
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
			concreteParams := []string{fmt.Sprintf("%%struct.%s*", sName)}
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
	// モジュール内で実際に call されている外部シンボルを収集
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
			switch fn.Name {
			case "malloc", "free", "calloc", "strcmp", "strlen", "memcpy", "memcmp", "printf":
				continue
			}

			// 追加: cfunc エイリアス宣言で、かつ Hike 内部から一度も call されていない外部 C シンボルは出力しない
			// （Goアセンブリ側から直接呼ばれるため、LLVM IR 側に UNDEF シンボルを残さない安全策）
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
