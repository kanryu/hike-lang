package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/logger"
	"hikec-go/pkg/sema"
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
	if te, ok := expr.(ast.TypeExpr); ok {
		return e.root.semaCtx.ResolveType(te)
	}
	if id, ok := expr.(*ast.Identifier); ok {
		if t, okT := sema.LookupBuiltinType(id.Value); okT {
			return t
		}
		if id.Value == "any" {
			return &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
		}
		if id.Value == "error" {
			return e.root.semaCtx.Interfaces["error"]
		}
		if st, _ := e.root.semaCtx.LookupStruct(id.Value); st != nil {
			return st
		}
		if iface, _ := e.root.semaCtx.LookupInterface(id.Value); iface != nil {
			return iface
		}
		if alias, _ := e.root.semaCtx.LookupAlias(id.Value); alias != nil {
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
		targetT := e.root.semaCtx.ResolveType(node.TargetType)

		if cl, ok := node.Expr.(*ast.CharLiteral); ok {
			intRank := func(llvm string) int {
				switch llvm {
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
			if intRank(targetT.LLVMType()) > 0 {
				return &hir.ConstInt{Val: int64(cl.CodePoint), Typ: targetT}
			}
		}

		val := e.LowerExpr(node.Expr)

		if tup, isTup := val.Type().(*sema.TupleType); isTup && len(tup.Types) > 0 {
			elem0 := e.root.nextReg(tup.Types[0])
			e.root.emit(&hir.InstrExtractValue{Dst: elem0, Agg: val, Index: 0})
			val = elem0
		}

		if val.Type().LLVMType() == targetT.LLVMType() {
			return val
		}
		if iface, ok := targetT.(*sema.InterfaceType); ok {
			if isNilValue(val) {
				return e.root.defaultConstValue(iface)
			}

			if srcIface, isSrcIface := val.Type().(*sema.InterfaceType); isSrcIface {
				if iface.IsAny() {
					dataPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
					e.root.emit(&hir.InstrExtractValue{Dst: dataPtr, Agg: val, Index: 0})
					typeIDReg := e.root.nextReg(sema.TypeInt)
					if !srcIface.IsAny() {
						itabPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
						e.root.emit(&hir.InstrExtractValue{Dst: itabPtr, Agg: val, Index: 1})
						typeIDPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
						e.root.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabPtr, ToType: &sema.PointerType{Base: sema.TypeInt}})
						e.root.emit(&hir.InstrLoad{Dst: typeIDReg, Ptr: typeIDPtr})
					} else {
						e.root.emit(&hir.InstrExtractValue{Dst: typeIDReg, Agg: val, Index: 1})
					}
					t1 := e.root.nextReg(iface)
					e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(iface), Val: dataPtr, Index: 0})
					dst := e.root.nextReg(iface)
					e.root.emit(&hir.InstrInsertValue{Dst: dst, Agg: t1, Val: typeIDReg, Index: 1})
					return dst
				}
				if srcIface.TypeName() == iface.TypeName() {
					return val
				}
			}

			itabName := ""
			if !iface.IsAny() {
				itabDef := e.root.Call.GetOrCreateItab(val.Type(), iface)
				itabName = itabDef.GlobalName
			}
			dst := e.root.nextReg(iface)
			e.root.emit(&hir.InstrBoxInterface{Dst: dst, Val: val, Iface: iface, ItabName: itabName})
			return dst
		}
		dst := e.root.nextReg(targetT)
		e.root.emit(&hir.InstrCast{Dst: dst, Val: val, ToType: targetT})
		return dst

	case *ast.GenericInstExpr:
		var baseName string
		if id, ok := node.Left.(*ast.Identifier); ok {
			baseName = id.Value
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
		if node.Value == "true" {
			return &hir.ConstBool{Val: true, Typ: sema.TypeBool}
		}
		if node.Value == "false" {
			return &hir.ConstBool{Val: false, Typ: sema.TypeBool}
		}
		if c, ok := e.root.semaCtx.LookupConstant(node.Value); ok {
			return &hir.ConstInt{Val: c, Typ: sema.TypeInt}
		}
		if c, ok := e.root.semaCtx.LookupFloatConstant(node.Value); ok {
			return &hir.ConstFloat{Val: c, Typ: sema.TypeFloat64}
		}
		if ptr, ok := e.root.symbols[node.Value]; ok {
			ptrType := ptr.Type().(*sema.PointerType)
			dst := e.root.nextReg(ptrType.Base)
			e.root.emit(&hir.InstrLoad{Dst: dst, Ptr: ptr})
			return dst
		}
		if gName, g, ok := e.lookupGlobal(node.Value); ok {
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

		if fn, canonicalName := lookupFn(node.Value); fn != nil {
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

		if sema.IsBuiltinType(node.Value) {
			return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
		}
		panic(fmt.Sprintf("[Lower Error] undefined identifier: %s", node.Value))

	case *ast.BinaryExpr:
		return e.LowerBinaryExpr(node)

	case *ast.PrefixExpr:
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
		slType := e.root.semaCtx.ResolveType(node.Type).(*sema.SliceType)
		count := len(node.Elements)
		elemSize := slType.Elem.Size()
		if elemSize <= 0 {
			elemSize = 1
		}
		totalBytes := count * elemSize

		mallocRaw := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e.root.emit(&hir.InstrHeapAlloc{Dst: mallocRaw, Size: &hir.ConstInt{Val: int64(totalBytes), Typ: sema.TypeInt}, AllocType: sema.TypeByte})

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

	case *ast.SliceExpr:
		baseVal := e.LowerExpr(node.Left)
		baseType := baseVal.Type()

		if baseType == sema.TypeString || baseType == sema.TypeCString {
			if baseType == sema.TypeString {
				basePtr, baseOffset, baseLen32 := e.root.stringViewParts(baseVal)
				baseLen := hir.Value(baseLen32)
				if sema.TypeInt.LLVMType() != sema.TypeInt32.LLVMType() {
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
				return view
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
			raw := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrCallStatic{Dst: raw, CalleeName: e.root.BuiltinName("hike_substr"), Args: []hir.Value{baseVal, lowVal, highVal}})
			length := e.root.nextReg(sema.TypeInt)
			e.root.emit(&hir.InstrBinary{Dst: length, Op: hir.OpSub, L: highVal, R: lowVal})
			return e.root.makeString(raw, length)
		}

		// ユーザー定義コレクション構造体の Sliceable (Slice(low, high int))
		objPtr := e.LowerStructPtr(node.Left)
		sliceFnName, sliceFn, finalRecv, found := e.root.Call.ResolveMethod(baseType, "Slice", objPtr)
		if strings.Contains(baseType.TypeName(), "__") {
			parts := strings.SplitN(strings.TrimPrefix(baseType.TypeName(), "*"), "__", 2)
			baseName := parts[0]
			typeSuffix := parts[1]
			specSlice := fmt.Sprintf("%s_Slice_%s", baseName, typeSuffix)
			if fn, ok := e.root.semaCtx.Functions[specSlice]; ok {
				sliceFnName = specSlice
				sliceFn = fn
				found = true
			}
		}
		if !found && strings.Contains(baseType.TypeName(), "__") {
			baseName := strings.Split(strings.TrimPrefix(baseType.TypeName(), "*"), "__")[0]
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
				if strings.Contains(baseType.TypeName(), "__") {
					parts := strings.SplitN(strings.TrimPrefix(baseType.TypeName(), "*"), "__", 2)
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
		} else {
			panic(fmt.Sprintf("[Lower Error] cannot slice type %s", baseType.TypeName()))
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

	case *ast.CallExpr:
		// sizeof 組み込みサポート (型名・変数・ポインタ構造体に対応)
		if id, ok := node.Function.(*ast.Identifier); ok && id.Value == "sizeof" {
			logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: ast=%T (%+v)\n", node, node)
			if len(node.Args) == 1 {
				arg := node.Args[0]
				logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: arg=%T (%+v)\n", arg, arg)
				t := e.resolveTypeFromExpr(arg)
				if t == nil || t == sema.TypeVoid {
					argVal := e.LowerExpr(arg)
					t = argVal.Type()
				}
				if t != nil && t != sema.TypeVoid {
					// ポインタ型の場合は指し先の実体型を評価
					if pt, isPtr := t.(*sema.PointerType); isPtr {
						t = pt.Base
					}
					sz := int64(t.Size())
					if st, _ := e.root.findStruct(t); st != nil {
						stSz := int64(st.Size())
						if stSz > sz {
							sz = stSz
						}
						// 特殊化構造体でフィールドが未展開等の場合にベーステンプレート構造体を探索
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
						logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: type '%s', size = %d\n", t.TypeName(), sz)
					}
					logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: folded to %d\n", sz)
					return &hir.ConstInt{Val: sz, Typ: sema.TypeInt}
				}
			}
			logger.LogVerbose2("[Verbose2] Lower CallExpr sizeof: FAILED to resolve, returning 0\n")
			return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
		}
		return e.root.Call.LowerCall(node)

	case *ast.MemberExpr:
		if pkgId, okPkg := node.Object.(*ast.Identifier); okPkg {
			qualified := pkgId.Value + "_" + node.Field.Value
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
				VariadicElem: targetFn.VariadicElem,
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
			if targetFn.IsCFunc && targetFn.CFuncAst != nil && !targetFn.CFuncAst.IsAlias() {
				callee = "__hike_impl_" + targetFn.Name
			} else if targetFn.IsExtern && targetFn.IRName != "" {
				callee = targetFn.IRName
			}

			fnGlobal := &hir.GlobalVar{Name: callee, Typ: &sema.PointerType{Base: sema.TypeByte}}
			t1 := e.root.nextReg(boundFnType)
			e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(boundFnType), Val: fnGlobal, Index: 0})
			t2 := e.root.nextReg(boundFnType)
			e.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: rawRecvPtr, Index: 1})
			return t2
		}

		panic(fmt.Sprintf("[Lower Error] field or method '%s' not found on type '%s'", node.Field.Value, baseType.TypeName()))

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
func (e *ExprLowerer) LowerIndexExpr(node *ast.IndexExpr) hir.Value {
	baseVal := e.LowerExpr(node.Left)
	idxVal := e.LowerExpr(node.Index)

	// タプル戻り値の場合は第0要素を抽出
	if tup, isTup := baseVal.Type().(*sema.TupleType); isTup && len(tup.Types) > 0 {
		elem0 := e.root.nextReg(tup.Types[0])
		e.root.emit(&hir.InstrExtractValue{Dst: elem0, Agg: baseVal, Index: 0})
		baseVal = elem0
	}
	if tup, isTup := idxVal.Type().(*sema.TupleType); isTup && len(tup.Types) > 0 {
		elem0 := e.root.nextReg(tup.Types[0])
		e.root.emit(&hir.InstrExtractValue{Dst: elem0, Agg: idxVal, Index: 0})
		idxVal = elem0
	}

	baseType := baseVal.Type()

	// 配列インデックス [N]T。組み込みコレクションのレシーバー解決より
	// 前に処理し、配列全体を一時領域へコピーしない。
	if ar, isArr := baseType.(*sema.ArrayType); isArr {
		idxVal = e.root.emitValueCoerce(idxVal, sema.TypeInt)
		basePtr := e.LowerLValue(node.Left)
		elemPtr := e.root.nextReg(&sema.PointerType{Base: ar.Elem})
		e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: basePtr, Index: idxVal})
		elemVal := e.root.nextReg(ar.Elem)
		e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
		return elemVal
	}

	// 組み込み文字列の添字アクセスも、コレクションのレシーバー解決を
	// 行わず直接要素を参照する。
	if baseType == sema.TypeString || baseType == sema.TypeCString {
		idxVal = e.root.emitValueCoerce(idxVal, sema.TypeInt)
		elemPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		basePtr := baseVal
		if baseType == sema.TypeString {
			basePtr, _ = e.root.stringParts(baseVal)
		}
		e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: basePtr, Index: idxVal})
		elemVal := e.root.nextReg(sema.TypeByte)
		e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
		return elemVal
	}

	// 組み込みスライスの添字アクセスは、スライスヘッダーからデータ
	// ポインターを取り出して直接要素を読む。
	if sl, isSlice := baseType.(*sema.SliceType); isSlice {
		idxVal = e.root.emitValueCoerce(idxVal, sema.TypeInt)
		rawBytePtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e.root.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: baseVal, Index: 0})
		typedPtr := e.root.nextReg(&sema.PointerType{Base: sl.Elem})
		e.root.emit(&hir.InstrCast{Dst: typedPtr, Val: rawBytePtr, ToType: typedPtr.Type()})
		elemPtr := e.root.nextReg(&sema.PointerType{Base: sl.Elem})
		e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedPtr, Index: idxVal})
		elemVal := e.root.nextReg(sl.Elem)
		e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
		return elemVal
	}

	// 1. 組み込み map[K]V
	if mp, isMap := baseType.(*sema.MapType); isMap {
		keyI64 := e.root.coerceToI64(idxVal, mp.Key)
		outPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
		e.root.emit(&hir.InstrAlloca{Dst: outPtr, AllocType: sema.TypeInt})
		e.root.emit(&hir.InstrCallStatic{CalleeName: "__hike_map_get", Args: []hir.Value{baseVal, keyI64, outPtr}})
		rawVal := e.root.nextReg(sema.TypeInt)
		e.root.emit(&hir.InstrLoad{Dst: rawVal, Ptr: outPtr})
		return e.root.coerceFromI64(rawVal, mp.Value)
	}

	// 2. ユーザー定義コレクション構造体の Indexable (Get(key))
	// baseVal からレシーバポインタを取得 (node.Left の二重評価を回避)
	var objPtr hir.Value
	if _, isPtr := baseType.(*sema.PointerType); isPtr {
		objPtr = baseVal
	} else {
		allocaTmp := e.root.nextReg(&sema.PointerType{Base: baseType})
		e.root.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: baseType})
		e.root.emit(&hir.InstrStore{Val: baseVal, Ptr: allocaTmp})
		objPtr = allocaTmp
	}

	getFnName, getFn, finalRecv, found := e.root.Call.ResolveMethod(baseType, "Get", objPtr)
	if strings.Contains(baseType.TypeName(), "__") {
		parts := strings.SplitN(strings.TrimPrefix(baseType.TypeName(), "*"), "__", 2)
		baseName := parts[0]
		typeSuffix := parts[1]
		specGet := fmt.Sprintf("%s_Get_%s", baseName, typeSuffix)
		if fn, ok := e.root.semaCtx.Functions[specGet]; ok {
			getFnName = specGet
			getFn = fn
			found = true
		}
	}
	if !found && strings.Contains(baseType.TypeName(), "__") {
		baseName := strings.Split(strings.TrimPrefix(baseType.TypeName(), "*"), "__")[0]
		if st, _ := e.root.semaCtx.LookupStruct(baseName); st != nil {
			getFnName, getFn, finalRecv, found = e.root.Call.ResolveMethod(st, "Get", objPtr)
		}
	}

	if found && getFnName != "" {
		if finalRecv == nil {
			finalRecv = objPtr
		}
		if getFn != nil && len(getFn.ParamTypes) > 0 {
			_, isPtrExpected := getFn.ParamTypes[0].(*sema.PointerType)
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

		var keyArg hir.Value
		if getFn != nil && len(getFn.ParamTypes) >= 2 {
			kIdx := 1
			if !getFn.IsMethod && len(getFn.ParamTypes) == 1 {
				kIdx = 0
			}
			keyArg = e.root.emitValueCoerce(idxVal, getFn.ParamTypes[kIdx])
		} else {
			keyArg = e.root.emitValueCoerce(idxVal, sema.TypeInt)
		}

		var retType sema.Type = sema.TypeInt
		if getFn != nil {
			if len(getFn.ReturnTypes) == 1 {
				retType = getFn.ReturnTypes[0]
			} else if len(getFn.ReturnTypes) > 1 {
				retType = &sema.TupleType{Types: getFn.ReturnTypes}
			}
		}

		resReg := e.root.nextReg(retType)
		e.root.emit(&hir.InstrCallStatic{
			Dst:        resReg,
			CalleeName: getFnName,
			Args:       []hir.Value{finalRecv, keyArg},
		})

		// 戻り値がタプル (T, bool) の場合、式コンテキストでは第0要素 (T) を抽出
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

	// 3. 従来の MapBehavior (後方互換性)
	if _, _, isBeh := e.root.semaCtx.CheckMapBehavior(baseType); isBeh {
		getFnName, getFn, finalRecv, found := e.root.Call.ResolveMethod(baseType, "Get", objPtr)
		if found && getFn != nil {
			if finalRecv == nil {
				finalRecv = objPtr
			}
			keyArg := e.root.emitValueCoerce(idxVal, getFn.ParamTypes[1])
			resReg := e.root.nextReg(getFn.ReturnTypes[0])
			e.root.emit(&hir.InstrCallStatic{
				Dst:        resReg,
				CalleeName: getFnName,
				Args:       []hir.Value{finalRecv, keyArg},
			})
			return resReg
		}
	}

	// ポインタインデックス (配列ポインタ *[N]T または 汎用ポインタ *T)
	if pt, isPtr := baseType.(*sema.PointerType); isPtr {
		idxVal = e.root.emitValueCoerce(idxVal, sema.TypeInt)
		if arrType, isArr := pt.Base.(*sema.ArrayType); isArr {
			elemPtr := e.root.nextReg(&sema.PointerType{Base: arrType.Elem})
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: baseVal, Index: idxVal})
			elemVal := e.root.nextReg(arrType.Elem)
			e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}
		elemPtr := e.root.nextReg(pt)
		e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: baseVal, Index: idxVal})
		elemVal := e.root.nextReg(pt.Base)
		e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
		return elemVal
	}

	panic(fmt.Sprintf("[Lower Error] unsupported index target type: %s", baseType.TypeName()))
}

