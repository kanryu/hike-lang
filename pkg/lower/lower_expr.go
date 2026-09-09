package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

// ExprLowerer は式の評価、演算子処理、左辺値（LValue）ポインタ解決、構造体フィールド探索を担当する
type ExprLowerer struct {
	root *Lowerer
}

func NewExprLowerer(root *Lowerer) *ExprLowerer {
	return &ExprLowerer{root: root}
}

// -----------------------------------------------------------------------------
// 式 (Expression) の評価
// -----------------------------------------------------------------------------

func (e *ExprLowerer) LowerExpr(expr ast.Expression) hir.Value {
	if expr == nil {
		return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
	}

	switch node := expr.(type) {
	case *ast.IntegerLiteral:
		return &hir.ConstInt{Val: node.Value, Typ: sema.TypeInt}

	case *ast.FloatLiteral:
		return &hir.ConstFloat{Val: node.Value, Typ: sema.TypeFloat64}

	case *ast.StringLiteral:
		return e.root.getStringConst(node.Value)

	case *ast.NilLiteral:
		return &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}

	case *ast.ImplicitCastExpr:
		val := e.LowerExpr(node.Expr)
		targetT := e.root.semaCtx.ResolveType(node.TargetType)

		// 右辺がタプルの場合、第0要素を取り出す
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
		if g, ok := e.root.semaCtx.Globals[node.Value]; ok {
			dst := e.root.nextReg(g)
			e.root.emit(&hir.InstrLoad{Dst: dst, Ptr: &hir.GlobalVar{Name: node.Value, Typ: &sema.PointerType{Base: g}}})
			return dst
		}
		if fn, ok := e.root.semaCtx.Functions[node.Value]; ok {
			fatType := fn
			callee := fn.Name
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
		op := hir.OpNeg
		if node.Operator == "!" {
			op = hir.OpNot
		}
		dst := e.root.nextReg(val.Type())
		e.root.emit(&hir.InstrUnary{Dst: dst, Op: op, Val: val})
		return dst

	case *ast.StructLiteral:
		stType := e.root.semaCtx.ResolveType(node.Type).(*sema.StructType)
		allocaReg := e.root.nextReg(&sema.PointerType{Base: stType})
		e.root.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: stType})

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

		resReg := e.root.nextReg(stType)
		e.root.emit(&hir.InstrLoad{Dst: resReg, Ptr: allocaReg})
		return resReg

	case *ast.ArrayLiteral:
		arType := e.root.semaCtx.ResolveType(node.Type).(*sema.ArrayType)
		allocaReg := e.root.nextReg(&sema.PointerType{Base: arType})
		e.root.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: arType})

		for i, el := range node.Elements {
			val := e.LowerExpr(el)
			val = e.root.emitValueCoerce(val, arType.Elem)
			elemPtr := e.root.nextReg(&sema.PointerType{Base: arType.Elem})
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: allocaReg, Index: &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt}})
			e.root.emit(&hir.InstrStore{Val: val, Ptr: elemPtr})
		}

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
		if baseVal.Type() == sema.TypeString {
			lowVal := hir.Value(&hir.ConstInt{Val: 0, Typ: sema.TypeInt})
			if node.Low != nil {
				lowVal = e.LowerExpr(node.Low)
			}
			highVal := hir.Value(e.root.nextReg(sema.TypeInt))
			if node.High != nil {
				highVal = e.LowerExpr(node.High)
			} else {
				e.root.emit(&hir.InstrCallStatic{Dst: highVal.(*hir.Reg), CalleeName: "strlen", Args: []hir.Value{baseVal}})
			}
			subRes := e.root.nextReg(sema.TypeString)
			e.root.emit(&hir.InstrCallStatic{Dst: subRes, CalleeName: "hike_substr", Args: []hir.Value{baseVal, lowVal, highVal}})
			return subRes
		}

		var elemType sema.Type = sema.TypeByte
		var typedDataPtr hir.Value = nil
		var capVal hir.Value = nil

		if slType, isSlice := baseVal.Type().(*sema.SliceType); isSlice {
			elemType = slType.Elem
			rawBytePtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			cVal := e.root.nextReg(sema.TypeInt)
			e.root.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: baseVal, Index: 0})
			e.root.emit(&hir.InstrExtractValue{Dst: cVal, Agg: baseVal, Index: 2})
			tPtr := e.root.nextReg(&sema.PointerType{Base: elemType})
			e.root.emit(&hir.InstrCast{Dst: tPtr, Val: rawBytePtr, ToType: &sema.PointerType{Base: elemType}})
			typedDataPtr = tPtr
			capVal = cVal
		} else if arType, isArray := baseVal.Type().(*sema.ArrayType); isArray {
			elemType = arType.Elem
			arrPtr := e.LowerLValue(node.Left)
			tPtr := e.root.nextReg(&sema.PointerType{Base: elemType})
			e.root.emit(&hir.InstrCast{Dst: tPtr, Val: arrPtr, ToType: &sema.PointerType{Base: elemType}})
			typedDataPtr = tPtr
			capVal = &hir.ConstInt{Val: int64(arType.Len), Typ: sema.TypeInt}
		} else {
			panic(fmt.Sprintf("[Lower Error] cannot slice type %s", baseVal.Type().TypeName()))
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
		ptr := e.LowerLValue(node)
		ptrType := ptr.Type().(*sema.PointerType)
		dst := e.root.nextReg(ptrType.Base)
		e.root.emit(&hir.InstrLoad{Dst: dst, Ptr: ptr})
		return dst

	case *ast.IndexExpr:
		baseVal := e.LowerExpr(node.Left)
		idxVal := e.LowerExpr(node.Index)

		if mp, isMap := baseVal.Type().(*sema.MapType); isMap {
			keyI64 := e.root.coerceToI64(idxVal, mp.Key)
			outPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
			e.root.emit(&hir.InstrAlloca{Dst: outPtr, AllocType: sema.TypeInt})
			e.root.emit(&hir.InstrCallStatic{CalleeName: "__hike_map_get", Args: []hir.Value{baseVal, keyI64, outPtr}})
			rawVal := e.root.nextReg(sema.TypeInt)
			e.root.emit(&hir.InstrLoad{Dst: rawVal, Ptr: outPtr})
			return e.root.coerceFromI64(rawVal, mp.Value)
		}

		if baseVal.Type() == sema.TypeString {
			elemPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: baseVal, Index: idxVal})
			elemVal := e.root.nextReg(sema.TypeByte)
			e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}

		if sl, isSlice := baseVal.Type().(*sema.SliceType); isSlice {
			rawBytePtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			e.root.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: baseVal, Index: 0})
			typedPtr := e.root.nextReg(&sema.PointerType{Base: sl.Elem})
			e.root.emit(&hir.InstrCast{Dst: typedPtr, Val: rawBytePtr, ToType: &sema.PointerType{Base: sl.Elem}})
			elemPtr := e.root.nextReg(&sema.PointerType{Base: sl.Elem})
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedPtr, Index: idxVal})
			elemVal := e.root.nextReg(sl.Elem)
			e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}

		if pt, isPtr := baseVal.Type().(*sema.PointerType); isPtr {
			elemPtr := e.root.nextReg(pt)
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: baseVal, Index: idxVal})
			elemVal := e.root.nextReg(pt.Base)
			e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}

		if ar, isArr := baseVal.Type().(*sema.ArrayType); isArr {
			arrPtr := e.LowerLValue(node.Left)
			elemPtr := e.root.nextReg(&sema.PointerType{Base: ar.Elem})
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: arrPtr, Index: idxVal})
			elemVal := e.root.nextReg(ar.Elem)
			e.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}

		panic(fmt.Sprintf("[Lower Error] unsupported index target type: %s", baseVal.Type().TypeName()))

	case *ast.TypeAssertExpr:
		tup := e.LowerTypeAssertExpr(node)
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

