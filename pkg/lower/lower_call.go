package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

// CallLowerer は関数定義、関数呼び出し、メソッド解決、型キャスト、クロージャ生成を担当する
type CallLowerer struct {
	root *Lowerer
}

func NewCallLowerer(root *Lowerer) *CallLowerer {
	return &CallLowerer{root: root}
}

// -----------------------------------------------------------------------------
// 型解決・型キャスト判定
// -----------------------------------------------------------------------------

// ResolveTypeFromExpr は式ノードから型を解決する（Duration(x) などの型キャスト判定に使用）
func (c *CallLowerer) ResolveTypeFromExpr(e ast.Expression) sema.Type {
	if e == nil {
		return nil
	}
	if te, ok := e.(ast.TypeExpr); ok {
		return c.root.semaCtx.ResolveType(te)
	}
	switch node := e.(type) {
	case *ast.PointerType:
		return c.root.semaCtx.ResolveType(node)
	case *ast.SliceType:
		return c.root.semaCtx.ResolveType(node)
	case *ast.EllipsisType:
		return c.root.semaCtx.ResolveType(node)
	case *ast.ArrayType:
		return c.root.semaCtx.ResolveType(node)
	case *ast.MapType:
		return c.root.semaCtx.ResolveType(node)
	case *ast.NamedType:
		return c.root.semaCtx.ResolveType(node)
	case *ast.FuncType:
		return c.root.semaCtx.ResolveType(node)
	case *ast.Identifier:
		if builtinT, ok := sema.LookupBuiltinType(node.Value); ok {
			return builtinT
		}
		if node.Value == "any" {
			return &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
		}
		if node.Value == "error" {
			if iface, ok := c.root.semaCtx.Interfaces["error"]; ok {
				return iface
			}
			return nil
		}
		if st, _ := c.root.semaCtx.LookupStruct(node.Value); st != nil {
			if st.IsGeneric() {
				return nil
			}
			return st
		}
		if iface, _ := c.root.semaCtx.LookupInterface(node.Value); iface != nil {
			if iface.IsGeneric() {
				return nil
			}
			return iface
		}
		if alias, _ := c.root.semaCtx.LookupAlias(node.Value); alias != nil {
			return alias
		}
		return nil

	case *ast.PrefixExpr:
		if node.Operator == "*" {
			baseT := c.ResolveTypeFromExpr(node.Right)
			if baseT != nil && baseT != sema.TypeVoid {
				return &sema.PointerType{Base: baseT}
			}
		}
		return nil

	case *ast.MemberExpr:
		if pkgId, okPkg := node.Object.(*ast.Identifier); okPkg {
			typeName := pkgId.Value + "_" + node.Field.Value
			if st, _ := c.root.semaCtx.LookupStruct(typeName); st != nil {
				return st
			}
			if iface, _ := c.root.semaCtx.LookupInterface(typeName); iface != nil {
				return iface
			}
			if alias, _ := c.root.semaCtx.LookupAlias(typeName); alias != nil {
				return alias
			}
		}
		return nil
	}
	return nil
}

// -----------------------------------------------------------------------------
// メソッドパス解決
// -----------------------------------------------------------------------------
// ResolveMethod はレシーバ型から対象メソッドを探索する
func (c *CallLowerer) ResolveMethod(recvType sema.Type, methodName string, curPtr hir.Value) (string, *sema.FuncType, hir.Value, bool) {
	if recvType == nil {
		return "", nil, nil, false
	}

	rawTypeName := strings.TrimPrefix(recvType.TypeName(), "*")
	shortTypeName := rawTypeName
	if idx := strings.LastIndex(rawTypeName, "_"); idx != -1 {
		shortTypeName = rawTypeName[idx+1:]
	}

	canonicalTarget := sema.CanonicalMethodName(rawTypeName, methodName)
	if fn, canonical := c.root.semaCtx.LookupFunction(canonicalTarget); fn != nil {
		return canonical, fn, curPtr, true
	}

	candidates := []string{
		canonicalTarget,
		rawTypeName + "_" + methodName,
		shortTypeName + "_" + methodName,
		c.root.hirProg.ModuleName + "_" + rawTypeName + "_" + methodName,
		c.root.hirProg.ModuleName + "_" + shortTypeName + "_" + methodName,
	}

	// エイリアス型名（例: Duration -> int64）に基づく候補も追加
	for aliasName, aliasType := range c.root.semaCtx.Aliases {
		if aliasType.TypeName() == rawTypeName || aliasType.TypeName() == shortTypeName {
			candidates = append(candidates,
				sema.CanonicalMethodName(aliasName, methodName),
				aliasName+"_"+methodName,
				c.root.hirProg.ModuleName+"_"+aliasName+"_"+methodName,
			)
		}
	}

	for _, cand := range candidates {
		if fn, canonical := c.root.semaCtx.LookupFunction(cand); fn != nil {
			return canonical, fn, curPtr, true
		}
	}

	for k, fn := range c.root.semaCtx.Functions {
		if strings.HasSuffix(k, "_"+methodName) {
			if strings.Contains(k, rawTypeName) || strings.Contains(k, shortTypeName) {
				return k, fn, curPtr, true
			}
			for aliasName, aliasType := range c.root.semaCtx.Aliases {
				if aliasType.TypeName() == rawTypeName || aliasType.TypeName() == shortTypeName {
					if strings.Contains(k, aliasName) {
						return k, fn, curPtr, true
					}
				}
			}
		}
	}

	st, _ := c.root.findStruct(recvType)
	if st != nil {
		for i, f := range st.Fields {
			if f.IsEmbedded {
				fieldPtr := c.root.nextReg(&sema.PointerType{Base: f.Type})
				if curPtr != nil {
					c.root.emit(&hir.InstrGetFieldPtr{Dst: fieldPtr, BasePtr: curPtr, FieldIndex: i, FieldName: f.Name})
				}
				nextPtr := hir.Value(fieldPtr)
				if _, isPtr := f.Type.(*sema.PointerType); isPtr {
					loadReg := c.root.nextReg(f.Type)
					if curPtr != nil {
						c.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: fieldPtr})
					}
					nextPtr = loadReg
				}
				if targetName, fnMeta, finalPtr, found := c.ResolveMethod(f.Type, methodName, nextPtr); found {
					return targetName, fnMeta, finalPtr, true
				}
			}
		}
	}

	return "", nil, nil, false
}

// -----------------------------------------------------------------------------
// 引数パッキング & ロワリングヘルパー (Go 可変長引数 & C 可変長引数)
// -----------------------------------------------------------------------------

