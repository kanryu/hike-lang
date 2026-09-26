package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/logger"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

type ExprLowerer struct {
	root *Lowerer
}

func NewExprLowerer(root *Lowerer) *ExprLowerer {
	return &ExprLowerer{root: root}
}

func (e *ExprLowerer) lookupGlobal(name string) (string, sema.Type, bool) {
	if g, ok := e.root.semaCtx.Globals[name]; ok {
		return name, g, true
	}
	targetSuffix := "_" + name
	for gName, gt := range e.root.semaCtx.Globals {
		if strings.HasSuffix(gName, targetSuffix) {
			return gName, gt, true
		}
	}
	return "", nil, false
}

func (e *ExprLowerer) resolveTypeFromExpr(expr ast.Expression) sema.Type {
	if expr == nil {
		return nil
	}
	if cast, ok := expr.(*ast.ImplicitCastExpr); ok {
		return e.resolveTypeFromExpr(cast.Expr)
	}
	if te, ok := expr.(ast.TypeExpr); ok {
		return e.root.semaCtx.ResolveType(te)
	}
	if id, ok := expr.(*ast.Identifier); ok {
		if t, okT := sema.LookupBuiltinType(astIDValue(id)); okT {
			return t
		}
		if astIDValue(id) == "any" {
			return &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
		}
		if astIDValue(id) == "error" {
			return e.root.semaCtx.Interfaces["error"]
		}
		if st, _ := e.root.semaCtx.LookupStruct(astIDValue(id)); st != nil {
			return st
		}
		if iface, _ := e.root.semaCtx.LookupInterface(astIDValue(id)); iface != nil {
			return iface
		}
		if alias, _ := e.root.semaCtx.LookupAlias(astIDValue(id)); alias != nil {
			return alias
		}
	}
	// 1. パッケージ修飾型名 (例: list.List)
	if mem, ok := expr.(*ast.MemberExpr); ok {
		if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
			qualified := pkgId.Value + "_" + mem.Field.Value
			if st, _ := e.root.semaCtx.LookupStruct(qualified); st != nil {
				return st
			}
			if iface, _ := e.root.semaCtx.LookupInterface(qualified); iface != nil {
				return iface
			}
			if alias, _ := e.root.semaCtx.LookupAlias(qualified); alias != nil {
				return alias
			}
			return e.root.semaCtx.ResolveType(&ast.NamedType{
				Token:   mem.Token,
				Package: pkgId,
				Name:    mem.Field,
			})
		}
	}
	// 2. 添字構文によるジェネリクス型指定 (例: List[int] や list.List[int])
	if idxExpr, ok := expr.(*ast.IndexExpr); ok {
		var pkgId *ast.Identifier
		var typeId *ast.Identifier
		if id, okId := idxExpr.Left.(*ast.Identifier); okId {
			typeId = id
		} else if mem, okMem := idxExpr.Left.(*ast.MemberExpr); okMem {
			if p, okP := mem.Object.(*ast.Identifier); okP {
				pkgId = p
				typeId = mem.Field
			}
		}

		if typeId != nil {
			var typeArgs []ast.TypeExpr
			if te, okTe := idxExpr.Index.(ast.TypeExpr); okTe {
				typeArgs = append(typeArgs, te)
			} else if id, okId := idxExpr.Index.(*ast.Identifier); okId {
				typeArgs = append(typeArgs, &ast.NamedType{Token: id.Token, Name: id})
			} else if mem, okMem := idxExpr.Index.(*ast.MemberExpr); okMem {
				if p, okP := mem.Object.(*ast.Identifier); okP {
					typeArgs = append(typeArgs, &ast.NamedType{Token: mem.Token, Package: p, Name: mem.Field})
				}
			}

			return e.root.semaCtx.ResolveType(&ast.NamedType{
				Token:    idxExpr.Token,
				Package:  pkgId,
				Name:     typeId,
				TypeArgs: typeArgs,
			})
		}
	}
	// 3. GenericInstExpr によるジェネリクス型指定
	if gen, ok := expr.(*ast.GenericInstExpr); ok {
		var pkgId *ast.Identifier
		var typeId *ast.Identifier
		if id, okId := gen.Left.(*ast.Identifier); okId {
			typeId = id
		} else if mem, okMem := gen.Left.(*ast.MemberExpr); okMem {
			if p, okP := mem.Object.(*ast.Identifier); okP {
				pkgId = p
				typeId = mem.Field
			}
		}
		if typeId != nil {
			return e.root.semaCtx.ResolveType(&ast.NamedType{
				Token:    gen.Token,
				Package:  pkgId,
				Name:     typeId,
				TypeArgs: gen.TypeArgs,
			})
		}
	}
	if pref, ok := expr.(*ast.PrefixExpr); ok && pref.Operator == "*" {
		base := e.resolveTypeFromExpr(pref.Right)
		if base != nil && base != sema.TypeVoid {
			return &sema.PointerType{Base: base}
		}
	}
	return nil
}

// -------------------------------------------------------------
// 式 (Expression) の評価
// -------------------------------------------------------------