// lowerStructLiteralPtrはスタック上の構造体リテラルをゼロ初期化して生成
func (e *ExprLowerer) lowerStructLiteralPtr(node *ast.StructLiteral) hir.Value {
	stType := e.root.semaCtx.ResolveType(node.Type).(*sema.StructType)
	allocaReg := e.root.nextReg(&sema.PointerType{Base: stType})
	e.root.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: stType})
	e.root.emit(&hir.InstrStore{Val: e.root.defaultConstValue(stType), Ptr: allocaReg})

	for i, fVal := range node.Fields {
		val := e.LowerExpr(fVal.Value)
		fieldIdx := i
		var fName string
		var fieldType sema.Type
		if fVal.Name != nil {
			fName = fVal.Name.Value
			for idx, sf := range stType.Fields {
				if sf.Name == fName {
					fieldIdx = idx
					fieldType = sf.Type
					break
				}
			}
		} else {
			fName = stType.Fields[i].Name
			fieldType = stType.Fields[i].Type
		}
		if fieldType != nil {
			val = e.root.emitValueCoerce(val, fieldType)
		} else {
			fieldType = val.Type()
		}
		fPtrReg := e.root.nextReg(&sema.PointerType{Base: fieldType})
		e.root.emit(&hir.InstrGetFieldPtr{Dst: fPtrReg, BasePtr: allocaReg, FieldIndex: fieldIdx, FieldName: fName})
		e.root.emit(&hir.InstrStore{Val: val, Ptr: fPtrReg})
	}
	return allocaReg
}