// lowerVariadicSlice は呼び出し時に渡された個別引数群を一時スライス []T としてメモリ確保・初期化する
func (c *CallLowerer) lowerVariadicSlice(args []ast.Expression, elemType sema.Type) hir.Value {
	if elemType == nil {
		elemType = &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
	}
	slType := &sema.SliceType{Elem: elemType}

	count := len(args)
	if count == 0 {
		return c.root.defaultConstValue(slType)
	}

	elemSize := elemType.Size()
	if elemSize <= 0 {
		elemSize = 8
	}
	totalBytes := count * elemSize

	mallocRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{
		Dst:       mallocRaw,
		Size:      &hir.ConstInt{Val: int64(totalBytes), Typ: sema.TypeInt},
		AllocType: sema.TypeByte,
	})

	typedBase := c.root.nextReg(&sema.PointerType{Base: elemType})
	c.root.emit(&hir.InstrCast{
		Dst:    typedBase,
		Val:    mallocRaw,
		ToType: &sema.PointerType{Base: elemType},
	})

	for i, arg := range args {
		val := c.root.Expr.LowerExpr(arg)
		val = c.root.emitValueCoerce(val, elemType)
		elemPtr := c.root.nextReg(&sema.PointerType{Base: elemType})
		c.root.emit(&hir.InstrGetElemPtr{
			Dst:     elemPtr,
			BasePtr: typedBase,
			Index:   &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt},
		})
		c.root.emit(&hir.InstrStore{Val: val, Ptr: elemPtr})
	}

	t1 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(slType), Val: mallocRaw, Index: 0})
	t2 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: &hir.ConstInt{Val: int64(count), Typ: sema.TypeInt}, Index: 1})
	t3 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: &hir.ConstInt{Val: int64(count), Typ: sema.TypeInt}, Index: 2})
	return t3
}

// lowerArgs は実引数式群を型チェック・キャストおよび可変長引数のパッキング/プロモーションを行って IR 値スライスを構築する
func (c *CallLowerer) lowerArgs(callArgs []ast.Expression, paramTypes []sema.Type, isVariadic bool, isCFunc bool, variadicElem sema.Type, hasEllipsis bool) []hir.Value {
	// C 言語スタイルの可変長引数 (C ABI: スタック展開 & 型昇格)
	if isCFunc && isVariadic {
		args := make([]hir.Value, 0, len(callArgs))
		for i, arg := range callArgs {
			// パススルー構文 "..." が C 関数へ渡された場合、親関数のスライスではなく個々の値を直接渡す
			if id, ok := arg.(*ast.Identifier); ok && id.Value == "..." {
				continue
			}
			val := c.root.Expr.LowerExpr(arg)
			if i >= len(paramTypes) {
				// C 可変長引数のプロモーション (i1/i8/i16/i32 -> i64, float -> double)
				if val.Type() == sema.TypeBool || val.Type().LLVMType() == "i1" {
					extReg := c.root.nextReg(sema.TypeInt)
					c.root.emit(&hir.InstrCast{Dst: extReg, Val: val, ToType: sema.TypeInt})
					val = extReg
				} else if val.Type() == sema.TypeByte || val.Type().LLVMType() == "i8" {
					extReg := c.root.nextReg(sema.TypeInt)
					c.root.emit(&hir.InstrCast{Dst: extReg, Val: val, ToType: sema.TypeInt})
					val = extReg
				} else if val.Type() == sema.TypeFloat32 || val.Type().LLVMType() == "float" {
					extReg := c.root.nextReg(sema.TypeFloat64)
					c.root.emit(&hir.InstrCast{Dst: extReg, Val: val, ToType: sema.TypeFloat64})
					val = extReg
				}
			} else {
				val = c.root.emitValueCoerce(val, paramTypes[i])
			}
			args = append(args, val)
		}
		return args
	}

	// Go 言語仕様の可変長引数 (Go ABI: 末尾引数は []T スライス)
	if isVariadic && len(paramTypes) > 0 {
		fixedCount := len(paramTypes) - 1
		args := make([]hir.Value, 0, fixedCount+1)
		for i := 0; i < fixedCount && i < len(callArgs); i++ {
			val := c.root.Expr.LowerExpr(callArgs[i])
			val = c.root.emitValueCoerce(val, paramTypes[i])
			args = append(args, val)
		}

		if hasEllipsis {
			if len(callArgs) > fixedCount {
				lastArg := callArgs[fixedCount]
				if id, ok := lastArg.(*ast.Identifier); ok && id.Value == "..." {
					// 可変長引数の転送 (パススルー): 親関数が可変長スライスを受け取っていればそれをそのまま転送
					if c.root.curFunc != nil && len(c.root.curFunc.Params) > 0 && c.root.curFunc.IsVariadic {
						parentSlice := c.root.curFunc.Params[len(c.root.curFunc.Params)-1]
						args = append(args, parentSlice)
					} else {
						args = append(args, c.root.defaultConstValue(paramTypes[fixedCount]))
					}
				} else {
					sliceVal := c.root.Expr.LowerExpr(lastArg)
					sliceVal = c.root.emitValueCoerce(sliceVal, paramTypes[fixedCount])
					args = append(args, sliceVal)
				}
			} else {
				args = append(args, c.root.defaultConstValue(paramTypes[fixedCount]))
			}
		} else {
			var varArgs []ast.Expression
			if len(callArgs) > fixedCount {
				varArgs = callArgs[fixedCount:]
			}
			elemType := variadicElem
			if elemType == nil {
				if sl, isSl := paramTypes[fixedCount].(*sema.SliceType); isSl {
					elemType = sl.Elem
				}
			}
			sliceVal := c.lowerVariadicSlice(varArgs, elemType)
			args = append(args, sliceVal)
		}
		return args
	}

	// 通常の固定引数関数
	args := make([]hir.Value, len(callArgs))
	for i, arg := range callArgs {
		val := c.root.Expr.LowerExpr(arg)
		if i < len(paramTypes) {
			val = c.root.emitValueCoerce(val, paramTypes[i])
		}
		args[i] = val
	}
	return args
}