func (e *ExprLowerer) LowerExpr(expr ast.Expression) hir.Value {
	if expr == nil {
		return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
	}
	restoreLocation := e.root.setTokenLocation(e.root.sourceFile, expressionToken(expr))
	defer restoreLocation()

	switch node := expr.(type) {
	case *ast.IntegerLiteral:
		return &hir.ConstInt{Val: node.Value, Typ: sema.TypeInt}

	case *ast.FloatLiteral:
		return &hir.ConstFloat{Val: node.Value, Typ: sema.TypeFloat64}

	// 文字リテラルは文字列定数ではなく、byte (i8) 整数として評価する
	case *ast.CharLiteral:
		return &hir.ConstInt{Val: int64(node.CodePoint), Typ: sema.TypeByte}

	case *ast.StringLiteral:
		return e.root.getStringConst(node.Value)

	case *ast.NilLiteral:
		return &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}

	case *ast.InlineAsmExpr:
		args := make([]hir.Value, len(node.Operands))
		for i, operand := range node.Operands {
			args[i] = e.LowerExpr(operand)
		}
		e.root.emit(&hir.InstrInlineAsm{Template: node.Template, OutputConstraints: node.OutputConstraints, InputConstraints: node.InputConstraints, ClobberConstraints: node.ClobberConstraints, Args: args})
		return &hir.ConstInt{Val: 0, Typ: sema.TypeVoid}

	case *ast.ImplicitCastExpr:
		return e.lowerImplicitCast(node)

	case *ast.GenericInstExpr:
		var baseName string
		if id, ok := node.Left.(*ast.Identifier); ok {
			baseName = astIDValue(id)
		} else if mem, ok := node.Left.(*ast.MemberExpr); ok {
			if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
				baseName = pkgId.Value + "_" + mem.Field.Value
			} else {
				baseName = mem.Field.Value
			}
		}
		if baseName != "" {
			typeArgs := make([]sema.Type, len(node.TypeArgs))
			for i, ta := range node.TypeArgs {
				typeArgs[i] = e.root.semaCtx.ResolveType(ta)
			}
			specName, specFn := e.root.Call.getOrSpecializeFunc(baseName, typeArgs)
			if specFn != nil {
				fatType := specFn
				t1 := e.root.nextReg(fatType)
				e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(fatType), Val: &hir.GlobalVar{Name: specName, Typ: &sema.PointerType{Base: sema.TypeByte}}, Index: 0})
				t2 := e.root.nextReg(fatType)
				e.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}, Index: 1})
				return t2
			}
		}
		return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}

	case *ast.Identifier:
		return e.lowerIdentifier(node)

	case *ast.BinaryExpr:
		return e.LowerBinaryExpr(node)

	case *ast.PrefixExpr:
		return e.lowerPrefixExpr(node)

	case *ast.StructLiteral:
		allocaReg := e.lowerStructLiteralPtr(node)
		stType := allocaReg.Type().(*sema.PointerType).Base
		resReg := e.root.nextReg(stType)
		e.root.emit(&hir.InstrLoad{Dst: resReg, Ptr: allocaReg})
		return resReg

	case *ast.MapLiteral:
		return e.lowerMapLiteral(node)

	case *ast.ArrayLiteral:
		allocaReg := e.lowerArrayLiteralPtr(node)
		arType := allocaReg.Type().(*sema.PointerType).Base
		resReg := e.root.nextReg(arType)
		e.root.emit(&hir.InstrLoad{Dst: resReg, Ptr: allocaReg})
		return resReg

	case *ast.SliceLiteral:
		return e.lowerSliceLiteral(node)

	case *ast.SliceExpr:
		return e.lowerSliceExpr(node)
	case *ast.CallExpr:
		// sizeof 組み込みサポート (型名・変数・ポインタ構造体に対応)
		if id, ok := node.Function.(*ast.Identifier); ok && astIDValue(id) == "sizeof" {
			return e.lowerSizeofExprCall(node)
		}
		return e.root.Call.LowerCall(node)

	case *ast.MemberExpr:
		return e.lowerMemberExpr(node)

	case *ast.IndexExpr:
		return e.LowerIndexExpr(node)

	case *ast.TypeAssertExpr:
		tup := e.LowerTypeAssertExprWithPanic(node)
		targetType := e.root.semaCtx.ResolveType(node.Target)
		valReg := e.root.nextReg(targetType)
		e.root.emit(&hir.InstrExtractValue{Dst: valReg, Agg: tup, Index: 0})
		return valReg

	case *ast.FuncLit:
		return e.root.Call.LowerFuncLit(node)

	case *ast.AsyncExpr:
		return e.LowerAsyncExpr(node)

	case *ast.ReceiveExpr:
		return e.LowerReceiveExpr(node)
	}

	return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
}

func expressionToken(expr ast.Expression) token.Token {
	switch node := expr.(type) {
	case *ast.Identifier:
		return node.Token
	case *ast.IntegerLiteral:
		return node.Token
	case *ast.FloatLiteral:
		return node.Token
	case *ast.CharLiteral:
		return node.Token
	case *ast.StringLiteral:
		return node.Token
	case *ast.NilLiteral:
		return node.Token
	case *ast.PrefixExpr:
		return node.Token
	case *ast.ReceiveExpr:
		return node.Token
	case *ast.AsyncExpr:
		return node.Token
	case *ast.BinaryExpr:
		return node.Token
	case *ast.IndexExpr:
		return node.Token
	case *ast.GenericInstExpr:
		return node.Token
	case *ast.MemberExpr:
		return node.Token
	case *ast.CallExpr:
		return node.Token
	case *ast.InlineAsmExpr:
		return node.Token
	case *ast.IotaExpr:
		return node.Token
	case *ast.SliceExpr:
		return node.Token
	case *ast.SliceLiteral:
		return node.Token
	case *ast.StructLiteral:
		return node.Token
	case *ast.MapLiteral:
		return node.Token
	case *ast.ArrayLiteral:
		return node.Token
	case *ast.TypeAssertExpr:
		return node.Token
	case *ast.FuncLit:
		return node.Token
	default:
		return token.Token{}
	}
}