// lowerStructLiteralHeapはヒープ領域 (calloc) に構造体を確保してゼロ初期化し、ポインタを返す
func (e *ExprLowerer) lowerStructLiteralHeap(node *ast.StructLiteral) hir.Value {
	stType := e.root.semaCtx.ResolveType(node.Type).(*sema.StructType)
	size := int64(stType.Size())
	if size <= 0 {
		size = int64(sema.PointerSize)
	}
	sizeVal := &hir.ConstInt{Val: size, Typ: sema.TypeInt}
	oneVal := &hir.ConstInt{Val: 1, Typ: sema.TypeInt}

	callocRaw := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	e.root.emit(&hir.InstrCallStatic{
		Dst:        callocRaw,
		CalleeName: "calloc",
		Args:       []hir.Value{oneVal, sizeVal},
	})

	heapReg := e.root.nextReg(&sema.PointerType{Base: stType})
	e.root.emit(&hir.InstrCast{Dst: heapReg, Val: callocRaw, ToType: &sema.PointerType{Base: stType}})

	for i, fVal := range node.Fields {
		val := e.LowerExpr(fVal.Value)
		fieldIdx := i
		var fName string
		var fieldType sema.Type
		if fVal.Name != nil {
			fName = fVal.Name.Value
			for idx, sf := range stType.Fields {
				if sf.Name == fName {
					fieldIdx = idx
					fieldType = sf.Type
					break
				}
			}
		} else {
			fName = stType.Fields[i].Name
			fieldType = stType.Fields[i].Type
		}
		if fieldType != nil {
			val = e.root.emitValueCoerce(val, fieldType)
		} else {
			fieldType = val.Type()
		}
		fPtrReg := e.root.nextReg(&sema.PointerType{Base: fieldType})
		e.root.emit(&hir.InstrGetFieldPtr{Dst: fPtrReg, BasePtr: heapReg, FieldIndex: fieldIdx, FieldName: fName})
		e.root.emit(&hir.InstrStore{Val: val, Ptr: fPtrReg})
	}
	return heapReg
}