// -----------------------------------------------------------------------------
// 関数・メソッド呼び出し (Call)
// -----------------------------------------------------------------------------
func (c *CallLowerer) LowerCall(call *ast.CallExpr) hir.Value {
	// 1. 型キャスト呼び出し (例: Duration(ns), int64(x), string(bytes))
	if len(call.Args) == 1 {
		targetType := c.ResolveTypeFromExpr(call.Function)
		if targetType != nil && targetType != sema.TypeVoid {
			if _, isFunc := targetType.(*sema.FuncType); !isFunc {
				argVal := c.root.Expr.LowerExpr(call.Args[0])
				if argVal.Type().LLVMType() == targetType.LLVMType() {
					return argVal
				}
				if _, isSlice := argVal.Type().(*sema.SliceType); isSlice && targetType == sema.TypeString {
					dst := c.root.nextReg(sema.TypeString)
					c.root.emit(&hir.InstrExtractValue{Dst: dst, Agg: argVal, Index: 0})
					return dst
				}
				dst := c.root.nextReg(targetType)
				c.root.emit(&hir.InstrCast{Dst: dst, Val: argVal, ToType: targetType})
				return dst
			}
		}
	}

	// 2. 言語組み込み関数 (make, close, delete, len, cap, append, string)
	if fnId, ok := call.Function.(*ast.Identifier); ok {
		switch fnId.Value {
		case "make":
			var chanTypeNode *ast.ChanType
			if ct, ok := call.Args[0].(*ast.ChanType); ok {
				chanTypeNode = ct
			} else if ice, ok := call.Args[0].(*ast.ImplicitCastExpr); ok {
				if ct, ok := ice.Expr.(*ast.ChanType); ok {
					chanTypeNode = ct
				}
			}

			if chanTypeNode != nil {
				elemType := c.root.semaCtx.ResolveType(chanTypeNode.Elem)
				resChanType := &sema.ChanType{Elem: elemType}
				capVal := hir.Value(&hir.ConstInt{Val: 0, Typ: sema.TypeInt})
				if len(call.Args) >= 2 {
					capVal = c.root.Expr.LowerExpr(call.Args[1])
				}
				dst := c.root.nextReg(resChanType)
				c.root.emit(&hir.InstrChanMake{Dst: dst, ElemType: elemType, Cap: capVal})
				return dst
			}

			if mapTypeNode, okMap := call.Args[0].(*ast.MapType); okMap {
				kType := c.root.semaCtx.ResolveType(mapTypeNode.Key)
				vType := c.root.semaCtx.ResolveType(mapTypeNode.Value)
				resMapType := &sema.MapType{Key: kType, Value: vType}
				isStr := 0
				if kType == sema.TypeString {
					isStr = 1
				}
				capVal := hir.Value(&hir.ConstInt{Val: 16, Typ: sema.TypeInt})
				if len(call.Args) >= 2 {
					capVal = c.root.Expr.LowerExpr(call.Args[1])
				}
				dst := c.root.nextReg(resMapType)
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "__hike_map_create", Args: []hir.Value{capVal, &hir.ConstInt{Val: int64(isStr), Typ: sema.TypeInt}}})
				return dst
			}
			if slNode, okSlice := call.Args[0].(*ast.SliceType); okSlice {
				elemType := c.root.semaCtx.ResolveType(slNode.Elem)
				resSliceType := &sema.SliceType{Elem: elemType}
				lenVal := c.root.Expr.LowerExpr(call.Args[1])
				capVal := lenVal
				if len(call.Args) >= 3 {
					capVal = c.root.Expr.LowerExpr(call.Args[2])
				}
				elemSize := elemType.Size()
				if elemSize <= 0 {
					elemSize = 1
				}
				callocRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
				c.root.emit(&hir.InstrCallStatic{Dst: callocRaw, CalleeName: "calloc", Args: []hir.Value{capVal, &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}}})

				t1 := c.root.nextReg(resSliceType)
				c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(resSliceType), Val: callocRaw, Index: 0})
				t2 := c.root.nextReg(resSliceType)
				c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: lenVal, Index: 1})
				t3 := c.root.nextReg(resSliceType)
				c.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: capVal, Index: 2})
				return t3
			}

		case "close":
			if len(call.Args) > 0 {
				chVal := c.root.Expr.LowerExpr(call.Args[0])
				c.root.emit(&hir.InstrChanClose{Chan: chVal})
				return nil
			}

		case "delete":
			argVal := c.root.Expr.LowerExpr(call.Args[0])
			keyVal := c.root.Expr.LowerExpr(call.Args[1])
			if mp, isMap := argVal.Type().(*sema.MapType); isMap {
				keyI64 := c.root.coerceToI64(keyVal, mp.Key)
				c.root.emit(&hir.InstrCallStatic{CalleeName: "__hike_map_delete", Args: []hir.Value{argVal, keyI64}})
				return nil
			}

		case "len", "cap":
			argVal := c.root.Expr.LowerExpr(call.Args[0])
			if _, isSlice := argVal.Type().(*sema.SliceType); isSlice {
				idx := 1
				if fnId.Value == "cap" {
					idx = 2
				}
				dst := c.root.nextReg(sema.TypeInt)
				c.root.emit(&hir.InstrExtractValue{Dst: dst, Agg: argVal, Index: idx})
				return dst
			}
			if fnId.Value == "len" && argVal.Type() == sema.TypeString {
				dst := c.root.nextReg(sema.TypeInt)
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "strlen", Args: []hir.Value{argVal}})
				return dst
			}
			if _, isMap := argVal.Type().(*sema.MapType); isMap {
				dst := c.root.nextReg(sema.TypeInt)
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "__hike_map_len", Args: []hir.Value{argVal}})
				return dst
			}

		case "append":
			return c.LowerAppend(call)

		case "string":
			argVal := c.root.Expr.LowerExpr(call.Args[0])
			if _, isSlice := argVal.Type().(*sema.SliceType); isSlice {
				dst := c.root.nextReg(sema.TypeString)
				c.root.emit(&hir.InstrExtractValue{Dst: dst, Agg: argVal, Index: 0})
				return dst
			}
			return argVal
		}
	}

	// 3. メンバー式経由の呼び出し (パッケージ関数呼び出し、またはオブジェクトメソッド呼び出し)
	if mem, ok := call.Function.(*ast.MemberExpr); ok {
		// 3A. パッケージ名修飾による関数呼び出し (例: fmt.Printf, time.Now)
		if pkgIdent, isIdent := mem.Object.(*ast.Identifier); isIdent && c.isPackageName(pkgIdent.Value) {
			methodName := mem.Field.Value
			targetFnName := pkgIdent.Value + "_" + methodName
			targetFn, canonicalName := c.root.semaCtx.LookupFunction(targetFnName)
			if targetFn == nil {
				targetFn, canonicalName = c.root.semaCtx.LookupFunction(methodName)
			}

			if targetFn == nil {
				panic(fmt.Sprintf("[Lower Error] undefined function: %s.%s", pkgIdent.Value, methodName))
			}

			isCVarArg := targetFn.IsCFunc || (targetFn.IsVariadic && targetFn.VariadicElem == nil)
			args := c.lowerArgs(call.Args, targetFn.ParamTypes, targetFn.IsVariadic, isCVarArg, targetFn.VariadicElem, call.HasEllipsis)

			var retType sema.Type = sema.TypeVoid
			if len(targetFn.ReturnTypes) == 1 {
				retType = targetFn.ReturnTypes[0]
			} else if len(targetFn.ReturnTypes) > 1 {
				retType = &sema.TupleType{Types: targetFn.ReturnTypes}
			}

			var dst *hir.Reg = nil
			if retType != sema.TypeVoid {
				dst = c.root.nextReg(retType)
			}

			callee := canonicalName
			if targetFn.IsCFunc && targetFn.CFuncTarget != "" {
				callee = targetFn.CFuncTarget
			} else if targetFn.IsCFunc {
				callee = "c_" + canonicalName
			} else if targetFnName == "fmt_Printf" {
				// 可変長引数が中間ラッパーで消失するのを防ぐため、libc の printf へ直接転送
				callee = "printf"
			}

			c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: callee, Args: args})
			return dst
		}

		// 3B. インターフェースまたは構造体/基本型のメソッド呼び出し
		objPtr := c.root.Expr.LowerStructPtr(mem.Object)
		objType := objPtr.Type().(*sema.PointerType).Base

		// インターフェースメソッド呼び出し
		if iface, isIface := objType.(*sema.InterfaceType); isIface && !iface.IsAny() {
			methodIdx := 0
			var targetMethod sema.Method
			for idx, m := range iface.Methods {
				if m.Name == mem.Field.Value {
					methodIdx = idx
					targetMethod = m
					break
				}
			}

			args := c.lowerArgs(call.Args, targetMethod.ParamTypes, targetMethod.IsVariadic, false, targetMethod.VariadicElem, call.HasEllipsis)

			var retType sema.Type = sema.TypeVoid
			if len(targetMethod.ReturnTypes) == 1 {
				retType = targetMethod.ReturnTypes[0]
			} else if len(targetMethod.ReturnTypes) > 1 {
				retType = &sema.TupleType{Types: targetMethod.ReturnTypes}
			}

			var dst *hir.Reg = nil
			if retType != sema.TypeVoid {
				dst = c.root.nextReg(retType)
			}

			ifaceVal := c.root.nextReg(iface)
			c.root.emit(&hir.InstrLoad{Dst: ifaceVal, Ptr: objPtr})

			c.root.emit(&hir.InstrCallIface{
				Dst:         dst,
				IfaceVal:    ifaceVal,
				MethodIndex: methodIdx,
				MethodName:  mem.Field.Value,
				Args:        args,
			})
			return dst
		}

		// 静的型メソッド呼び出し (構造体および基本型エイリアス)
		targetFnName, targetFn, finalRecv, found := c.ResolveMethod(objType, mem.Field.Value, objPtr)
		if found && targetFn != nil {
			isPtrRecv := false
			if len(targetFn.ParamTypes) > 0 {
				_, isPtrRecv = targetFn.ParamTypes[0].(*sema.PointerType)
			}

			recvArg := finalRecv
			if !isPtrRecv {
				if ptrType, ok := finalRecv.Type().(*sema.PointerType); ok {
					loaded := c.root.nextReg(ptrType.Base)
					c.root.emit(&hir.InstrLoad{Dst: loaded, Ptr: finalRecv})
					recvArg = loaded
				}
			} else {
				if _, ok := finalRecv.Type().(*sema.PointerType); !ok {
					allocaTmp := c.root.nextReg(&sema.PointerType{Base: finalRecv.Type()})
					c.root.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: finalRecv.Type()})
					c.root.emit(&hir.InstrStore{Val: finalRecv, Ptr: allocaTmp})
					recvArg = allocaTmp
				}
			}

			methodParamTypes := []sema.Type{}
			if len(targetFn.ParamTypes) > 1 {
				methodParamTypes = targetFn.ParamTypes[1:]
			}
			isCVarArg := targetFn.IsCFunc || (targetFn.IsVariadic && targetFn.VariadicElem == nil)
			callArgVals := c.lowerArgs(call.Args, methodParamTypes, targetFn.IsVariadic, isCVarArg, targetFn.VariadicElem, call.HasEllipsis)
			args := append([]hir.Value{recvArg}, callArgVals...)

			var retType sema.Type = sema.TypeVoid
			if len(targetFn.ReturnTypes) == 1 {
				retType = targetFn.ReturnTypes[0]
			} else if len(targetFn.ReturnTypes) > 1 {
				retType = &sema.TupleType{Types: targetFn.ReturnTypes}
			}

			var dst *hir.Reg = nil
			if retType != sema.TypeVoid {
				dst = c.root.nextReg(retType)
			}
			c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: targetFnName, Args: args})
			return dst
		}

		// 3C. 構造体フィールドに関数ポインタが格納されている場合 (クロージャフィールド呼び出し)
		st, sName := c.root.findStruct(objType)
		if st != nil {
			fieldPtr, fieldType, _, fieldFound := c.root.Expr.ResolveFieldPath(st, sName, objPtr, mem.Field.Value)
			if fieldFound {
				if ft, ok := fieldType.(*sema.FuncType); ok {
					loadedFn := c.root.nextReg(ft)
					c.root.emit(&hir.InstrLoad{Dst: loadedFn, Ptr: fieldPtr})
					return c.lowerIndirectCall(loadedFn, call.Args, call.HasEllipsis)
				}
			}
		}

		panic(fmt.Sprintf("[Lower Error] method or function field '%s' not found on type '%s'", mem.Field.Value, objType.TypeName()))
	}

	// 4. 単一識別子によるトップレベル関数呼び出し (例: myFunc())
	if fnId, ok := call.Function.(*ast.Identifier); ok {
		_, isLocal := c.root.symbols[fnId.Value]
		_, isGlobal := c.root.semaCtx.Globals[fnId.Value]
		if !isLocal && !isGlobal {
			targetFn, canonicalName := c.root.semaCtx.LookupFunction(fnId.Value)
			if targetFn != nil {
				isCVarArg := targetFn.IsCFunc || (targetFn.IsVariadic && targetFn.VariadicElem == nil)
				args := c.lowerArgs(call.Args, targetFn.ParamTypes, targetFn.IsVariadic, isCVarArg, targetFn.VariadicElem, call.HasEllipsis)

				var retType sema.Type = sema.TypeVoid
				if len(targetFn.ReturnTypes) == 1 {
					retType = targetFn.ReturnTypes[0]
				} else if len(targetFn.ReturnTypes) > 1 {
					retType = &sema.TupleType{Types: targetFn.ReturnTypes}
				}

				var dst *hir.Reg = nil
				if retType != sema.TypeVoid {
					dst = c.root.nextReg(retType)
				}
				callee := canonicalName
				if targetFn.IsCFunc && targetFn.CFuncTarget != "" {
					callee = targetFn.CFuncTarget
				} else if targetFn.IsCFunc {
					callee = "c_" + canonicalName
				}
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: callee, Args: args})
				return dst
			}
		}
	}

	// 5. 間接関数ポインタ / クロージャ呼び出し (例: act())
	fnFatPtr := c.root.Expr.LowerExpr(call.Function)
	return c.lowerIndirectCall(fnFatPtr, call.Args, call.HasEllipsis)
}