func (e *ExprLowerer) lowerMemberExpr(node *ast.MemberExpr) hir.Value {
	if pkgId, okPkg := node.Object.(*ast.Identifier); okPkg {
		qualified := pkgId.Value + "_" + node.Field.Value
		if c, ok := e.root.semaCtx.LookupStringConstant(qualified); ok {
			return e.root.getStringConst(c)
		}
		if c, ok := e.root.semaCtx.LookupConstant(qualified); ok {
			return &hir.ConstInt{Val: c, Typ: sema.TypeInt}
		}
		if c, ok := e.root.semaCtx.LookupFloatConstant(qualified); ok {
			return &hir.ConstFloat{Val: c, Typ: sema.TypeFloat64}
		}
		if g, ok := e.root.semaCtx.Globals[qualified]; ok {
			dst := e.root.nextReg(g)
			e.root.emit(&hir.InstrLoad{Dst: dst, Ptr: &hir.GlobalVar{Name: qualified, Typ: &sema.PointerType{Base: g}}})
			return dst
		}
		if fn, canonical := e.root.semaCtx.LookupFunction(qualified); fn != nil {
			fatType := fn
			callee := canonical
			if semaFuncCFunc(fn) && semaFuncCFuncAst(fn) != nil && !semaFuncCFuncAst(fn).IsAlias() {
				callee = "__hike_impl_" + semaFuncName(fn)
			} else if semaFuncExtern(fn) && semaFuncIRName(fn) != "" {
				callee = semaFuncIRName(fn)
			}
			t1 := e.root.nextReg(fatType)
			e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(fatType), Val: &hir.GlobalVar{Name: callee, Typ: &sema.PointerType{Base: sema.TypeByte}}, Index: 0})
			t2 := e.root.nextReg(fatType)
			e.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}, Index: 1})
			return t2
		}
		_, isLocal := e.root.symbols[pkgId.Value]
		if e.root.semaCtx.GoHikeMode && !isLocal && (pkgId.Value == "token" || pkgId.Value == "runtime") {
			return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
		}
		if e.root.semaCtx.GoHikeMode && !isLocal && pkgId.Value == "logger" {
			return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
		}
		if e.root.semaCtx.GoHikeMode && pkgId.Value == "os" &&
			(node.Field.Value == "Stdin" || node.Field.Value == "Stdout" || node.Field.Value == "Stderr") {
			return &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}
		}
		if e.root.semaCtx.GoHikeMode && pkgId.Value == "filepath" && node.Field.Value == "Separator" {
			return e.root.getStringConst("/")
		}
	}

	basePtr := e.LowerStructPtr(node.Object)
	baseType := basePtr.Type().(*sema.PointerType).Base
	st, sName := e.root.findStruct(baseType)
	if st != nil {
		if fieldPtr, fieldType, _, found := e.ResolveFieldPath(st, sName, basePtr, node.Field.Value); found {
			dst := e.root.nextReg(fieldType)
			e.root.emit(&hir.InstrLoad{Dst: dst, Ptr: fieldPtr})
			return dst
		}
	}

	// メソッド探索 (Method Value / バウンドメソッド)
	if targetFnName, targetFn, finalRecv, found := e.root.Call.ResolveMethod(baseType, node.Field.Value, basePtr); found && targetFn != nil {
		if finalRecv == nil {
			finalRecv = basePtr
		}
		methodParamTypes := []sema.Type{}
		if len(targetFn.ParamTypes) > 1 {
			methodParamTypes = targetFn.ParamTypes[1:]
		}
		boundFnType := &sema.FuncType{
			ParamTypes:   methodParamTypes,
			ReturnTypes:  targetFn.ReturnTypes,
			IsVariadic:   targetFn.IsVariadic,
			VariadicElem: semaFuncVariadicElem(targetFn),
		}

		var recvPtr hir.Value = finalRecv
		if _, isPtr := finalRecv.Type().(*sema.PointerType); !isPtr {
			allocaTmp := e.root.nextReg(&sema.PointerType{Base: finalRecv.Type()})
			e.root.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: finalRecv.Type()})
			e.root.emit(&hir.InstrStore{Val: finalRecv, Ptr: allocaTmp})
			recvPtr = allocaTmp
		}

		rawRecvPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e.root.emit(&hir.InstrCast{Dst: rawRecvPtr, Val: recvPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})

		callee := targetFnName
		if semaFuncCFunc(targetFn) && semaFuncCFuncAst(targetFn) != nil && !semaFuncCFuncAst(targetFn).IsAlias() {
			callee = "__hike_impl_" + semaFuncName(targetFn)
		} else if semaFuncExtern(targetFn) && semaFuncIRName(targetFn) != "" {
			callee = semaFuncIRName(targetFn)
		}

		fnGlobal := &hir.GlobalVar{Name: callee, Typ: &sema.PointerType{Base: sema.TypeByte}}
		t1 := e.root.nextReg(boundFnType)
		e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(boundFnType), Val: fnGlobal, Index: 0})
		t2 := e.root.nextReg(boundFnType)
		e.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: rawRecvPtr, Index: 1})
		return t2
	}

	file := e.root.sourceFile
	if file == "" {
		file = "input.hike"
	}
	panic(fmt.Sprintf("%s:%d:%d: field or method '%s' not found on type '%s'", file, node.Field.Token.Line, node.Field.Token.Col, node.Field.Value, semaTypeName(baseType)))
}

