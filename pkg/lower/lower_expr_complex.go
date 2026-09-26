package lower

import (
	"fmt"
	"strconv"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

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
	if strings.Contains(semaTypeName(baseType), "__") {
		parts := strings.SplitN(strings.TrimPrefix(semaTypeName(baseType), "*"), "__", 2)
		baseName := parts[0]
		typeSuffix := parts[1]
		specGet := fmt.Sprintf("%s_Get_%s", baseName, typeSuffix)
		if fn, ok := e.root.semaCtx.Functions[specGet]; ok {
			getFnName = specGet
			getFn = fn
			found = true
		}
	}
	if !found && strings.Contains(semaTypeName(baseType), "__") {
		baseName := strings.Split(strings.TrimPrefix(semaTypeName(baseType), "*"), "__")[0]
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

	file := e.root.sourceFile
	if file == "" {
		file = "input.hike"
	}
	panic(fmt.Sprintf("%s:%d:%d: unsupported index target type: %s", file, node.Token.Line, node.Token.Col, semaTypeName(baseType)))
}

func isNilNamedType(typ ast.TypeExpr) bool {
	named, ok := typ.(*ast.NamedType)
	return ok && named == nil
}

func annotateNestedLiteralType(expr ast.Expression, target sema.Type) {
	if expr == nil || target == nil {
		return
	}
	if pointer, ok := target.(*sema.PointerType); ok {
		target = pointer.Base
	}
	switch literal := expr.(type) {
	case *ast.StructLiteral:
		if literal.Type == nil {
			if named, ok := semaTypeToTypeExpr(target).(*ast.NamedType); ok {
				literal.Type = named
			}
		}
		if structType, ok := target.(*sema.StructType); ok {
			for _, field := range literal.Fields {
				if field == nil || field.Name == nil {
					continue
				}
				for _, declared := range structType.Fields {
					if declared.Name == field.Name.Value {
						annotateNestedLiteralType(field.Value, declared.Type)
						break
					}
				}
			}
		}
	case *ast.ArrayLiteral:
		if arrayType, ok := target.(*sema.ArrayType); ok {
			for _, element := range literal.Elements {
				annotateNestedLiteralType(element, arrayType.Elem)
			}
		}
	case *ast.SliceLiteral:
		if sliceType, ok := target.(*sema.SliceType); ok {
			for _, element := range literal.Elements {
				annotateNestedLiteralType(element, sliceType.Elem)
			}
		}
	case *ast.MapLiteral:
		if mapType, ok := target.(*sema.MapType); ok {
			for _, entry := range literal.Entries {
				if entry != nil {
					annotateNestedLiteralType(entry.Value, mapType.Value)
				}
			}
		}
	}
}

// lowerStructLiteralPtrはスタック上の構造体リテラルをゼロ初期化して生成
func (e *ExprLowerer) lowerStructLiteralPtr(node *ast.StructLiteral) hir.Value {
	if node.Type == nil || isNilNamedType(node.Type) {
		panic(fmt.Sprintf("[Lower Error] struct literal has no type at %d:%d", node.Token.Line, node.Token.Col))
	}
	resolvedType := e.root.semaCtx.ResolveType(node.Type)
	if slType, ok := resolvedType.(*sema.SliceType); ok {
		// Go permits composite slice literals to arrive through the generic
		// StructLiteral AST path (notably for pointer-prefixed literals).
		elements := make([]ast.Expression, 0, len(node.Fields))
		for _, field := range node.Fields {
			if field != nil {
				elements = append(elements, field.Value)
			}
		}
		sliceType := &ast.SliceType{Token: node.Type.Token, Elem: semaTypeToTypeExpr(slType.Elem)}
		sliceValue := e.lowerSliceLiteral(&ast.SliceLiteral{Token: node.Token, Type: sliceType, Elements: elements})
		slicePtr := e.root.nextReg(&sema.PointerType{Base: slType})
		e.root.emit(&hir.InstrAlloca{Dst: slicePtr, AllocType: slType})
		e.root.emit(&hir.InstrStore{Val: sliceValue, Ptr: slicePtr})
		return slicePtr
	}
	stType, ok := resolvedType.(*sema.StructType)
	if !ok {
		panic(fmt.Sprintf("[Lower Error] composite literal is not a struct at %d:%d", node.Token.Line, node.Token.Col))
	}
	for _, field := range node.Fields {
		if field == nil || field.Name == nil {
			continue
		}
		for _, declared := range stType.Fields {
			if declared.Name == field.Name.Value {
				annotateNestedLiteralType(field.Value, declared.Type)
				break
			}
		}
	}
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
	if node.Type == nil || isNilNamedType(node.Type) {
		panic(fmt.Sprintf("[Lower Error] struct literal has no type at %d:%d", node.Token.Line, node.Token.Col))
	}
	stType := e.root.semaCtx.ResolveType(node.Type).(*sema.StructType)
	for _, field := range node.Fields {
		if field == nil || field.Name == nil {
			continue
		}
		for _, declared := range stType.Fields {
			if declared.Name == field.Name.Value {
				annotateNestedLiteralType(field.Value, declared.Type)
				break
			}
		}
	}
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
		if ptr, exists := e.root.symbols[astIDValue(id)]; exists {
			valueType := ptr.Type().(*sema.PointerType).Base
			if declaredType, known := e.root.symbolTypes[astIDValue(id)]; known {
				valueType = declaredType
			}
			if _, isPtr := valueType.(*sema.PointerType); isPtr {
				loadReg := e.root.nextReg(valueType)
				e.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: ptr})
				return loadReg
			}
			return ptr
		}
		if gName, g, exists := e.lookupGlobal(astIDValue(id)); exists {
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
		if ptr, ok := e.root.symbols[astIDValue(node)]; ok {
			return ptr
		}
		if gName, g, ok := e.lookupGlobal(astIDValue(node)); ok {
			return &hir.GlobalVar{Name: gName, Typ: &sema.PointerType{Base: g}}
		}
		file := e.root.sourceFile
		if file == "" {
			file = "input.hike"
		}
		panic(fmt.Sprintf("%s:%d:%d: undefined identifier for lvalue: %s", file, node.Token.Line, node.Token.Col, astIDValue(node)))

	case *ast.MemberExpr:
		if pkgID, ok := node.Object.(*ast.Identifier); ok {
			qualified := pkgID.Value + "_" + node.Field.Value
			if gType, exists := e.root.semaCtx.Globals[qualified]; exists {
				return &hir.GlobalVar{Name: qualified, Typ: &sema.PointerType{Base: gType}}
			}
		}
		basePtr := e.LowerStructPtr(node.Object)
		baseType := basePtr.Type().(*sema.PointerType).Base
		st, sName := e.root.findStruct(baseType)
		if st == nil {
			file := e.root.sourceFile
			if file == "" {
				file = "input.hike"
			}
			panic(fmt.Sprintf("%s:%d:%d: type '%s' has no fields", file, node.Field.Token.Line, node.Field.Token.Col, semaTypeName(baseType)))
		}
		fieldPtr, _, _, found := e.ResolveFieldPath(st, sName, basePtr, node.Field.Value)
		if !found {
			file := e.root.sourceFile
			if file == "" {
				file = "input.hike"
			}
			panic(fmt.Sprintf("%s:%d:%d: field '%s' not found on struct '%s'", file, node.Field.Token.Line, node.Field.Token.Col, node.Field.Value, sName))
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

	case *ast.GenericInstExpr:
		// A specialized generic variable/member may remain wrapped in a
		// GenericInstExpr after transformation. Its storage is still the
		// storage of the underlying expression, so resolve that expression
		// as the lvalue rather than rejecting the wrapper.
		return e.LowerLValue(node.Left)

	case *ast.IndexExpr:
		idxVal := e.LowerExpr(node.Index)
		leftVal := e.LowerExpr(node.Left)
		// Prefer the semantic type of the source expression. This matters for
		// Go-style `&(*slice)[i]` and `&(*array)[i]`, where lowering the
		// dereference first can lose the named slice/array type information.
		leftType := e.root.semaCtx.InferExprType(node.Left, e.root.symbolTypes)
		if leftType == nil || sema.IsBad(leftType) {
			leftType = leftVal.Type()
		}

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

		if leftType == sema.TypeString || leftType == sema.TypeCString || semaTypeName(leftType) == "string" || semaTypeName(leftType) == "cstring" {
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

		panic(fmt.Sprintf("[Lower Error] cannot index type '%s' as lvalue (left=%T at %d:%d)",
			semaTypeName(leftType), node.Left, node.Token.Line, node.Token.Col))

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
		rightBB := e.root.newBlock("logical.and.right")
		endBB := e.root.newBlock("logical.and.end")
		e.root.terminate(&hir.InstrBranch{Cond: leftVal, ThenTarget: rightBB.Label, ElseTarget: endBB.Label})
		e.root.setBlock(rightBB)
		shortCircuit := &hir.IfNode{Label: "logical.and", Cond: leftVal}
		e.root.appendStructuredNode(shortCircuit)
		e.root.pushStructuredFrame(shortCircuit.Label)
		e.root.pushStructuredBody(&shortCircuit.Then)
		rightVal := e.LowerExpr(node.Right)
		e.root.emit(&hir.InstrStore{Val: rightVal, Ptr: resAlloca})
		e.root.popStructuredBody()
		e.root.popStructuredFrame()
		if e.root.curBlock.Terminator == nil {
			e.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}
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
		rightBB := e.root.newBlock("logical.or.right")
		endBB := e.root.newBlock("logical.or.end")
		e.root.terminate(&hir.InstrBranch{Cond: leftVal, ThenTarget: endBB.Label, ElseTarget: rightBB.Label})
		shortCircuit := &hir.IfNode{Label: "logical.or", Cond: leftVal}
		e.root.appendStructuredNode(shortCircuit)
		e.root.pushStructuredFrame(shortCircuit.Label)
		e.root.pushStructuredBody(&shortCircuit.Else)
		e.root.setBlock(rightBB)
		rightVal := e.LowerExpr(node.Right)
		e.root.emit(&hir.InstrStore{Val: rightVal, Ptr: resAlloca})
		e.root.popStructuredBody()
		e.root.popStructuredFrame()
		if e.root.curBlock.Terminator == nil {
			e.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}
		e.root.setBlock(endBB)
		finalReg := e.root.nextReg(sema.TypeBool)
		e.root.emit(&hir.InstrLoad{Dst: finalReg, Ptr: resAlloca})
		return finalReg
	}

	leftVal := e.LowerExpr(node.Left)
	rightVal := e.LowerExpr(node.Right)
	leftReg, leftIsReg := leftVal.(*hir.Reg)
	rightReg, rightIsReg := rightVal.(*hir.Reg)
	if leftVal == nil || rightVal == nil || (leftIsReg && leftReg == nil) || (rightIsReg && rightReg == nil) || leftVal.Type() == nil || rightVal.Type() == nil {
		panic(fmt.Sprintf("[Lower Error] binary operand is nil: op=%s left=%T right=%T at %d:%d", node.Operator, node.Left, node.Right, node.Token.Line, node.Token.Col))
	}

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
			dataIndex := 0
			if iface, ok := leftVal.Type().(*sema.InterfaceType); ok && iface.IsAny() {
				dataIndex = 1
			}
			e.root.emit(&hir.InstrExtractValue{Dst: dataPtr, Agg: leftVal, Index: dataIndex})
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
			dataIndex := 0
			if iface, ok := rightVal.Type().(*sema.InterfaceType); ok && iface.IsAny() {
				dataIndex = 1
			}
			e.root.emit(&hir.InstrExtractValue{Dst: dataPtr, Agg: rightVal, Index: dataIndex})
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
				leftDataIndex := 0
				rightDataIndex := 0
				if leftIface.IsAny() {
					leftDataIndex = 1
				}
				if rightIface.IsAny() {
					rightDataIndex = 1
				}
				e.root.emit(&hir.InstrExtractValue{Dst: data1, Agg: leftVal, Index: leftDataIndex})
				e.root.emit(&hir.InstrExtractValue{Dst: data2, Agg: rightVal, Index: rightDataIndex})

				dataEq := e.root.nextReg(sema.TypeBool)
				e.root.emit(&hir.InstrBinary{Dst: dataEq, Op: hir.OpEq, L: data1, R: data2})

				var metaEq *hir.Reg
				if leftIface.IsAny() && rightIface.IsAny() {
					meta1 := e.root.nextReg(sema.TypeInt32)
					meta2 := e.root.nextReg(sema.TypeInt32)
					e.root.emit(&hir.InstrExtractValue{Dst: meta1, Agg: leftVal, Index: 0})
					e.root.emit(&hir.InstrExtractValue{Dst: meta2, Agg: rightVal, Index: 0})
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

	// 両辺がstring型の場合のみhike_streq / hike_strcatを呼ぶ。
	// NamedTypeなど、sema.TypeStringと同一ポインタではないstring相当型も
	// ここで文字列として扱う必要がある。そうでないと通常のi32.addへ
	// フォールバックし、文字列ビューのアドレス同士を加算してしまう。
	if e.root.isStringType(leftVal.Type()) && e.root.isStringType(rightVal.Type()) {
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
		width := integerBitWidth(sema.LLVMTypeOf(valueType))
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
	width, err := strconv.Atoi(strings.TrimPrefix(llvmType, "i"))
	if err != nil || width <= 0 {
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
	targetTypeID := targetType.TypeID(e.root.semaCtx)

	dataPtrReg := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	typeIDReg := e.root.nextReg(sema.TypeInt32)

	if it, ok := ifaceType.(*sema.InterfaceType); ok && !it.IsAny() {
		itabRawReg := e.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e.root.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: ifaceVal, Index: 0})
		e.root.emit(&hir.InstrExtractValue{Dst: itabRawReg, Agg: ifaceVal, Index: 1})
		typeIDPtr := e.root.nextReg(&sema.PointerType{Base: sema.TypeInt32})
		e.root.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabRawReg, ToType: &sema.PointerType{Base: sema.TypeInt32}})
		e.root.emit(&hir.InstrLoad{Dst: typeIDReg, Ptr: typeIDPtr})
	} else {
		e.root.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: ifaceVal, Index: 1})
		e.root.emit(&hir.InstrExtractValue{Dst: typeIDReg, Agg: ifaceVal, Index: 0})
	}

	matchReg := e.root.nextReg(sema.TypeBool)
	e.root.emit(&hir.InstrBinary{Dst: matchReg, Op: hir.OpEq, L: typeIDReg, R: &hir.ConstInt{Val: targetTypeID, Typ: sema.TypeInt32}})
	if trapOnFailure {
		okBB := e.root.newBlock("typeassert.ok")
		failBB := e.root.newBlock("typeassert.fail")
		var structuredAssert *hir.IfNode
		if len(e.root.structuredStack) > 0 {
			structuredAssert = &hir.IfNode{Label: okBB.Label, Cond: matchReg}
			e.root.appendStructuredNode(structuredAssert)
			e.root.pushStructuredFrame(structuredAssert.Label)
			e.root.pushStructuredBody(&structuredAssert.Else)
		}
		e.root.terminate(&hir.InstrBranch{Cond: matchReg, ThenTarget: okBB.Label, ElseTarget: failBB.Label})
		e.root.setBlock(failBB)
		e.root.emit(&hir.InstrCallStatic{CalleeName: "llvm.trap"})
		e.root.terminate(&hir.InstrUnreachable{})
		if structuredAssert != nil {
			e.root.popStructuredBody()
			e.root.popStructuredFrame()
		}
		e.root.setBlock(okBB)
	}

	unpackedReg := e.root.nextReg(targetType)
	if strings.HasSuffix(sema.LLVMTypeOf(targetType), "*") {
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
			embTypeName := strings.TrimPrefix(semaTypeName(f.Type), "*")
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