func (c *CallLowerer) isPackageName(name string) bool {
	for _, imp := range c.root.prog.Imports {
		// "std/fmt" -> "fmt", "time" -> "time"
		pkgName := imp.Path
		if idx := strings.LastIndex(pkgName, "/"); idx != -1 {
			pkgName = pkgName[idx+1:]
		}
		if pkgName == name {
			return true
		}
	}
	return false
}

func (c *CallLowerer) lowerIndirectCall(fnFatPtr hir.Value, callArgs []ast.Expression, hasEllipsis bool) hir.Value {
	fnPtrReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	envPtrReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrExtractValue{Dst: fnPtrReg, Agg: fnFatPtr, Index: 0})
	c.root.emit(&hir.InstrExtractValue{Dst: envPtrReg, Agg: fnFatPtr, Index: 1})

	ft, _ := fnFatPtr.Type().(*sema.FuncType)
	var isVariadic bool
	var variadicElem sema.Type
	var paramTypes []sema.Type
	if ft != nil {
		isVariadic = ft.IsVariadic
		variadicElem = ft.VariadicElem
		paramTypes = ft.ParamTypes
	}
	args := c.lowerArgs(callArgs, paramTypes, isVariadic, false, variadicElem, hasEllipsis)

	var retType sema.Type = sema.TypeVoid
	if ft != nil {
		if len(ft.ReturnTypes) == 1 {
			retType = ft.ReturnTypes[0]
		} else if len(ft.ReturnTypes) > 1 {
			retType = &sema.TupleType{Types: ft.ReturnTypes}
		}
	}

	var dst *hir.Reg = nil
	if retType != sema.TypeVoid {
		dst = c.root.nextReg(retType)
	}
	c.root.emit(&hir.InstrCallIndirect{
		Dst:    dst,
		FnPtr:  fnPtrReg,
		EnvPtr: envPtrReg,
		Args:   args,
	})
	return dst
}