func (e *ExprLowerer) lowerSizeofExprCall(node *ast.CallExpr) hir.Value {
	logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: ast=%T (%+v)\n", node, node)
	if len(node.Args) == 1 {
		arg := node.Args[0]
		if cast, ok := arg.(*ast.ImplicitCastExpr); ok {
			arg = cast.Expr
		}
		logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: arg=%T (%+v)\n", arg, arg)
		t := e.resolveTypeFromExpr(arg)
		if t == nil || t == sema.TypeVoid {
			argVal := e.LowerExpr(arg)
			t = argVal.Type()
		}
		if t != nil && t != sema.TypeVoid {
			if pt, isPtr := t.(*sema.PointerType); isPtr {
				t = pt.Base
			}
			sz := int64(sema.SizeOf(t))
			if st, _ := e.root.findStruct(t); st != nil {
				stSz := int64(st.Size())
				if stSz > sz {
					sz = stSz
				}
				if sz <= int64(sema.PointerSize) && strings.Contains(st.Name, "__") {
					baseName := strings.Split(strings.TrimPrefix(st.Name, "*"), "__")[0]
					if baseSt, _ := e.root.findStructByName(baseName); baseSt != nil && len(baseSt.Fields) > 0 {
						baseSz := int64(baseSt.Size())
						if baseSz > sz {
							sz = baseSz
						}
					}
				}
				logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: struct '%s', size = %d\n", st.Name, sz)
			} else {
				logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: type '%s', size = %d\n", semaTypeName(t), sz)
			}
			logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: folded to %d\n", sz)
			return &hir.ConstInt{Val: sz, Typ: sema.TypeInt}
		}
	}
	logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: FAILED to resolve, returning 0\n")
	return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
}

