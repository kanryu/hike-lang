package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

// CallLowerer は関数呼び出し、メソッド解決、型キャスト判定、引数パッキングを担当する
type CallLowerer struct {
	root           *Lowerer
	currentVarArgs []ast.Expression // インライン展開中の可変長実引数リスト
}

func NewCallLowerer(root *Lowerer) *CallLowerer {
	return &CallLowerer{root: root}
}

// findFuncDecl は AST プログラム宣言から指定名に一致する FuncDecl を探索する
func (c *CallLowerer) findFuncDecl(canonicalName string) *ast.FuncDecl {
	if c.root.prog == nil {
		return nil
	}
	for _, d := range c.root.prog.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			if fn.Name.Value == canonicalName {
				return fn
			}
			if strings.HasSuffix(canonicalName, "_"+fn.Name.Value) {
				return fn
			}
			if strings.HasSuffix(fn.Name.Value, "_"+canonicalName) {
				return fn
			}
		}
	}
	return nil
}

// inlineVariadicCall は C スタイル可変長引数を受け取り本体を持つ Hike 関数を呼び出し箇所でインライン展開する
func (c *CallLowerer) inlineVariadicCall(fnDecl *ast.FuncDecl, call *ast.CallExpr) hir.Value {
	prevSymbols := c.root.symbols
	prevTypes := c.root.symbolTypes
	prevVarArgs := c.currentVarArgs

	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	for k, v := range prevSymbols {
		c.root.symbols[k] = v
	}
	for k, v := range prevTypes {
		c.root.symbolTypes[k] = v
	}

	var fixedParams []*ast.ParamDecl
	for _, p := range fnDecl.Params {
		if !p.IsVariadic {
			fixedParams = append(fixedParams, p)
		}
	}

	for i, p := range fixedParams {
		var pType sema.Type = sema.TypeString
		if p.Type != nil {
			pType = c.root.semaCtx.ResolveType(p.Type)
		}
		var val hir.Value = c.root.defaultConstValue(pType)
		if i < len(call.Args) {
			val = c.root.Expr.LowerExpr(call.Args[i])
			val = c.root.emitValueCoerce(val, pType)
		}
		ptrReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
		c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: pType})
		c.root.emit(&hir.InstrStore{Val: val, Ptr: ptrReg})
		c.root.symbols[p.Name.Value] = ptrReg
		c.root.symbolTypes[p.Name.Value] = pType
	}

	var varArgs []ast.Expression
	if len(call.Args) > len(fixedParams) {
		varArgs = call.Args[len(fixedParams):]
	}
	c.currentVarArgs = varArgs

	var retVal hir.Value = nil
	if fnDecl.Body != nil {
		for _, stmt := range fnDecl.Body.Statements {
			if retStmt, isRet := stmt.(*ast.ReturnStmt); isRet {
				if len(retStmt.Values) > 0 {
					retVal = c.root.Expr.LowerExpr(retStmt.Values[0])
				}
				break
			}
			c.root.Stmt.LowerStmt(stmt)
		}
	}

	if retVal == nil && len(fnDecl.ReturnTypes) > 0 {
		retType := c.root.semaCtx.ResolveType(fnDecl.ReturnTypes[0])
		retVal = c.root.defaultConstValue(retType)
	}

	c.currentVarArgs = prevVarArgs
	c.root.symbols = prevSymbols
	c.root.symbolTypes = prevTypes

	return retVal
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

func (c *CallLowerer) lowerArgs(callArgs []ast.Expression, paramTypes []sema.Type, isVariadic bool, isCFunc bool, variadicElem sema.Type, hasEllipsis bool) []hir.Value {
	// C 言語スタイルの可変長引数 (C ABI: スタック展開 & 型昇格)
	if isCFunc && isVariadic {
		args := make([]hir.Value, 0, len(callArgs))
		for i, arg := range callArgs {
			if id, ok := arg.(*ast.Identifier); ok && id.Value == "..." {
				for _, vArg := range c.currentVarArgs {
					vVal := c.root.Expr.LowerExpr(vArg)
					if vVal.Type() == sema.TypeBool || vVal.Type().LLVMType() == "i1" {
						extReg := c.root.nextReg(sema.TypeInt)
						c.root.emit(&hir.InstrCast{Dst: extReg, Val: vVal, ToType: sema.TypeInt})
						vVal = extReg
					} else if vVal.Type() == sema.TypeByte || vVal.Type().LLVMType() == "i8" {
						extReg := c.root.nextReg(sema.TypeInt)
						c.root.emit(&hir.InstrCast{Dst: extReg, Val: vVal, ToType: sema.TypeInt})
						vVal = extReg
					} else if vVal.Type() == sema.TypeFloat32 || vVal.Type().LLVMType() == "float" {
						extReg := c.root.nextReg(sema.TypeFloat64)
						c.root.emit(&hir.InstrCast{Dst: extReg, Val: vVal, ToType: sema.TypeFloat64})
						vVal = extReg
					}
					args = append(args, vVal)
				}
				continue
			}
			val := c.root.Expr.LowerExpr(arg)
			if i >= len(paramTypes) {
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
					rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
					rawLen := c.root.nextReg(sema.TypeInt)
					c.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: argVal, Index: 0})
					c.root.emit(&hir.InstrExtractValue{Dst: rawLen, Agg: argVal, Index: 1})
					dst := c.root.nextReg(sema.TypeString)
					c.root.emit(&hir.InstrCallStatic{
						Dst:        dst,
						CalleeName: "__hike_slice_to_str",
						Args:       []hir.Value{rawPtr, rawLen},
					})
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
				rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
				rawLen := c.root.nextReg(sema.TypeInt)
				c.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: argVal, Index: 0})
				c.root.emit(&hir.InstrExtractValue{Dst: rawLen, Agg: argVal, Index: 1})
				dst := c.root.nextReg(sema.TypeString)
				c.root.emit(&hir.InstrCallStatic{
					Dst:        dst,
					CalleeName: "__hike_slice_to_str",
					Args:       []hir.Value{rawPtr, rawLen},
				})
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

			if targetFn.IsVariadic && targetFn.VariadicElem == nil {
				if fnDecl := c.findFuncDecl(canonicalName); fnDecl != nil && fnDecl.Body != nil {
					return c.inlineVariadicCall(fnDecl, call)
				}
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
				if targetFn.IsVariadic && targetFn.VariadicElem == nil {
					if fnDecl := c.findFuncDecl(canonicalName); fnDecl != nil && fnDecl.Body != nil {
						return c.inlineVariadicCall(fnDecl, call)
					}
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