func (e *ExprLowerer) lowerArrayLiteralPtr(node *ast.ArrayLiteral) hir.Value {
	arType := e.root.semaCtx.ResolveType(node.Type).(*sema.ArrayType)
	allocaReg := e.root.nextReg(&sema.PointerType{Base: arType})
	e.root.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: arType})
	e.root.emit(&hir.InstrStore{Val: e.root.defaultConstValue(arType), Ptr: allocaReg})

	for i, el := range node.Elements {
		val := e.LowerExpr(el)
		val = e.root.emitValueCoerce(val, arType.Elem)
		elemPtr := e.root.nextReg(&sema.PointerType{Base: arType.Elem})
		e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: allocaReg, Index: &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt}})
		e.root.emit(&hir.InstrStore{Val: val, Ptr: elemPtr})
	}
	return allocaReg
}

// -------------------------------------------------------------
// 左辺値 (LValue) のポインタ解決
// -------------------------------------------------------------

func (e *ExprLowerer) LowerStructPtr(expr ast.Expression) hir.Value {
	if id, ok := expr.(*ast.Identifier); ok {
		if ptr, exists := e.root.symbols[id.Value]; exists {
			valueType := ptr.Type().(*sema.PointerType).Base
			if declaredType, known := e.root.symbolTypes[id.Value]; known {
				valueType = declaredType
			}
			if _, isPtr := valueType.(*sema.PointerType); isPtr {
				loadReg := e.root.nextReg(valueType)
				e.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: ptr})
				return loadReg
			}
			return ptr
		}
		if gName, g, exists := e.lookupGlobal(id.Value); exists {
			if _, isPtr := g.(*sema.PointerType); isPtr {
				loadReg := e.root.nextReg(g)
				e.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: &hir.GlobalVar{Name: gName, Typ: &sema.PointerType{Base: g}}})
				return loadReg
			}
			return &hir.GlobalVar{Name: gName, Typ: &sema.PointerType{Base: g}}
		}
	}
	val := e.LowerExpr(expr)
	if _, isPtr := val.Type().(*sema.PointerType); isPtr {
		return val
	}
	allocaTmp := e.root.nextReg(&sema.PointerType{Base: val.Type()})
	e.root.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: val.Type()})
	e.root.emit(&hir.InstrStore{Val: val, Ptr: allocaTmp})
	return allocaTmp
}