func (e *ExprLowerer) lowerSliceExpr(node *ast.SliceExpr) hir.Value {
	baseVal := e.LowerExpr(node.Left)
	baseType := baseVal.Type()

	if value, handled := e.lowerStringSliceExpr(node, baseVal, baseType); handled {
		return value
	}

	// ユーザー定義コレクション構造体の Sliceable (Slice(low, high int))
	objPtr := e.LowerStructPtr(node.Left)
	sliceFnName, sliceFn, finalRecv, found := e.root.Call.ResolveMethod(baseType, "Slice", objPtr)
	if strings.Contains(semaTypeName(baseType), "__") {
		parts := strings.SplitN(strings.TrimPrefix(semaTypeName(baseType), "*"), "__", 2)
		baseName := parts[0]
		typeSuffix := parts[1]
		specSlice := fmt.Sprintf("%s_Slice_%s", baseName, typeSuffix)
		if fn, ok := e.root.semaCtx.Functions[specSlice]; ok {
			sliceFnName = specSlice
			sliceFn = fn
			found = true
		}
	}
	if !found && strings.Contains(semaTypeName(baseType), "__") {
		baseName := strings.Split(strings.TrimPrefix(semaTypeName(baseType), "*"), "__")[0]
		if st, _ := e.root.semaCtx.LookupStruct(baseName); st != nil {
			sliceFnName, sliceFn, finalRecv, found = e.root.Call.ResolveMethod(st, "Slice", objPtr)
		}
	}

	if found && sliceFnName != "" {
		if finalRecv == nil {
			finalRecv = objPtr
		}
		if sliceFn != nil && len(sliceFn.ParamTypes) > 0 {
			_, isPtrExpected := sliceFn.ParamTypes[0].(*sema.PointerType)
			_, isPtrActual := finalRecv.Type().(*sema.PointerType)
			if isPtrExpected && !isPtrActual {
				allocaTmp := e.root.nextReg(&sema.PointerType{Base: finalRecv.Type()})
				e.root.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: finalRecv.Type()})
				e.root.emit(&hir.InstrStore{Val: finalRecv, Ptr: allocaTmp})
				finalRecv = allocaTmp
			} else if !isPtrExpected && isPtrActual {
				ptrType := finalRecv.Type().(*sema.PointerType)
				loadReg := e.root.nextReg(ptrType.Base)
				e.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: finalRecv})
				finalRecv = loadReg
			}
		}

		lowVal := hir.Value(&hir.ConstInt{Val: 0, Typ: sema.TypeInt})
		if node.Low != nil {
			lowVal = e.LowerExpr(node.Low)
			lowVal = e.root.emitValueCoerce(lowVal, sema.TypeInt)
		}
		var highVal hir.Value
		if node.High != nil {
			highVal = e.LowerExpr(node.High)
			highVal = e.root.emitValueCoerce(highVal, sema.TypeInt)
		} else {
			lenFnName, lenFn, lenRecv, lenFound := e.root.Call.ResolveMethod(baseType, "Len", objPtr)
			if strings.Contains(semaTypeName(baseType), "__") {
				parts := strings.SplitN(strings.TrimPrefix(semaTypeName(baseType), "*"), "__", 2)
				baseName := parts[0]
				typeSuffix := parts[1]
				specLen := fmt.Sprintf("%s_Len_%s", baseName, typeSuffix)
				if fn, ok := e.root.semaCtx.Functions[specLen]; ok {
					lenFnName = specLen
					lenFn = fn
					lenFound = true
				}
			}
			if lenFound && lenFn != nil {
				if lenRecv == nil {
					lenRecv = objPtr
				}
				if len(lenFn.ParamTypes) > 0 {
					_, isPtrExpected := lenFn.ParamTypes[0].(*sema.PointerType)
					_, isPtrActual := lenRecv.Type().(*sema.PointerType)
					if isPtrExpected && !isPtrActual {
						allocaTmp := e.root.nextReg(&sema.PointerType{Base: lenRecv.Type()})
						e.root.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: lenRecv.Type()})
						e.root.emit(&hir.InstrStore{Val: lenRecv, Ptr: allocaTmp})
						lenRecv = allocaTmp
					} else if !isPtrExpected && isPtrActual {
						ptrType := lenRecv.Type().(*sema.PointerType)
						loadReg := e.root.nextReg(ptrType.Base)
						e.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: lenRecv})
						lenRecv = loadReg
					}
				}
				lenReg := e.root.nextReg(sema.TypeInt)
				e.root.emit(&hir.InstrCallStatic{Dst: lenReg, CalleeName: lenFnName, Args: []hir.Value{lenRecv}})
				highVal = lenReg
			} else {
				highVal = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
			}
		}

		var retType sema.Type = sema.TypeVoid
		if sliceFn != nil {
			if len(sliceFn.ReturnTypes) == 1 {
				retType = sliceFn.ReturnTypes[0]
			} else if len(sliceFn.ReturnTypes) > 1 {
				retType = &sema.TupleType{Types: sliceFn.ReturnTypes}
			}
		}
		if retType == sema.TypeVoid {
			retType = baseType
		}

		resReg := e.root.nextReg(retType)
		e.root.emit(&hir.InstrCallStatic{
			Dst:        resReg,
			CalleeName: sliceFnName,
			Args:       []hir.Value{finalRecv, lowVal, highVal},
		})

		// 戻り値がタプル (*List[T], bool) の場合、式コンテキストでは第0要素を抽出
		if tup, isTup := retType.(*sema.TupleType); isTup && len(tup.Types) > 0 {
			valReg := e.root.nextReg(tup.Types[0])
			e.root.emit(&hir.InstrExtractValue{
				Dst:   valReg,
				Agg:   resReg,
				Index: 0,
			})
			return valReg
		}

		return resReg
	}

	var elemType sema.Type = sema.TypeByte
	var typedDataPtr hir.Value = nil
	var capVal hir.Value = nil

	if slType, isSlice := baseType.(*sema.SliceType); isSlice {
		elemType = slType.Elem
		rawBytePtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		cVal := e.root.nextReg(sema.TypeInt)
		e.root.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: baseVal, Index: 0})
		e.root.emit(&hir.InstrExtractValue{Dst: cVal, Agg: baseVal, Index: 2})
		tPtr := e.root.nextReg(&sema.PointerType{Base: elemType})
		e.root.emit(&hir.InstrCast{Dst: tPtr, Val: rawBytePtr, ToType: &sema.PointerType{Base: elemType}})
		typedDataPtr = tPtr
		capVal = cVal
	} else if arType, isArray := baseType.(*sema.ArrayType); isArray {
		elemType = arType.Elem
		arrPtr := e.LowerLValue(node.Left)
		tPtr := e.root.nextReg(&sema.PointerType{Base: elemType})
		e.root.emit(&hir.InstrCast{Dst: tPtr, Val: arrPtr, ToType: &sema.PointerType{Base: elemType}})
		typedDataPtr = tPtr
		capVal = &hir.ConstInt{Val: int64(arType.Len), Typ: sema.TypeInt}
	} else if baseType == sema.TypeCString {
		// cstring indexing/slicing operates on its NUL-terminated byte
		// buffer, with strlen providing the implicit capacity.
		typedDataPtr = baseVal
		lenReg := e.root.nextReg(sema.TypeInt)
		e.root.emit(&hir.InstrCallStatic{Dst: lenReg, CalleeName: e.root.BuiltinName("strlen"), Args: []hir.Value{baseVal}})
		capVal = lenReg
	} else if ptrType, isBytePtr := baseType.(*sema.PointerType); isBytePtr && ptrType.Base == sema.TypeByte {
		typedDataPtr = baseVal
		lenReg := e.root.nextReg(sema.TypeInt)
		e.root.emit(&hir.InstrCallStatic{Dst: lenReg, CalleeName: e.root.BuiltinName("strlen"), Args: []hir.Value{baseVal}})
		capVal = lenReg
	} else {
		panic(fmt.Sprintf("[Lower Error] cannot slice type %s", semaTypeName(baseType)))
	}

	lowVal := hir.Value(&hir.ConstInt{Val: 0, Typ: sema.TypeInt})
	if node.Low != nil {
		lowVal = e.LowerExpr(node.Low)
	}
	highVal := hir.Value(capVal)
	if node.High != nil {
		highVal = e.LowerExpr(node.High)
	}

	elemPtr := e.root.nextReg(&sema.PointerType{Base: elemType})
	e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedDataPtr, Index: lowVal})
	newLen := e.root.nextReg(sema.TypeInt)
	e.root.emit(&hir.InstrBinary{Dst: newLen, Op: hir.OpSub, L: highVal, R: lowVal})
	newCap := e.root.nextReg(sema.TypeInt)
	e.root.emit(&hir.InstrBinary{Dst: newCap, Op: hir.OpSub, L: capVal, R: lowVal})

	if baseType == sema.TypeCString || semaTypeName(baseType) == "cstring" {
		// A string view stores the original payload pointer and a byte
		// offset.  Do not pass elemPtr here: that already points at the
		// sliced element and would make the offset relative to the wrong
		// base (and, for cstrings, lose the NUL-terminated buffer).
		return e.root.makeStringView(baseVal, lowVal, newLen)
	}

	elemBytePtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	e.root.emit(&hir.InstrCast{Dst: elemBytePtr, Val: elemPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})

	resSliceType := &sema.SliceType{Elem: elemType}
	t1 := e.root.nextReg(resSliceType)
	e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(resSliceType), Val: elemBytePtr, Index: 0})
	t2 := e.root.nextReg(resSliceType)
	e.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: newLen, Index: 1})
	t3 := e.root.nextReg(resSliceType)
	e.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: newCap, Index: 2})
	return t3
}