// -----------------------------------------------------------------------------
// 関数定義 (Function Declaration) の変換
// -----------------------------------------------------------------------------
func (c *CallLowerer) LowerFunc(fn *ast.FuncDecl) {
	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	c.root.deferStack = []*ast.CallExpr{}
	c.root.regCount = 0
	if fn.Body != nil {
		c.root.escapedVars = sema.CollectAllCapturesInBlock(fn.Body)
	} else {
		c.root.escapedVars = make(map[string]bool)
	}

	fnName := fn.Name.Value
	var recvType sema.Type = nil
	if fn.Receiver != nil {
		recvType = c.root.semaCtx.ResolveType(fn.Receiver.Type)
		recvName := strings.TrimPrefix(recvType.TypeName(), "*")
		fnName = sema.CanonicalMethodName(recvName, fnName)
	}

	isMain := (fn.Name.Value == "main")
	returnTypes := []sema.Type{}
	if isMain {
		returnTypes = []sema.Type{sema.TypeInt}
	} else if fnType := c.root.semaCtx.Functions[fnName]; fnType != nil {
		returnTypes = fnType.ReturnTypes
	} else {
		for _, rt := range fn.ReturnTypes {
			returnTypes = append(returnTypes, c.root.semaCtx.ResolveType(rt))
		}
	}

	// Go 仕様の関数本体を持つ関数は LLVM IR 上では通常スライス引数を受け取るため、本体なしの extern 宣言のみ IR 可変長フラグを有効にする
	isIRVariadic := (fn.Body == nil && fn.IsVariadic)

	hirFn := &hir.Function{
		Name:        fnName,
		Params:      []*hir.Reg{},
		ReturnTypes: returnTypes,
		Blocks:      []*hir.BasicBlock{},
		IsVariadic:  isIRVariadic,
		IsExtern:    (fn.Body == nil),
	}
	c.root.curFunc = hirFn
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)

	// 本体を持たない外部関数宣言 (extern) の場合
	if fn.Body == nil {
		// ランタイム内部ですでに define されている OS 組み込み関数は declare 出力対象から除外する
		switch fn.Name.Value {
		case "os_now_ns", "os_sleep_ms":
			return
		}

		hirFn.Blocks = nil
		for i, p := range fn.Params {
			pType := c.root.semaCtx.ResolveType(p.Type)
			if p.IsVariadic {
				if _, isSlice := pType.(*sema.SliceType); !isSlice {
					pType = &sema.SliceType{Elem: pType}
				}
			}
			hirFn.Params = append(hirFn.Params, &hir.Reg{ID: i + 1, Typ: pType, Name: p.Name.Value})
		}
		return
	}

	entryBB := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(entryBB)

	if isMain {
		argc32Reg := c.root.nextReg(&sema.BasicType{Name: "int32", ByteSize: 4, LLVM: "i32"}, "argc")
		argvReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeString}, "argv")
		hirFn.Params = append(hirFn.Params, argc32Reg, argvReg)

		argcReg := c.root.nextReg(sema.TypeInt, "argc64")
		c.root.emit(&hir.InstrCast{Dst: argcReg, Val: argc32Reg, ToType: sema.TypeInt})

		if _, exists := c.root.semaCtx.Globals["os_Args"]; exists {
			callocRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			c.root.emit(&hir.InstrCallStatic{Dst: callocRaw, CalleeName: "calloc", Args: []hir.Value{argcReg, &hir.ConstInt{Val: 8, Typ: sema.TypeInt}}})

			callocRes := c.root.nextReg(&sema.PointerType{Base: sema.TypeString})
			c.root.emit(&hir.InstrCast{Dst: callocRes, Val: callocRaw, ToType: &sema.PointerType{Base: sema.TypeString}})

			loopCondBB := c.root.newBlock("argv.loop.cond")
			loopBodyBB := c.root.newBlock("argv.loop.body")
			loopEndBB := c.root.newBlock("argv.loop.end")

			idxAlloca := c.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "i.arg")
			c.root.emit(&hir.InstrAlloca{Dst: idxAlloca, AllocType: sema.TypeInt})
			c.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: idxAlloca})
			c.root.terminate(&hir.InstrJump{Target: loopCondBB.Label})

			c.root.setBlock(loopCondBB)
			curI := c.root.nextReg(sema.TypeInt)
			c.root.emit(&hir.InstrLoad{Dst: curI, Ptr: idxAlloca})
			cmp := c.root.nextReg(sema.TypeBool)
			c.root.emit(&hir.InstrBinary{Dst: cmp, Op: hir.OpLt, L: curI, R: argcReg})
			c.root.terminate(&hir.InstrBranch{Cond: cmp, ThenTarget: loopBodyBB.Label, ElseTarget: loopEndBB.Label})

			c.root.setBlock(loopBodyBB)
			srcElemPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeString})
			c.root.emit(&hir.InstrGetElemPtr{Dst: srcElemPtr, BasePtr: argvReg, Index: curI})
			argStr := c.root.nextReg(sema.TypeString)
			c.root.emit(&hir.InstrLoad{Dst: argStr, Ptr: srcElemPtr})

			dstElemPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeString})
			c.root.emit(&hir.InstrGetElemPtr{Dst: dstElemPtr, BasePtr: callocRes, Index: curI})
			c.root.emit(&hir.InstrStore{Val: argStr, Ptr: dstElemPtr})

			nextI := c.root.nextReg(sema.TypeInt)
			c.root.emit(&hir.InstrBinary{Dst: nextI, Op: hir.OpAdd, L: curI, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
			c.root.emit(&hir.InstrStore{Val: nextI, Ptr: idxAlloca})
			c.root.terminate(&hir.InstrJump{Target: loopCondBB.Label})

			c.root.setBlock(loopEndBB)
			sliceType := &sema.SliceType{Elem: sema.TypeString}
			t1 := c.root.nextReg(sliceType)
			c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(sliceType), Val: callocRaw, Index: 0})
			t2 := c.root.nextReg(sliceType)
			c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: argcReg, Index: 1})
			t3 := c.root.nextReg(sliceType)
			c.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: argcReg, Index: 2})
			c.root.emit(&hir.InstrStore{Val: t3, Ptr: &hir.GlobalVar{Name: "os_Args", Typ: &sema.PointerType{Base: sliceType}}})
		}
	} else {
		if fn.Receiver != nil {
			paramReg := c.root.nextReg(recvType, fn.Receiver.Name.Value+"_arg")
			hirFn.Params = append(hirFn.Params, paramReg)

			ptrReg := c.root.nextReg(&sema.PointerType{Base: recvType}, fn.Receiver.Name.Value)
			if fn.Receiver.IsEscaped {
				sizeVal := &hir.ConstInt{Val: int64(recvType.Size()), Typ: sema.TypeInt}
				c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: recvType})
			} else {
				c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: recvType})
			}
			c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
			c.root.symbols[fn.Receiver.Name.Value] = ptrReg
			c.root.symbolTypes[fn.Receiver.Name.Value] = recvType
		}

		for _, p := range fn.Params {
			pType := c.root.semaCtx.ResolveType(p.Type)
			// Go 仕様: パラメータ自身が可変長 (...T) の場合のみスライス []T として扱う
			if p.IsVariadic {
				if _, isSlice := pType.(*sema.SliceType); !isSlice {
					pType = &sema.SliceType{Elem: pType}
				}
			}
			paramReg := c.root.nextReg(pType, p.Name.Value+"_arg")
			hirFn.Params = append(hirFn.Params, paramReg)

			ptrReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
			if p.IsEscaped || c.root.escapedVars[p.Name.Value] {
				sizeVal := &hir.ConstInt{Val: int64(pType.Size()), Typ: sema.TypeInt}
				c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: pType})
			} else {
				c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: pType})
			}
			c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
			c.root.symbols[p.Name.Value] = ptrReg
			c.root.symbolTypes[p.Name.Value] = pType
		}
	}

	for _, stmt := range fn.Body.Statements {
		c.root.Stmt.LowerStmt(stmt)
	}

	for i := len(c.root.deferStack) - 1; i >= 0; i-- {
		c.LowerCall(c.root.deferStack[i])
	}

	if c.root.curBlock.Terminator == nil {
		if isMain {
			c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{&hir.ConstInt{Val: 0, Typ: sema.TypeInt}}})
		} else if len(returnTypes) == 0 {
			c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		} else {
			defaults := make([]hir.Value, len(returnTypes))
			for i, rt := range returnTypes {
				defaults[i] = c.root.defaultConstValue(rt)
			}
			c.root.terminate(&hir.InstrReturn{Vals: defaults})
		}
	}
}