func (e *ExprLowerer) LowerLValue(expr ast.Expression) hir.Value {
	switch node := expr.(type) {
	case *ast.Identifier:
		if ptr, ok := e.root.symbols[node.Value]; ok {
			return ptr
		}
		if gName, g, ok := e.lookupGlobal(node.Value); ok {
			return &hir.GlobalVar{Name: gName, Typ: &sema.PointerType{Base: g}}
		}
		panic(fmt.Sprintf("[Lower Error] undefined identifier for lvalue: %s", node.Value))

	case *ast.MemberExpr:
		basePtr := e.LowerStructPtr(node.Object)
		baseType := basePtr.Type().(*sema.PointerType).Base
		st, sName := e.root.findStruct(baseType)
		if st == nil {
			panic(fmt.Sprintf("[Lower Error] type '%s' has no fields", baseType.TypeName()))
		}
		fieldPtr, _, _, found := e.ResolveFieldPath(st, sName, basePtr, node.Field.Value)
		if !found {
			panic(fmt.Sprintf("[Lower Error] field '%s' not found on struct '%s'", node.Field.Value, sName))
		}
		return fieldPtr

	case *ast.PrefixExpr:
		if node.Operator == "*" {
			return e.LowerExpr(node.Right)
		}
		panic(fmt.Sprintf("[Lower Error] invalid prefix operator for lvalue: %s", node.Operator))

	case *ast.StructLiteral:
		return e.lowerStructLiteralHeap(node)

	case *ast.ArrayLiteral:
		return e.lowerArrayLiteralPtr(node)

	case *ast.IndexExpr:
		idxVal := e.LowerExpr(node.Index)
		leftVal := e.LowerExpr(node.Left)
		leftType := leftVal.Type()

		if slType, ok := leftType.(*sema.SliceType); ok {
			elemPtrType := &sema.PointerType{Base: slType.Elem}
			rawPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: leftVal, Index: 0})
			typedPtr := e.root.nextReg(elemPtrType)
			e.root.emit(&hir.InstrCast{Dst: typedPtr, Val: rawPtr, ToType: elemPtrType})
			elemPtr := e.root.nextReg(elemPtrType)
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedPtr, Index: idxVal})
			return elemPtr
		}

		if leftType == sema.TypeString || leftType == sema.TypeCString || (leftType != nil && (leftType.TypeName() == "string" || leftType.TypeName() == "cstring")) {
			elemPtrType := &sema.PointerType{Base: sema.TypeByte}
			elemPtr := e.root.nextReg(elemPtrType)
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: leftVal, Index: idxVal})
			return elemPtr
		}

		if pt, ok := leftType.(*sema.PointerType); ok {
			if arrType, isArr := pt.Base.(*sema.ArrayType); isArr {
				elemPtrType := &sema.PointerType{Base: arrType.Elem}
				elemPtr := e.root.nextReg(elemPtrType)
				e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: leftVal, Index: idxVal})
				return elemPtr
			}
			elemPtr := e.root.nextReg(pt)
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: leftVal, Index: idxVal})
			return elemPtr
		}

		if arrType, ok := leftType.(*sema.ArrayType); ok {
			basePtr := e.LowerLValue(node.Left)
			elemPtrType := &sema.PointerType{Base: arrType.Elem}
			elemPtr := e.root.nextReg(elemPtrType)
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: basePtr, Index: idxVal})
			return elemPtr
		}

		panic(fmt.Sprintf("[Lower Error] cannot index type '%s' as lvalue", leftType.TypeName()))

	default:
		panic(fmt.Sprintf("[Lower Error] expression is not an lvalue: %T", expr))
	}
}

// -------------------------------------------------------------
// 二項演算子 (BinaryExpr) の変換
// -------------------------------------------------------------