func (e *ExprLowerer) lowerStringSliceExpr(node *ast.SliceExpr, baseVal hir.Value, baseType sema.Type) (hir.Value, bool) {
	if baseType != sema.TypeString && semaTypeName(baseType) != "string" && baseType != sema.TypeCString && semaTypeName(baseType) != "cstring" {
		return nil, false
	}
	if baseType == sema.TypeString || semaTypeName(baseType) == "string" {
		basePtr, baseOffset, baseLen32 := e.root.stringViewParts(baseVal)
		baseLen := hir.Value(baseLen32)
		if sema.LLVMTypeOf(sema.TypeInt) != sema.LLVMTypeOf(sema.TypeInt32) {
			baseLen64 := e.root.nextReg(sema.TypeInt)
			e.root.emit(&hir.InstrCast{Dst: baseLen64, Val: baseLen32, ToType: sema.TypeInt})
			baseLen = baseLen64
		}
		lowVal := hir.Value(&hir.ConstInt{Val: 0, Typ: sema.TypeInt})
		if node.Low != nil {
			lowVal = e.LowerExpr(node.Low)
		}
		highVal := hir.Value(baseLen)
		if node.High != nil {
			highVal = e.LowerExpr(node.High)
		}
		newOffset := e.root.nextReg(sema.TypeInt32)
		low32 := e.root.emitValueCoerce(lowVal, sema.TypeInt32)
		e.root.emit(&hir.InstrBinary{Dst: newOffset, Op: hir.OpAdd, L: baseOffset, R: low32})
		length := e.root.nextReg(sema.TypeInt)
		e.root.emit(&hir.InstrBinary{Dst: length, Op: hir.OpSub, L: highVal, R: lowVal})
		view := e.root.makeStringView(basePtr, newOffset, length)
		e.root.retainString(view)
		return view, true
	}
	lowVal := hir.Value(&hir.ConstInt{Val: 0, Typ: sema.TypeInt})
	if node.Low != nil {
		lowVal = e.LowerExpr(node.Low)
	}
	highVal := hir.Value(e.root.nextReg(sema.TypeInt))
	if node.High != nil {
		highVal = e.LowerExpr(node.High)
	} else {
		e.root.emit(&hir.InstrCallStatic{Dst: highVal.(*hir.Reg), CalleeName: e.root.BuiltinName("strlen"), Args: []hir.Value{baseVal}})
	}
	length := e.root.nextReg(sema.TypeInt)
	e.root.emit(&hir.InstrBinary{Dst: length, Op: hir.OpSub, L: highVal, R: lowVal})
	if baseType == sema.TypeCString || semaTypeName(baseType) == "cstring" {
		return e.root.makeStringView(baseVal, lowVal, length), true
	}
	raw := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	e.root.emit(&hir.InstrCallStatic{Dst: raw, CalleeName: e.root.BuiltinName("hike_substr"), Args: []hir.Value{baseVal, lowVal, highVal}})
	return e.root.makeString(raw, length), true
}

func (e *ExprLowerer) lowerPrefixExpr(node *ast.PrefixExpr) hir.Value {
	if node.Operator == "&" {
		// 構造体リテラルのポインタ化 (&Struct{}) はヒープ領域 (calloc) に確保
		if sl, ok := node.Right.(*ast.StructLiteral); ok {
			return e.lowerStructLiteralHeap(sl)
		}
		return e.LowerLValue(node.Right)
	}
	if node.Operator == "*" {
		ptrVal := e.LowerExpr(node.Right)
		baseType := ptrVal.Type().(*sema.PointerType).Base
		dst := e.root.nextReg(baseType)
		e.root.emit(&hir.InstrLoad{Dst: dst, Ptr: ptrVal})
		return dst
	}
	val := e.LowerExpr(node.Right)
	if val == nil {
		if node.Operator == "!" {
			return &hir.ConstInt{Val: 1, Typ: sema.TypeBool}
		}
		panic(fmt.Sprintf("[Lower Error] unary operator '%s' produced no value for %T", node.Operator, node.Right))
	}
	if reg, isReg := val.(*hir.Reg); isReg && reg == nil {
		if node.Operator == "!" {
			return &hir.ConstInt{Val: 1, Typ: sema.TypeBool}
		}
		panic(fmt.Sprintf("[Lower Error] unary operator '%s' produced a nil register for %T at %d:%d", node.Operator, node.Right, node.Token.Line, node.Token.Col))
	}
	if val.Type() == nil {
		panic(fmt.Sprintf("[Lower Error] unary operator '%s' produced an untyped value for %T at %d:%d", node.Operator, node.Right, node.Token.Line, node.Token.Col))
	}
	if tup, isTup := val.Type().(*sema.TupleType); isTup && len(tup.Types) > 0 {
		elem0 := e.root.nextReg(tup.Types[0])
		e.root.emit(&hir.InstrExtractValue{Dst: elem0, Agg: val, Index: 0})
		val = elem0
	}
	op := hir.OpNeg
	if node.Operator == "!" {
		op = hir.OpNot
	}
	dst := e.root.nextReg(val.Type())
	e.root.emit(&hir.InstrUnary{Dst: dst, Op: op, Val: val})
	return dst
}