func (c *CallLowerer) LowerCFunc(cfn *ast.CFuncDecl) {
	targetCName := ""
	if cfn.TargetCName != nil {
		targetCName = cfn.TargetCName.Value
	} else {
		targetCName = "c_" + cfn.Name.Value
	}

	returnTypes := []sema.Type{}
	if fnType := c.root.semaCtx.Functions[cfn.Name.Value]; fnType != nil && len(fnType.ReturnTypes) > 0 {
		returnTypes = fnType.ReturnTypes
	} else {
		for _, rt := range cfn.ReturnTypes {
			returnTypes = append(returnTypes, c.root.semaCtx.ResolveType(rt))
		}
	}

	if cfn.Body == nil {
		params := []*hir.Reg{}
		for i, p := range cfn.Params {
			pType := c.root.semaCtx.ResolveType(p.Type)
			params = append(params, &hir.Reg{ID: i + 1, Typ: pType, Name: p.Name.Value})
		}

		hirFn := &hir.Function{
			Name:          targetCName,
			Params:        params,
			ReturnTypes:   returnTypes,
			Blocks:        nil,
			IsVariadic:    cfn.IsVariadic,
			IsExtern:      true,
			IsCFunc:       true,
			IsPassThrough: cfn.IsPassThrough,
			CFuncTarget:   targetCName,
		}
		c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)
		return
	}

	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	c.root.deferStack = []*ast.CallExpr{}
	c.root.regCount = 0
	c.root.escapedVars = sema.CollectAllCapturesInBlock(cfn.Body)

	hirFn := &hir.Function{
		Name:          targetCName,
		Params:        []*hir.Reg{},
		ReturnTypes:   returnTypes,
		Blocks:        []*hir.BasicBlock{},
		IsVariadic:    cfn.IsVariadic,
		IsExtern:      false,
		IsCFunc:       true,
		IsPassThrough: cfn.IsPassThrough,
		CFuncTarget:   targetCName,
	}
	c.root.curFunc = hirFn
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)

	entryBB := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(entryBB)

	for _, p := range cfn.Params {
		pType := c.root.semaCtx.ResolveType(p.Type)
		paramReg := c.root.nextReg(pType, p.Name.Value+"_arg")
		hirFn.Params = append(hirFn.Params, paramReg)

		ptrReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
		if p.IsEscaped || c.root.escapedVars[p.Name.Value] {
			sizeVal := &hir.ConstInt{Val: int64(pType.Size()), Typ: sema.TypeInt}
			c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: pType})
		} else {
			c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: pType})
		}
		c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
		c.root.symbols[p.Name.Value] = ptrReg
		c.root.symbolTypes[p.Name.Value] = pType
	}

	for _, stmt := range cfn.Body.Statements {
		c.root.Stmt.LowerStmt(stmt)
	}

	for i := len(c.root.deferStack) - 1; i >= 0; i-- {
		c.LowerCall(c.root.deferStack[i])
	}

	if c.root.curBlock.Terminator == nil {
		if len(returnTypes) == 0 {
			c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		} else {
			defaults := make([]hir.Value, len(returnTypes))
			for i, rt := range returnTypes {
				defaults[i] = c.root.defaultConstValue(rt)
			}
			c.root.terminate(&hir.InstrReturn{Vals: defaults})
		}
	}
}