func (e *ExprLowerer) LowerBinaryExpr(node *ast.BinaryExpr) hir.Value {
	if node.WithCarry && (node.Operator == "<<" || node.Operator == ">>") {
		return e.lowerShiftWithCarry(node)
	}
	if node.Operator == "&&" {
		resAlloca := e.root.nextReg(&sema.PointerType{Base: sema.TypeBool}, "land.res")
		e.root.emit(&hir.InstrAlloca{Dst: resAlloca, AllocType: sema.TypeBool})
		e.root.emit(&hir.InstrStore{Val: &hir.ConstBool{Val: false, Typ: sema.TypeBool}, Ptr: resAlloca})

		leftVal := e.LowerExpr(node.Left)
		rhsBB := e.root.newBlock("land.rhs")
		endBB := e.root.newBlock("land.end")

		e.root.terminate(&hir.InstrBranch{Cond: leftVal, ThenTarget: rhsBB.Label, ElseTarget: endBB.Label})

		e.root.setBlock(rhsBB)
		rightVal := e.LowerExpr(node.Right)
		e.root.emit(&hir.InstrStore{Val: rightVal, Ptr: resAlloca})
		e.root.terminate(&hir.InstrJump{Target: endBB.Label})

		e.root.setBlock(endBB)
		finalReg := e.root.nextReg(sema.TypeBool)
		e.root.emit(&hir.InstrLoad{Dst: finalReg, Ptr: resAlloca})
		return finalReg
	}

	if node.Operator == "||" {
		resAlloca := e.root.nextReg(&sema.PointerType{Base: sema.TypeBool}, "lor.res")
		e.root.emit(&hir.InstrAlloca{Dst: resAlloca, AllocType: sema.TypeBool})
		e.root.emit(&hir.InstrStore{Val: &hir.ConstBool{Val: true, Typ: sema.TypeBool}, Ptr: resAlloca})

		leftVal := e.LowerExpr(node.Left)
		rhsBB := e.root.newBlock("lor.rhs")
		endBB := e.root.newBlock("lor.end")

		e.root.terminate(&hir.InstrBranch{Cond: leftVal, ThenTarget: endBB.Label, ElseTarget: rhsBB.Label})

		e.root.setBlock(rhsBB)
		rightVal := e.LowerExpr(node.Right)
		e.root.emit(&hir.InstrStore{Val: rightVal, Ptr: resAlloca})
		e.root.terminate(&hir.InstrJump{Target: endBB.Label})

		e.root.setBlock(endBB)
		finalReg := e.root.nextReg(sema.TypeBool)
		e.root.emit(&hir.InstrLoad{Dst: finalReg, Ptr: resAlloca})
		return finalReg
	}

	leftVal := e.LowerExpr(node.Left)
	rightVal := e.LowerExpr(node.Right)

	if tup, isTup := leftVal.Type().(*sema.TupleType); isTup && len(tup.Types) > 0 {
		elem0 := e.root.nextReg(tup.Types[0])
		e.root.emit(&hir.InstrExtractValue{Dst: elem0, Agg: leftVal, Index: 0})
		leftVal = elem0
	}
	if tup, isTup := rightVal.Type().(*sema.TupleType); isTup && len(tup.Types) > 0 {
		elem0 := e.root.nextReg(tup.Types[0])
		e.root.emit(&hir.InstrExtractValue{Dst: elem0, Agg: rightVal, Index: 0})
		rightVal = elem0
	}

	if node.Operator == "==" || node.Operator == "!=" {
		if _, isIface := leftVal.Type().(*sema.InterfaceType); isIface && isNilValue(rightVal) {
			dataPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrExtractValue{Dst: dataPtr, Agg: leftVal, Index: 0})
			op := hir.OpEq
			if node.Operator == "!=" {
				op = hir.OpNeq
			}
			cmpReg := e.root.nextReg(sema.TypeBool)
			e.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: op, L: dataPtr, R: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}})
			return cmpReg
		}
		if _, isIface := rightVal.Type().(*sema.InterfaceType); isIface && isNilValue(leftVal) {
			dataPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrExtractValue{Dst: dataPtr, Agg: rightVal, Index: 0})
			op := hir.OpEq
			if node.Operator == "!=" {
				op = hir.OpNeq
			}
			cmpReg := e.root.nextReg(sema.TypeBool)
			e.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: op, L: dataPtr, R: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}})
			return cmpReg
		}

		if leftIface, isLeftIface := leftVal.Type().(*sema.InterfaceType); isLeftIface {
			if rightIface, isRightIface := rightVal.Type().(*sema.InterfaceType); isRightIface {
				data1 := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
				data2 := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
				e.root.emit(&hir.InstrExtractValue{Dst: data1, Agg: leftVal, Index: 0})
				e.root.emit(&hir.InstrExtractValue{Dst: data2, Agg: rightVal, Index: 0})

				dataEq := e.root.nextReg(sema.TypeBool)
				e.root.emit(&hir.InstrBinary{Dst: dataEq, Op: hir.OpEq, L: data1, R: data2})

				var metaEq *hir.Reg
				if leftIface.IsAny() && rightIface.IsAny() {
					meta1 := e.root.nextReg(sema.TypeInt)
					meta2 := e.root.nextReg(sema.TypeInt)
					e.root.emit(&hir.InstrExtractValue{Dst: meta1, Agg: leftVal, Index: 1})
					e.root.emit(&hir.InstrExtractValue{Dst: meta2, Agg: rightVal, Index: 1})
					metaEq = e.root.nextReg(sema.TypeBool)
					e.root.emit(&hir.InstrBinary{Dst: metaEq, Op: hir.OpEq, L: meta1, R: meta2})
				} else {
					meta1 := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
					meta2 := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
					e.root.emit(&hir.InstrExtractValue{Dst: meta1, Agg: leftVal, Index: 1})
					e.root.emit(&hir.InstrExtractValue{Dst: meta2, Agg: rightVal, Index: 1})
					metaEq = e.root.nextReg(sema.TypeBool)
					e.root.emit(&hir.InstrBinary{Dst: metaEq, Op: hir.OpEq, L: meta1, R: meta2})
				}

				allEq := e.root.nextReg(sema.TypeBool)
				e.root.emit(&hir.InstrBinary{Dst: allEq, Op: hir.OpAnd, L: dataEq, R: metaEq})

				if node.Operator == "!=" {
					notEq := e.root.nextReg(sema.TypeBool)
					e.root.emit(&hir.InstrUnary{Dst: notEq, Op: hir.OpNot, Val: allEq})
					return notEq
				}
				return allEq
			}
		}

		if _, isFunc := leftVal.Type().(*sema.FuncType); isFunc && isNilValue(rightVal) {
			fnPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrExtractValue{Dst: fnPtr, Agg: leftVal, Index: 0})
			op := hir.OpEq
			if node.Operator == "!=" {
				op = hir.OpNeq
			}
			cmpReg := e.root.nextReg(sema.TypeBool)
			e.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: op, L: fnPtr, R: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}})
			return cmpReg
		}
		if _, isFunc := rightVal.Type().(*sema.FuncType); isFunc && isNilValue(leftVal) {
			fnPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrExtractValue{Dst: fnPtr, Agg: rightVal, Index: 0})
			op := hir.OpEq
			if node.Operator == "!=" {
				op = hir.OpNeq
			}
			cmpReg := e.root.nextReg(sema.TypeBool)
			e.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: op, L: fnPtr, R: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}})
			return cmpReg
		}
	}

	isPtrOrCString := false
	if _, isPtr := leftVal.Type().(*sema.PointerType); isPtr {
		isPtrOrCString = true
	} else if leftVal.Type() == sema.TypeCString {
		isPtrOrCString = true
	}
	if isPtrOrCString && (rightVal.Type() == sema.TypeInt || rightVal.Type() == sema.TypeByte || rightVal.Type() == sema.TypeInt32 || rightVal.Type() == sema.TypeInt64 || rightVal.Type() == sema.TypeUint || rightVal.Type() == sema.TypeUint32 || rightVal.Type() == sema.TypeUint64) {
		if node.Operator == "+" {
			dst := e.root.nextReg(leftVal.Type())
			e.root.emit(&hir.InstrGetElemPtr{Dst: dst, BasePtr: leftVal, Index: rightVal})
			return dst
		}
	}

	// 両辺がstring型の場合のみhike_streq / hike_strcatを呼ぶ
	if leftVal.Type() == sema.TypeString && rightVal.Type() == sema.TypeString {
		leftPtr, leftLen := e.root.stringParts(leftVal)
		rightPtr, rightLen := e.root.stringParts(rightVal)
		if node.Operator == "+" {
			raw := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrCallStatic{Dst: raw, CalleeName: e.root.BuiltinName("hike_strcat_len"), Args: []hir.Value{leftPtr, leftLen, rightPtr, rightLen}})
			length := e.root.nextReg(sema.TypeInt)
			e.root.emit(&hir.InstrCallStatic{Dst: length, CalleeName: e.root.BuiltinName("strlen"), Args: []hir.Value{raw}})
			return e.root.makeString(raw, length)
		}
		if node.Operator == "==" || node.Operator == "!=" {
			eqRes := e.root.nextReg(sema.TypeBool)
			e.root.emit(&hir.InstrCallStatic{Dst: eqRes, CalleeName: e.root.BuiltinName("hike_streq_len"), Args: []hir.Value{leftPtr, leftLen, rightPtr, rightLen}})
			if node.Operator == "!=" {
				notRes := e.root.nextReg(sema.TypeBool)
				e.root.emit(&hir.InstrUnary{Dst: notRes, Op: hir.OpNot, Val: eqRes})
				return notRes
			}
			return eqRes
		}
	}

	op := hir.OpAdd
	resType := leftVal.Type()

	switch node.Operator {
	case "+":
		op = hir.OpAdd
	case "-":
		op = hir.OpSub
	case "*":
		op = hir.OpMul
	case "/":
		op = hir.OpDiv
	case "%":
		op = hir.OpRem
	case "&":
		op = hir.OpAnd
	case "|":
		op = hir.OpOr
	case "^":
		op = hir.OpXor
	case "<<":
		op = hir.OpShl
	case ">>":
		op = hir.OpShr
	case "==":
		op = hir.OpEq
		resType = sema.TypeBool
	case "!=":
		op = hir.OpNeq
		resType = sema.TypeBool
	case "<":
		op = hir.OpLt
		resType = sema.TypeBool
	case "<=":
		op = hir.OpLe
		resType = sema.TypeBool
	case ">":
		op = hir.OpGt
		resType = sema.TypeBool
	case ">=":
		op = hir.OpGe
		resType = sema.TypeBool
	}

	dst := e.root.nextReg(resType)
	e.root.emit(&hir.InstrBinary{Dst: dst, Op: op, L: leftVal, R: rightVal})
	return dst
}