func (e *ExprLowerer) lowerSliceLiteral(node *ast.SliceLiteral) hir.Value {
	slType := e.root.semaCtx.ResolveType(node.Type).(*sema.SliceType)
	count := len(node.Elements)
	elemSize := sema.SizeOf(slType.Elem)
	if elemSize <= 0 {
		elemSize = 1
	}
	totalBytes := count * elemSize

	mallocRaw := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	e.root.emit(&hir.InstrHeapAlloc{Dst: mallocRaw, Size: &hir.ConstInt{Val: int64(totalBytes), Typ: sema.TypeInt}, AllocType: sema.TypeByte, KeepOnHeapInArea: true})

	typedBase := e.root.nextReg(&sema.PointerType{Base: slType.Elem})
	e.root.emit(&hir.InstrCast{Dst: typedBase, Val: mallocRaw, ToType: &sema.PointerType{Base: slType.Elem}})

	for i, el := range node.Elements {
		val := e.LowerExpr(el)
		val = e.root.emitValueCoerce(val, slType.Elem)
		elemPtr := e.root.nextReg(&sema.PointerType{Base: slType.Elem})
		e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedBase, Index: &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt}})
		e.root.emit(&hir.InstrStore{Val: val, Ptr: elemPtr})
	}

	t1 := e.root.nextReg(slType)
	e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(slType), Val: mallocRaw, Index: 0})
	t2 := e.root.nextReg(slType)
	e.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: &hir.ConstInt{Val: int64(count), Typ: sema.TypeInt}, Index: 1})
	t3 := e.root.nextReg(slType)
	e.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: &hir.ConstInt{Val: int64(count), Typ: sema.TypeInt}, Index: 2})
	return t3
}

func (e *ExprLowerer) lowerIdentifier(node *ast.Identifier) hir.Value {
	name := astIDValue(node)
	if name == "true" {
		return &hir.ConstBool{Val: true, Typ: sema.TypeBool}
	}
	if name == "false" {
		return &hir.ConstBool{Val: false, Typ: sema.TypeBool}
	}
	if c, ok := e.root.semaCtx.LookupStringConstant(name); ok {
		return e.root.getStringConst(c)
	}
	if c, ok := e.root.semaCtx.LookupConstant(name); ok {
		return &hir.ConstInt{Val: c, Typ: sema.TypeInt}
	}
	if c, ok := e.root.semaCtx.LookupFloatConstant(name); ok {
		return &hir.ConstFloat{Val: c, Typ: sema.TypeFloat64}
	}
	if ptr, ok := e.root.symbols[name]; ok {
		ptrType := ptr.Type().(*sema.PointerType)
		dst := e.root.nextReg(ptrType.Base)
		e.root.emit(&hir.InstrLoad{Dst: dst, Ptr: ptr})
		return dst
	}
	if gName, g, ok := e.lookupGlobal(name); ok {
		dst := e.root.nextReg(g)
		e.root.emit(&hir.InstrLoad{Dst: dst, Ptr: &hir.GlobalVar{Name: gName, Typ: &sema.PointerType{Base: g}}})
		return dst
	}

	lookupFn := func(name string) (*sema.FuncType, string) {
		if fn, ok := e.root.semaCtx.Functions[name]; ok {
			return fn, name
		}
		// パッケージ内の未修飾呼び出し（例: md5.Sum 内の
		// compress）は、現在の関数と同じパッケージを最優先する。
		// 全関数名をサフィックス検索すると、md5_compress と
		// sha256_compress のような同名関数の選択がmapの反復順に
		// 依存し、生成IRと実行結果が不定になる。
		if e.root.curFunc != nil {
			if sep := strings.IndexByte(e.root.curFunc.Name, '_'); sep > 0 {
				qualified := e.root.curFunc.Name[:sep] + "_" + name
				if fn, ok := e.root.semaCtx.Functions[qualified]; ok {
					return fn, qualified
				}
			}
		}
		targetSuffix := "_" + name
		for fnName, fn := range e.root.semaCtx.Functions {
			if strings.HasSuffix(fnName, targetSuffix) {
				return fn, fnName
			}
		}
		return nil, ""
	}

	if fn, canonicalName := lookupFn(name); fn != nil {
		fatType := fn
		callee := canonicalName
		if fn.IsCFunc && fn.CFuncAst != nil && !fn.CFuncAst.IsAlias() {
			callee = "__hike_impl_" + fn.Name
		} else if fn.IsExtern && fn.IRName != "" {
			callee = fn.IRName
		}
		t1 := e.root.nextReg(fatType)
		e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(fatType), Val: &hir.GlobalVar{Name: callee, Typ: &sema.PointerType{Base: sema.TypeByte}}, Index: 0})
		t2 := e.root.nextReg(fatType)
		e.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}, Index: 1})
		return t2
	}

	if sema.IsBuiltinType(name) {
		return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
	}
	if e.root.semaCtx.GoHikeMode && (name == "len" || name == "cap") {
		// Keep builtin names usable when the Go-shaped parser leaves the
		// callee as a standalone identifier; normal calls are handled by
		// CallLowerer below.
		return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
	}
	if e.root.semaCtx.GoHikeMode && (name == "token" || name == "runtime") {
		// Go-Hike package sources may use token constants in AST metadata.
		// They do not affect generated program behavior.
		return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
	}
	panic(fmt.Sprintf("[Lower Error] undefined identifier: %s", name))
}