// -------------------------------------------------------------
// クロージャ (FuncLit) の Lowering
// -------------------------------------------------------------

func (c *CallLowerer) LowerFuncLit(fl *ast.FuncLit) hir.Value {
	c.root.anonFuncCount++
	anonName := fmt.Sprintf("__anon_func_%d", c.root.anonFuncCount)

	ft := c.root.semaCtx.InferExprType(fl, nil).(*sema.FuncType)
	anonFn := &hir.Function{
		Name:        anonName,
		Params:      []*hir.Reg{},
		ReturnTypes: ft.ReturnTypes,
		Blocks:      []*hir.BasicBlock{},
		IsVariadic:  fl.IsVariadic,
		IsExtern:    false,
	}

	envParamReg := &hir.Reg{ID: 1, Typ: &sema.PointerType{Base: sema.TypeByte}, Name: "__env_arg"}
	anonFn.Params = append(anonFn.Params, envParamReg)

	rawCaptures := sema.ScanCapturesFromLit(fl)
	captures := []string{}
	for _, name := range rawCaptures {
		if _, ok := c.root.symbols[name]; ok {
			captures = append(captures, name)
		}
	}

	prevFunc := c.root.curFunc
	prevBlock := c.root.curBlock
	prevSymbols := c.root.symbols
	prevTypes := c.root.symbolTypes
	prevLoopStack := c.root.loopStack
	prevDeferStack := c.root.deferStack
	prevEscapedVars := c.root.escapedVars

	c.root.curFunc = anonFn
	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	c.root.loopStack = []loopContext{}
	c.root.deferStack = []*ast.CallExpr{}
	if fl.Body != nil {
		c.root.escapedVars = sema.CollectAllCapturesInBlock(fl.Body)
	} else {
		c.root.escapedVars = make(map[string]bool)
	}

	anonEntry := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(anonEntry)

	if len(captures) > 0 {
		envTyped := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		c.root.emit(&hir.InstrCast{Dst: envTyped, Val: envParamReg, ToType: &sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}})

		for idx, name := range captures {
			symType := prevTypes[name]
			slot := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
			c.root.emit(&hir.InstrGetElemPtr{Dst: slot, BasePtr: envTyped, Index: &hir.ConstInt{Val: int64(idx), Typ: sema.TypeInt}})
			rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			c.root.emit(&hir.InstrLoad{Dst: rawPtr, Ptr: slot})
			typedPtr := c.root.nextReg(&sema.PointerType{Base: symType})
			c.root.emit(&hir.InstrCast{Dst: typedPtr, Val: rawPtr, ToType: &sema.PointerType{Base: symType}})
			c.root.symbols[name] = typedPtr
			c.root.symbolTypes[name] = symType
		}
	}

	for i, p := range fl.Params {
		pType := c.root.semaCtx.ResolveType(p.Type)
		if p.IsVariadic || (fl.IsVariadic && i == len(fl.Params)-1) {
			if _, isSlice := pType.(*sema.SliceType); !isSlice {
				pType = &sema.SliceType{Elem: pType}
			}
		}
		pReg := c.root.nextReg(pType, p.Name.Value+"_arg")
		anonFn.Params = append(anonFn.Params, pReg)
		allocaReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
		if p.IsEscaped || c.root.escapedVars[p.Name.Value] {
			sizeVal := &hir.ConstInt{Val: int64(pType.Size()), Typ: sema.TypeInt}
			c.root.emit(&hir.InstrHeapAlloc{Dst: allocaReg, Size: sizeVal, AllocType: pType})
		} else {
			c.root.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: pType})
		}
		c.root.emit(&hir.InstrStore{Val: pReg, Ptr: allocaReg})
		c.root.symbols[p.Name.Value] = allocaReg
		c.root.symbolTypes[p.Name.Value] = pType
	}

	for _, s := range fl.Body.Statements {
		c.root.Stmt.LowerStmt(s)
	}

	for i := len(c.root.deferStack) - 1; i >= 0; i-- {
		c.LowerCall(c.root.deferStack[i])
	}

	if c.root.curBlock.Terminator == nil {
		if len(ft.ReturnTypes) == 0 {
			c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		} else {
			defaults := make([]hir.Value, len(ft.ReturnTypes))
			for i, rt := range ft.ReturnTypes {
				defaults[i] = c.root.defaultConstValue(rt)
			}
			c.root.terminate(&hir.InstrReturn{Vals: defaults})
		}
	}

	c.root.hirProg.Functions = append(c.root.hirProg.Functions, anonFn)

	c.root.curFunc = prevFunc
	c.root.curBlock = prevBlock
	c.root.symbols = prevSymbols
	c.root.symbolTypes = prevTypes
	c.root.loopStack = prevLoopStack
	c.root.deferStack = prevDeferStack
	c.root.escapedVars = prevEscapedVars

	var envVal hir.Value = &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}
	if len(captures) > 0 {
		envSize := len(captures) * 8
		envRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		c.root.emit(&hir.InstrHeapAlloc{Dst: envRaw, Size: &hir.ConstInt{Val: int64(envSize), Typ: sema.TypeInt}, AllocType: sema.TypeByte})

		envTyped := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		c.root.emit(&hir.InstrCast{Dst: envTyped, Val: envRaw, ToType: &sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}})

		for idx, name := range captures {
			symPtr := c.root.symbols[name]
			symRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			c.root.emit(&hir.InstrCast{Dst: symRaw, Val: symPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})
			slot := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
			c.root.emit(&hir.InstrGetElemPtr{Dst: slot, BasePtr: envTyped, Index: &hir.ConstInt{Val: int64(idx), Typ: sema.TypeInt}})
			c.root.emit(&hir.InstrStore{Val: symRaw, Ptr: slot})
		}
		envVal = envRaw
	}

	fatType := ft
	t1 := c.root.nextReg(fatType)
	fnGlobal := &hir.GlobalVar{Name: anonName, Typ: &sema.PointerType{Base: sema.TypeByte}}
	c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(fatType), Val: fnGlobal, Index: 0})
	t2 := c.root.nextReg(fatType)
	c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: envVal, Index: 1})
	return t2
}