func (e *ExprLowerer) lowerShiftWithCarry(node *ast.BinaryExpr) hir.Value {
	leftVal := e.LowerExpr(node.Left)
	rightVal := e.LowerExpr(node.Right)
	valueType := leftVal.Type()
	valueReg := e.root.nextReg(valueType)
	shiftOp := hir.OpShl
	if node.Operator == ">>" {
		shiftOp = hir.OpShr
	}
	e.root.emit(&hir.InstrBinary{Dst: valueReg, Op: shiftOp, L: leftVal, R: rightVal})

	var carryReg *hir.Reg
	if node.Operator == "<<" {
		width := integerBitWidth(valueType.LLVMType())
		carryReg = e.root.nextReg(valueType)
		e.root.emit(&hir.InstrBinary{
			Dst:          carryReg,
			Op:           hir.OpShr,
			L:            leftVal,
			R:            &hir.ConstInt{Val: int64(width - 1), Typ: valueType},
			LogicalShift: true,
		})
	} else {
		carryReg = e.root.nextReg(valueType)
		e.root.emit(&hir.InstrBinary{
			Dst: carryReg,
			Op:  hir.OpAnd,
			L:   leftVal,
			R:   &hir.ConstInt{Val: 1, Typ: valueType},
		})
	}

	tupleType := &sema.TupleType{Types: []sema.Type{valueType, valueType}}
	tuple0 := e.root.nextReg(tupleType)
	e.root.emit(&hir.InstrInsertValue{Dst: tuple0, Agg: e.root.defaultConstValue(tupleType), Val: valueReg, Index: 0})
	tuple := e.root.nextReg(tupleType)
	e.root.emit(&hir.InstrInsertValue{Dst: tuple, Agg: tuple0, Val: carryReg, Index: 1})
	return tuple
}

func integerBitWidth(llvmType string) int {
	var width int
	if _, err := fmt.Sscanf(strings.TrimPrefix(llvmType, "i"), "%d", &width); err != nil || width <= 0 {
		panic(fmt.Sprintf("[Lower Error] shift requires a sized integer type, got '%s'", llvmType))
	}
	return width
}

func (e *ExprLowerer) LowerAsyncExpr(ae *ast.AsyncExpr) hir.Value {
	fnVal := e.LowerExpr(ae.Fn)

	var fnPtr hir.Value
	var envPtr hir.Value
	var retTypes []sema.Type

	if ft, ok := fnVal.Type().(*sema.FuncType); ok {
		retTypes = ft.ReturnTypes
		fnPtrReg := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		envPtrReg := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e.root.emit(&hir.InstrExtractValue{Dst: fnPtrReg, Agg: fnVal, Index: 0})
		e.root.emit(&hir.InstrExtractValue{Dst: envPtrReg, Agg: fnVal, Index: 1})
		fnPtr = fnPtrReg
		envPtr = envPtrReg
	} else {
		fnPtr = fnVal
		envPtr = &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}
	}

	futureType := &sema.FutureType{ReturnTypes: retTypes}
	futReg := e.root.nextReg(futureType)
	e.root.emit(&hir.InstrAsync{
		Dst:      futReg,
		FnPtr:    fnPtr,
		EnvPtr:   envPtr,
		RetTypes: retTypes,
	})
	return futReg
}