func (e *ExprLowerer) lowerImplicitCast(node *ast.ImplicitCastExpr) hir.Value {
	targetType := e.root.semaCtx.ResolveType(node.TargetType)
	if char, ok := node.Expr.(*ast.CharLiteral); ok {
		if integerWidth(sema.LLVMTypeOf(targetType)) > 0 {
			return &hir.ConstInt{Val: int64(char.CodePoint), Typ: targetType}
		}
	}

	value := e.LowerExpr(node.Expr)
	if isNilValue(value) {
		return e.root.defaultConstValue(targetType)
	}
	if tuple, ok := value.Type().(*sema.TupleType); ok && len(tuple.Types) > 0 {
		elem := e.root.nextReg(tuple.Types[0])
		e.root.emit(&hir.InstrExtractValue{Dst: elem, Agg: value, Index: 0})
		value = elem
	}
	if sema.LLVMTypeOf(value.Type()) == sema.LLVMTypeOf(targetType) {
		return value
	}
	if iface, ok := targetType.(*sema.InterfaceType); ok {
		if isNilValue(value) {
			return e.root.defaultConstValue(iface)
		}
		if result := e.lowerInterfaceConversion(value, iface); result != nil {
			return result
		}
		itabName := ""
		typeID := int64(0)
		if !iface.IsAny() {
			itabName = e.root.Call.GetOrCreateItab(value.Type(), iface).GlobalName
		} else {
			typeID = value.Type().TypeID(e.root.semaCtx)
		}
		dst := e.root.nextReg(iface)
		e.root.emit(&hir.InstrBoxInterface{Dst: dst, Val: value, Iface: iface, ItabName: itabName, TypeID: typeID})
		return dst
	}
	dst := e.root.nextReg(targetType)
	e.root.emit(&hir.InstrCast{Dst: dst, Val: value, ToType: targetType})
	return dst
}

func integerWidth(llvmType string) int {
	switch llvmType {
	case "i64":
		return 64
	case "i32":
		return 32
	case "i16":
		return 16
	case "i8":
		return 8
	default:
		return 0
	}
}

// lowerInterfaceConversion handles conversions that preserve an interface
// value or recover its dynamic type when boxing it as any.
func (e *ExprLowerer) lowerInterfaceConversion(val hir.Value, target *sema.InterfaceType) hir.Value {
	source, ok := val.Type().(*sema.InterfaceType)
	if !ok {
		return nil
	}
	if !target.IsAny() {
		if semaTypeName(source) == semaTypeName(target) {
			return val
		}
		return nil
	}
	if source.IsAny() {
		return val
	}

	// Recover the dynamic concrete type from the source interface's itab before
	// constructing the any pair.
	data := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	itab := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	e.root.emit(&hir.InstrExtractValue{Dst: data, Agg: val, Index: 0})
	e.root.emit(&hir.InstrExtractValue{Dst: itab, Agg: val, Index: 1})
	typeIDPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeInt32})
	e.root.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itab, ToType: typeIDPtr.Typ})
	typeID := e.root.nextReg(sema.TypeInt32)
	e.root.emit(&hir.InstrLoad{Dst: typeID, Ptr: typeIDPtr})
	t1 := e.root.nextReg(target)
	e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(target), Val: typeID, Index: 0})
	dst := e.root.nextReg(target)
	e.root.emit(&hir.InstrInsertValue{Dst: dst, Agg: t1, Val: data, Index: 1})
	return dst
}

// lowerMapLiteral implements Go-compatible map[K]V{key: value, ...}
// initialization using the same runtime representation as make(map[K]V).
// Keeping construction here also makes map literals work in global
// initializers, which are lowered into the generated main function.
func (e *ExprLowerer) lowerMapLiteral(node *ast.MapLiteral) hir.Value {
	mapType := e.root.semaCtx.ResolveType(node.Type)
	mp, ok := mapType.(*sema.MapType)
	if !ok {
		panic("[Lower Error] map literal has non-map type")
	}

	isStr := int64(0)
	if mp.Key == sema.TypeString {
		isStr = 1
	}
	capVal := &hir.ConstInt{Val: int64(len(node.Entries)), Typ: sema.TypeInt}
	mapVal := e.root.nextReg(mp)
	e.root.emit(&hir.InstrCallStatic{
		Dst:        mapVal,
		CalleeName: "__hike_map_create",
		Args:       []hir.Value{capVal, &hir.ConstInt{Val: isStr, Typ: sema.TypeInt}},
	})

	for _, entry := range node.Entries {
		keyVal := e.root.Expr.LowerExpr(entry.Key)
		// Go permits eliding the value type in map literals (for example
		// map[string]runtimeFunc{"malloc": { ... }}).  The Hike AST keeps
		// that shorthand as a struct literal without Type; recover it from
		// the map's value type before lowering the value.
		if sl, ok := entry.Value.(*ast.StructLiteral); ok && sl.Type == nil {
			if st, isStruct := mp.Value.(*sema.StructType); isStruct {
				sl.Type = &ast.NamedType{
					Token: sl.Token,
					Name:  &ast.Identifier{Token: sl.Token, Value: st.Name},
				}
			}
		}
		valueVal := e.root.Expr.LowerExpr(entry.Value)
		keyI64 := e.root.coerceToI64(keyVal, mp.Key)
		valueI64 := e.root.coerceToI64(valueVal, mp.Value)
		e.root.emit(&hir.InstrCallStatic{
			CalleeName: "__hike_map_set",
			Args:       []hir.Value{mapVal, keyI64, valueI64},
		})
	}
	return mapVal
}

// LowerIndexExpr は添字式 (IndexExpr) を評価して HIR 値を生成する