// -------------------------------------------------------------
// スライス追加 (Append)
// -------------------------------------------------------------

func (c *CallLowerer) LowerAppend(call *ast.CallExpr) hir.Value {
	sliceVal := c.root.Expr.LowerExpr(call.Args[0])
	slType := sliceVal.Type().(*sema.SliceType)
	elemSize := slType.Elem.Size()
	if elemSize <= 0 {
		elemSize = 1
	}

	oldRawBytePtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	oldLen := c.root.nextReg(sema.TypeInt)
	oldCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrExtractValue{Dst: oldRawBytePtr, Agg: sliceVal, Index: 0})
	c.root.emit(&hir.InstrExtractValue{Dst: oldLen, Agg: sliceVal, Index: 1})
	c.root.emit(&hir.InstrExtractValue{Dst: oldCap, Agg: sliceVal, Index: 2})

	oldTypedPtr := c.root.nextReg(&sema.PointerType{Base: slType.Elem})
	c.root.emit(&hir.InstrCast{Dst: oldTypedPtr, Val: oldRawBytePtr, ToType: &sema.PointerType{Base: slType.Elem}})

	numElems := len(call.Args) - 1
	reqCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: reqCap, Op: hir.OpAdd, L: oldLen, R: &hir.ConstInt{Val: int64(numElems), Typ: sema.TypeInt}})

	growCond := c.root.nextReg(sema.TypeBool)
	c.root.emit(&hir.InstrBinary{Dst: growCond, Op: hir.OpGt, L: reqCap, R: oldCap})

	growBB := c.root.newBlock("append.grow")
	noGrowBB := c.root.newBlock("append.nogrow")
	storeBB := c.root.newBlock("append.store")

	finalPtrAlloca := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: slType.Elem}}, "finalPtr")
	finalCapAlloca := c.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "finalCap")
	c.root.emit(&hir.InstrAlloca{Dst: finalPtrAlloca, AllocType: &sema.PointerType{Base: slType.Elem}})
	c.root.emit(&hir.InstrAlloca{Dst: finalCapAlloca, AllocType: sema.TypeInt})

	c.root.terminate(&hir.InstrBranch{Cond: growCond, ThenTarget: growBB.Label, ElseTarget: noGrowBB.Label})

	c.root.setBlock(noGrowBB)
	c.root.emit(&hir.InstrStore{Val: oldTypedPtr, Ptr: finalPtrAlloca})
	c.root.emit(&hir.InstrStore{Val: oldCap, Ptr: finalCapAlloca})
	c.root.terminate(&hir.InstrJump{Target: storeBB.Label})

	c.root.setBlock(growBB)
	doubleCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: doubleCap, Op: hir.OpMul, L: oldCap, R: &hir.ConstInt{Val: 2, Typ: sema.TypeInt}})
	newCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: newCap, Op: hir.OpAdd, L: doubleCap, R: reqCap})
	newBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: newBytes, Op: hir.OpMul, L: newCap, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})

	newRawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{Dst: newRawPtr, Size: newBytes, AllocType: sema.TypeByte})

	newTypedPtr := c.root.nextReg(&sema.PointerType{Base: slType.Elem})
	c.root.emit(&hir.InstrCast{Dst: newTypedPtr, Val: newRawPtr, ToType: &sema.PointerType{Base: slType.Elem}})

	oldBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: oldBytes, Op: hir.OpMul, L: oldLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})

	memcpyTmp := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCallStatic{Dst: memcpyTmp, CalleeName: "memcpy", Args: []hir.Value{newRawPtr, oldRawBytePtr, oldBytes}})

	c.root.emit(&hir.InstrStore{Val: newTypedPtr, Ptr: finalPtrAlloca})
	c.root.emit(&hir.InstrStore{Val: newCap, Ptr: finalCapAlloca})
	c.root.terminate(&hir.InstrJump{Target: storeBB.Label})

	c.root.setBlock(storeBB)
	resPtr := c.root.nextReg(&sema.PointerType{Base: slType.Elem})
	resCap := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrLoad{Dst: resPtr, Ptr: finalPtrAlloca})
	c.root.emit(&hir.InstrLoad{Dst: resCap, Ptr: finalCapAlloca})

	for i := 0; i < numElems; i++ {
		elVal := c.root.Expr.LowerExpr(call.Args[1+i])
		elVal = c.root.emitValueCoerce(elVal, slType.Elem)
		offsetIdx := c.root.nextReg(sema.TypeInt)
		c.root.emit(&hir.InstrBinary{Dst: offsetIdx, Op: hir.OpAdd, L: oldLen, R: &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt}})
		destPtr := c.root.nextReg(&sema.PointerType{Base: slType.Elem})
		c.root.emit(&hir.InstrGetElemPtr{Dst: destPtr, BasePtr: resPtr, Index: offsetIdx})
		c.root.emit(&hir.InstrStore{Val: elVal, Ptr: destPtr})
	}

	resBytePtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCast{Dst: resBytePtr, Val: resPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})

	t1 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(slType), Val: resBytePtr, Index: 0})
	t2 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: reqCap, Index: 1})
	t3 := c.root.nextReg(slType)
	c.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: resCap, Index: 2})
	return t3
}

// -------------------------------------------------------------
// itab 生成
// -------------------------------------------------------------

func (c *CallLowerer) GetOrCreateItab(concreteType sema.Type, iface *sema.InterfaceType) *hir.ItabDef {
	sName := strings.TrimPrefix(concreteType.TypeName(), "*")
	ifName := iface.Name
	if ifName == "" {
		ifName = "anon_iface"
	}
	key := fmt.Sprintf("%s_%s", sName, ifName)
	if existing, ok := c.root.itabs[key]; ok {
		return existing
	}

	typeID := c.root.semaCtx.GetTypeID(concreteType)
	globalName := fmt.Sprintf("__itab_%s_%s", sName, ifName)
	itabStructName := fmt.Sprintf("__itab_%s", ifName)

	methods := []hir.ItabMethodEntry{}

	for _, m := range iface.Methods {
		targetFnName, _, _, found := c.ResolveMethod(concreteType, m.Name, nil)
		if !found {
			targetFnName = fmt.Sprintf("%s_%s", sName, m.Name)
		}
		methods = append(methods, hir.ItabMethodEntry{
			MethodName:   m.Name,
			TargetFnName: targetFnName,
			MethodType:   m,
		})
	}

	def := &hir.ItabDef{
		GlobalName:     globalName,
		ConcreteType:   concreteType,
		InterfaceType:  iface,
		TypeID:         typeID,
		ItabStructName: itabStructName,
		Methods:        methods,
	}
	c.root.itabs[key] = def
	c.root.hirProg.Itabs = append(c.root.hirProg.Itabs, def)
	return def
}