func (e *ExprLowerer) LowerReceiveExpr(re *ast.ReceiveExpr) hir.Value {
	targetVal := e.LowerExpr(re.Expr)
	targetType := targetVal.Type()

	if fut, isFut := targetType.(*sema.FutureType); isFut {
		waitRes := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e.root.emit(&hir.InstrTaskWait{Dst: waitRes, Task: targetVal})

		if len(fut.ReturnTypes) == 0 {
			return &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}
		}

		if len(fut.ReturnTypes) == 1 {
			retType := fut.ReturnTypes[0]
			typedPtr := e.root.nextReg(&sema.PointerType{Base: retType})
			e.root.emit(&hir.InstrCast{Dst: typedPtr, Val: waitRes, ToType: &sema.PointerType{Base: retType}})
			resVal := e.root.nextReg(retType)
			e.root.emit(&hir.InstrLoad{Dst: resVal, Ptr: typedPtr})
			return resVal
		}

		tupleType := &sema.TupleType{Types: fut.ReturnTypes}
		tuplePtrType := &sema.PointerType{Base: tupleType}
		typedBuf := e.root.nextReg(tuplePtrType)
		e.root.emit(&hir.InstrCast{Dst: typedBuf, Val: waitRes, ToType: tuplePtrType})

		loadedTuple := e.root.nextReg(tupleType)
		e.root.emit(&hir.InstrLoad{Dst: loadedTuple, Ptr: typedBuf})
		return loadedTuple
	}

	if ct, isChan := targetType.(*sema.ChanType); isChan {
		dst := e.root.nextReg(ct.Elem)
		e.root.emit(&hir.InstrChanRecv{Dst: dst, Chan: targetVal})
		return dst
	}

	return targetVal
}

func (e *ExprLowerer) LowerTypeAssertExpr(tae *ast.TypeAssertExpr) hir.Value {
	return e.lowerTypeAssertExpr(tae, false)
}

// LowerTypeAssertExprWithPanic implements Go's single-result assertion form.
// The comma-ok form is lowered by LowerTypeAssertExpr and deliberately
// returns the match flag instead of trapping.
func (e *ExprLowerer) LowerTypeAssertExprWithPanic(tae *ast.TypeAssertExpr) hir.Value {
	return e.lowerTypeAssertExpr(tae, true)
}

func (e *ExprLowerer) lowerTypeAssertExpr(tae *ast.TypeAssertExpr, trapOnFailure bool) hir.Value {
	ifaceVal := e.LowerExpr(tae.Expr)
	ifaceType := ifaceVal.Type()
	targetType := e.root.semaCtx.ResolveType(tae.Target)
	targetTypeID := e.root.semaCtx.GetTypeID(targetType)

	dataPtrReg := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	typeIDReg := e.root.nextReg(sema.TypeInt)

	if it, ok := ifaceType.(*sema.InterfaceType); ok && !it.IsAny() {
		itabRawReg := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e.root.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: ifaceVal, Index: 0})
		e.root.emit(&hir.InstrExtractValue{Dst: itabRawReg, Agg: ifaceVal, Index: 1})
		typeIDPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
		e.root.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabRawReg, ToType: &sema.PointerType{Base: sema.TypeInt}})
		e.root.emit(&hir.InstrLoad{Dst: typeIDReg, Ptr: typeIDPtr})
	} else {
		e.root.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: ifaceVal, Index: 0})
		e.root.emit(&hir.InstrExtractValue{Dst: typeIDReg, Agg: ifaceVal, Index: 1})
	}

	matchReg := e.root.nextReg(sema.TypeBool)
	e.root.emit(&hir.InstrBinary{Dst: matchReg, Op: hir.OpEq, L: typeIDReg, R: &hir.ConstInt{Val: targetTypeID, Typ: sema.TypeInt}})
	if trapOnFailure {
		okBB := e.root.newBlock("typeassert.ok")
		failBB := e.root.newBlock("typeassert.fail")
		e.root.terminate(&hir.InstrBranch{Cond: matchReg, ThenTarget: okBB.Label, ElseTarget: failBB.Label})
		e.root.setBlock(failBB)
		e.root.emit(&hir.InstrCallStatic{CalleeName: "llvm.trap"})
		e.root.terminate(&hir.InstrUnreachable{})
		e.root.setBlock(okBB)
	}

	unpackedReg := e.root.nextReg(targetType)
	if strings.HasSuffix(targetType.LLVMType(), "*") {
		e.root.emit(&hir.InstrCast{Dst: unpackedReg, Val: dataPtrReg, ToType: targetType})
	} else {
		typedPtr := e.root.nextReg(&sema.PointerType{Base: targetType})
		e.root.emit(&hir.InstrCast{Dst: typedPtr, Val: dataPtrReg, ToType: &sema.PointerType{Base: targetType}})
		e.root.emit(&hir.InstrLoad{Dst: unpackedReg, Ptr: typedPtr})
	}

	tupleType := &sema.TupleType{Types: []sema.Type{targetType, sema.TypeBool}}
	t1 := e.root.nextReg(tupleType)
	e.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: e.root.defaultConstValue(tupleType), Val: unpackedReg, Index: 0})
	t2 := e.root.nextReg(tupleType)
	e.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: matchReg, Index: 1})
	return t2
}

func (e *ExprLowerer) ResolveFieldPath(st *sema.StructType, sName string, curPtr hir.Value, fieldName string) (hir.Value, sema.Type, string, bool) {
	if st == nil {
		return nil, nil, "", false
	}
	for i, f := range st.Fields {
		if f.Name == fieldName {
			fieldPtr := e.root.nextReg(&sema.PointerType{Base: f.Type})
			e.root.emit(&hir.InstrGetFieldPtr{Dst: fieldPtr, BasePtr: curPtr, FieldIndex: i, FieldName: f.Name})
			return fieldPtr, f.Type, sName, true
		}
	}
	for i, f := range st.Fields {
		if f.IsEmbedded {
			embTypeName := strings.TrimPrefix(f.Type.TypeName(), "*")
			embSt, embStructName := e.root.findStructByName(embTypeName)
			if embSt != nil {
				gepReg := e.root.nextReg(&sema.PointerType{Base: f.Type})
				e.root.emit(&hir.InstrGetFieldPtr{Dst: gepReg, BasePtr: curPtr, FieldIndex: i, FieldName: f.Name})
				nextPtr := hir.Value(gepReg)
				if _, isPtr := f.Type.(*sema.PointerType); isPtr {
					loadReg := e.root.nextReg(f.Type)
					e.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: gepReg})
					nextPtr = loadReg
				}
				if finalGep, finalType, sNameFound, found := e.ResolveFieldPath(embSt, embStructName, nextPtr, fieldName); found {
					return finalGep, finalType, sNameFound, true
				}
			}
		}
	}
	return nil, nil, "", false
}