// -------------------------------------------------------------
// 左辺値 (LValue) のポインタ解決
// -------------------------------------------------------------

func (e *ExprLowerer) LowerStructPtr(expr ast.Expression) hir.Value {
	if id, ok := expr.(*ast.Identifier); ok {
		if ptr, exists := e.root.symbols[id.Value]; exists {
			ptrType := ptr.Type().(*sema.PointerType)
			if _, isPtr := ptrType.Base.(*sema.PointerType); isPtr {
				loadReg := e.root.nextReg(ptrType.Base)
				e.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: ptr})
				return loadReg
			}
			return ptr
		}
		if g, exists := e.root.semaCtx.Globals[id.Value]; exists {
			if _, isPtr := g.(*sema.PointerType); isPtr {
				loadReg := e.root.nextReg(g)
				e.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: &hir.GlobalVar{Name: id.Value, Typ: &sema.PointerType{Base: g}}})
				return loadReg
			}
			return &hir.GlobalVar{Name: id.Value, Typ: &sema.PointerType{Base: g}}
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

// LowerLValue は代入先やアドレス取得（&）の対象となるメモリアドレス（ポインタ）を取得する
func (e *ExprLowerer) LowerLValue(expr ast.Expression) hir.Value {
	switch node := expr.(type) {
	case *ast.Identifier:
		if ptr, ok := e.root.symbols[node.Value]; ok {
			return ptr
		}
		// semaCtx.Globals の値 g はそれ自体が sema.Type
		if g, ok := e.root.semaCtx.Globals[node.Value]; ok {
			return &hir.GlobalVar{Name: node.Value, Typ: &sema.PointerType{Base: g}}
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

	case *ast.IndexExpr:
		idxVal := e.LowerExpr(node.Index)
		leftVal := e.LowerExpr(node.Left)
		leftType := leftVal.Type()

		// 1. スライス ([]T)
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

		// 2. 文字列 (string)
		if leftType == sema.TypeString || (leftType != nil && leftType.TypeName() == "string") {
			elemPtrType := &sema.PointerType{Base: sema.TypeByte}
			elemPtr := e.root.nextReg(elemPtrType)
			e.root.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: leftVal, Index: idxVal})
			return elemPtr
		}

		// 3. ポインタ (*T または *[N]T)
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

		// 4. 配列 ([N]T)
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

	if pt, isPtr := leftVal.Type().(*sema.PointerType); isPtr && (rightVal.Type() == sema.TypeInt || rightVal.Type() == sema.TypeByte) {
		if node.Operator == "+" {
			dst := e.root.nextReg(pt)
			e.root.emit(&hir.InstrGetElemPtr{Dst: dst, BasePtr: leftVal, Index: rightVal})
			return dst
		}
	}

	if leftVal.Type() == sema.TypeString || rightVal.Type() == sema.TypeString {
		if node.Operator == "+" {
			res := e.root.nextReg(sema.TypeString)
			e.root.emit(&hir.InstrCallStatic{Dst: res, CalleeName: "hike_strcat", Args: []hir.Value{leftVal, rightVal}})
			return res
		}
		if node.Operator == "==" || node.Operator == "!=" {
			eqRes := e.root.nextReg(sema.TypeBool)
			e.root.emit(&hir.InstrCallStatic{Dst: eqRes, CalleeName: "hike_streq", Args: []hir.Value{leftVal, rightVal}})
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

// -------------------------------------------------------------
// 並行処理式 (Async / Receive)
// -------------------------------------------------------------

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

// -------------------------------------------------------------
// 型アサーション (TypeAssert)
// -------------------------------------------------------------

func (e *ExprLowerer) LowerTypeAssertExpr(tae *ast.TypeAssertExpr) hir.Value {
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

// -------------------------------------------------------------
// 構造体フィールド探索 (Field Path Resolution)
// -------------------------------------------------------------

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
