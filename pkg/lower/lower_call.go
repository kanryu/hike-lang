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

// getFuncParams は対象関数の仮引数宣言リスト（ParamDecl）を取得する
func (c *CallLowerer) getFuncParams(fnType *sema.FuncType, canonicalName string) []*ast.ParamDecl {
	if fnType != nil {
		if fnType.Template != nil {
			return fnType.Template.Params
		}
		if fnType.SpecializedAst != nil {
			return fnType.SpecializedAst.Params
		}
		if fnType.CFuncAst != nil {
			return fnType.CFuncAst.Params
		}
	}
	if fnDecl := c.findFuncDecl(canonicalName); fnDecl != nil {
		return fnDecl.Params
	}
	if c.root.prog != nil {
		for _, d := range c.root.prog.Decls {
			switch decl := d.(type) {
			case *ast.ExternFuncDecl:
				if decl.Name.Value == canonicalName || (decl.TargetCName != nil && decl.TargetCName.Value == canonicalName) {
					return decl.Params
				}
			case *ast.CFuncDecl:
				if decl.Name.Value == canonicalName || (decl.TargetCName != nil && decl.TargetCName.Value == canonicalName) {
					return decl.Params
				}
			case *ast.JFuncDecl:
				if decl.Name.Value == canonicalName {
					return decl.Params
				}
			}
		}
	}
	return nil
}

// fillDefaultArgs は省略された末尾引数にデフォルト値式を補完する
func (c *CallLowerer) fillDefaultArgs(callArgs []ast.Expression, params []*ast.ParamDecl) []ast.Expression {
	if params == nil || len(callArgs) >= len(params) {
		return callArgs
	}
	filled := make([]ast.Expression, len(callArgs), len(params))
	for i := 0; i < len(callArgs); i++ {
		filled[i] = callArgs[i]
	}
	for i := len(callArgs); i < len(params); i++ {
		if params[i].Default != nil {
			filled = append(filled, params[i].Default)
		} else {
			break
		}
	}
	return filled
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

	fixedParams := c.bindInlineVariadicParams(fnDecl, call)

	var varArgs []ast.Expression
	if len(call.Args) > len(fixedParams) {
		varArgs = call.Args[len(fixedParams):]
	}
	c.currentVarArgs = varArgs

	retVal := c.lowerInlineVariadicBody(fnDecl)

	c.currentVarArgs = prevVarArgs
	c.root.symbols = prevSymbols
	c.root.symbolTypes = prevTypes

	return retVal
}

func (c *CallLowerer) lowerInlineVariadicBody(fnDecl *ast.FuncDecl) hir.Value {
	var retVal hir.Value
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
	return retVal
}

func (c *CallLowerer) bindInlineVariadicParams(fnDecl *ast.FuncDecl, call *ast.CallExpr) []*ast.ParamDecl {
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
		val := c.root.defaultConstValue(pType)
		if i < len(call.Args) {
			val = c.root.Expr.LowerExpr(call.Args[i])
			val = c.root.emitValueCoerce(val, pType)
		} else if p.Default != nil {
			val = c.root.Expr.LowerExpr(p.Default)
			val = c.root.emitValueCoerce(val, pType)
		}
		ptrReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
		c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: pType})
		c.root.emit(&hir.InstrStore{Val: val, Ptr: ptrReg})
		c.root.symbols[p.Name.Value] = ptrReg
		c.root.symbolTypes[p.Name.Value] = pType
	}
	return fixedParams
}

// -----------------------------------------------------------------------------
// 文字列相互変換ヘルパー (string <-> cstring)
// -----------------------------------------------------------------------------

func (c *CallLowerer) lowerStringToCString(strVal hir.Value) hir.Value {
	strPtr, lenReg := c.root.stringParts(strVal)
	oneVal := &hir.ConstInt{Val: 1, Typ: sema.TypeInt}
	sizeReg := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: sizeReg, Op: hir.OpAdd, L: lenReg, R: oneVal})

	bufReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte, KeepOnHeapInArea: true})

	c.root.emit(&hir.InstrCallStatic{
		CalleeName: c.root.BuiltinName("memcpy"),
		Args:       []hir.Value{bufReg, strPtr, lenReg},
	})

	endPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrGetElemPtr{Dst: endPtr, BasePtr: bufReg, Index: lenReg})
	c.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeByte}, Ptr: endPtr})

	// Keep the semantic cstring type even though its wasm ABI is the same as
	// *byte.  Losing that type makes a later slice take the byte-slice path
	// instead of the cstring/string-view path.
	result := c.root.nextReg(sema.TypeCString)
	c.root.emit(&hir.InstrCast{Dst: result, Val: bufReg, ToType: sema.TypeCString})
	return result
}

func (c *CallLowerer) lowerCStringToString(cstrVal hir.Value) hir.Value {
	lenReg := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrCallStatic{
		Dst:        lenReg,
		CalleeName: c.root.BuiltinName("strlen"),
		Args:       []hir.Value{cstrVal},
	})
	oneVal := &hir.ConstInt{Val: 1, Typ: sema.TypeInt}
	sizeReg := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: sizeReg, Op: hir.OpAdd, L: lenReg, R: oneVal})

	bufReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte, KeepOnHeapInArea: true})

	c.root.emit(&hir.InstrCallStatic{
		CalleeName: c.root.BuiltinName("memcpy"),
		Args:       []hir.Value{bufReg, cstrVal, sizeReg},
	})

	return c.root.makeString(bufReg, lenReg)
}

// -----------------------------------------------------------------------------
// 型解決・型キャスト判定
// -----------------------------------------------------------------------------

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
	case *ast.IndexExpr:
		var pkgId *ast.Identifier
		var typeId *ast.Identifier
		if id, okId := node.Left.(*ast.Identifier); okId {
			typeId = id
		} else if mem, okMem := node.Left.(*ast.MemberExpr); okMem {
			if p, okP := mem.Object.(*ast.Identifier); okP {
				pkgId = p
				typeId = mem.Field
			}
		}
		if typeId != nil {
			var typeArgs []ast.TypeExpr
			if te, okTe := node.Index.(ast.TypeExpr); okTe {
				typeArgs = append(typeArgs, te)
			} else if id, okId := node.Index.(*ast.Identifier); okId {
				typeArgs = append(typeArgs, &ast.NamedType{Token: id.Token, Name: id})
			} else if mem, okMem := node.Index.(*ast.MemberExpr); okMem {
				if p, okP := mem.Object.(*ast.Identifier); okP {
					typeArgs = append(typeArgs, &ast.NamedType{Token: mem.Token, Package: p, Name: mem.Field})
				}
			}
			return c.root.semaCtx.ResolveType(&ast.NamedType{
				Token:    node.Token,
				Package:  pkgId,
				Name:     typeId,
				TypeArgs: typeArgs,
			})
		}
		return nil

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
			return nil
		}
		return nil
	}
	return nil
}

// semaTypeToTypeExpr converts a resolved type back into the AST form used by
// address and slice lowering. Generic specialization itself belongs to the
// transform phase; this helper only preserves the common type representation.
func semaTypeToTypeExpr(t sema.Type) ast.TypeExpr {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case *sema.ConstValueType:
		return &ast.ConstArg{Expr: &ast.IntegerLiteral{Value: v.Value}}
	case *sema.PointerType:
		return &ast.PointerType{Base: semaTypeToTypeExpr(v.Base)}
	case *sema.SliceType:
		return &ast.SliceType{Elem: semaTypeToTypeExpr(v.Elem)}
	case *sema.ArrayType:
		return &ast.ArrayType{Len: int64(v.Len), Elem: semaTypeToTypeExpr(v.Elem)}
	case *sema.MapType:
		return &ast.MapType{Key: semaTypeToTypeExpr(v.Key), Value: semaTypeToTypeExpr(v.Value)}
	default:
		return &ast.NamedType{Name: &ast.Identifier{Value: semaTypeName(v)}}
	}
}

// -------------------------------------------------------------
// メソッドパス解決
// -------------------------------------------------------------

func (c *CallLowerer) ResolveMethod(recvType sema.Type, methodName string, curPtr hir.Value) (string, *sema.FuncType, hir.Value, bool) {
	if recvType == nil {
		return "", nil, nil, false
	}

	rawTypeName := strings.TrimPrefix(semaTypeName(recvType), "*")
	shortTypeName := rawTypeName
	if idx := strings.LastIndex(rawTypeName, "_"); idx != -1 {
		shortTypeName = rawTypeName[idx+1:]
	}

	if fn, canonical := c.root.semaCtx.LookupMethod(semaTypeName(recvType), methodName); fn != nil {
		targetName := canonical
		if semaFuncIRName(fn) != "" {
			targetName = semaFuncIRName(fn)
		}
		return targetName, fn, curPtr, true
	}
	if !strings.HasPrefix(semaTypeName(recvType), "*") {
		if fn, canonical := c.root.semaCtx.LookupMethod("*"+semaTypeName(recvType), methodName); fn != nil {
			targetName := canonical
			if semaFuncIRName(fn) != "" {
				targetName = semaFuncIRName(fn)
			}
			return targetName, fn, curPtr, true
		}
	}

	canonicalTarget := sema.CanonicalMethodName(rawTypeName, methodName)
	if fn, canonical := c.root.semaCtx.LookupFunction(canonicalTarget); fn != nil {
		return canonical, fn, curPtr, true
	}

	candidates := []string{
		canonicalTarget,
		rawTypeName + "_" + methodName,
		shortTypeName + "_" + methodName,
		c.root.moduleName() + "_" + rawTypeName + "_" + methodName,
		c.root.moduleName() + "_" + shortTypeName + "_" + methodName,
	}
	for aliasName, aliasType := range c.root.semaCtx.Aliases {
		if semaTypeName(aliasType) == rawTypeName || semaTypeName(aliasType) == shortTypeName {
			candidates = append(candidates,
				sema.CanonicalMethodName(aliasName, methodName),
				aliasName+"_"+methodName,
				c.root.moduleName()+"_"+aliasName+"_"+methodName,
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
				if semaTypeName(aliasType) == rawTypeName || semaTypeName(aliasType) == shortTypeName {
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

// -------------------------------------------------------------
// 引数パッキング & ロワリングヘルパー
// -------------------------------------------------------------

func (c *CallLowerer) lowerVariadicSlice(args []ast.Expression, elemType sema.Type) hir.Value {
	if elemType == nil {
		elemType = &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
	}
	slType := &sema.SliceType{Elem: elemType}

	count := len(args)
	if count == 0 {
		return c.root.defaultConstValue(slType)
	}

	elemSize := sema.SizeOf(elemType)
	if elemSize <= 0 {
		elemSize = sema.PointerSize
	}
	totalBytes := count * elemSize

	mallocRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{
		Dst:              mallocRaw,
		Size:             &hir.ConstInt{Val: int64(totalBytes), Typ: sema.TypeInt},
		AllocType:        sema.TypeByte,
		KeepOnHeapInArea: true,
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
	if isCFunc && isVariadic {
		return c.lowerCVariadicArgs(callArgs, paramTypes)
	}

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
				if id, ok := lastArg.(*ast.Identifier); ok && astIDValue(id) == "..." {
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

	args := make([]hir.Value, len(callArgs))
	for i, arg := range callArgs {
		val := c.root.Expr.LowerExpr(arg)
		if isCFunc && (val.Type() == sema.TypeString || semaTypeName(val.Type()) == "string") {
			val = c.lowerStringToCString(val)
		}
		if i < len(paramTypes) {
			if expectedPtr, okExpected := paramTypes[i].(*sema.PointerType); okExpected {
				if actualPtr, okActual := val.Type().(*sema.PointerType); okActual {
					if nestedPtr, okNested := actualPtr.Base.(*sema.PointerType); okNested && semaTypeName(nestedPtr.Base) == semaTypeName(expectedPtr.Base) {
						loaded := c.root.nextReg(actualPtr.Base)
						c.root.emit(&hir.InstrLoad{Dst: loaded, Ptr: val})
						val = loaded
					}
				}
			}
			if !(isCFunc && (paramTypes[i] == sema.TypeString || semaTypeName(paramTypes[i]) == "string")) {
				val = c.root.emitValueCoerce(val, paramTypes[i])
			}
		}
		args[i] = val
	}
	return args
}

func (c *CallLowerer) lowerCVariadicArgs(callArgs []ast.Expression, paramTypes []sema.Type) []hir.Value {
	args := make([]hir.Value, 0, len(callArgs))
	for i, arg := range callArgs {
		if id, ok := arg.(*ast.Identifier); ok && astIDValue(id) == "..." {
			for _, vArg := range c.currentVarArgs {
				vVal := c.root.Expr.LowerExpr(vArg)
				if vVal.Type() == sema.TypeString || semaTypeName(vVal.Type()) == "string" {
					vVal = c.lowerStringToCString(vVal)
				}
				vVal = c.promoteCVarArg(vVal)
				args = append(args, vVal)
			}
			continue
		}
		val := c.root.Expr.LowerExpr(arg)
		if val.Type() == sema.TypeString || semaTypeName(val.Type()) == "string" {
			val = c.lowerStringToCString(val)
		}
		if i >= len(paramTypes) {
			val = c.promoteCVarArg(val)
		} else if !(paramTypes[i] == sema.TypeString || semaTypeName(paramTypes[i]) == "string") {
			val = c.root.emitValueCoerce(val, paramTypes[i])
		}
		args = append(args, val)
	}
	return args
}

func (c *CallLowerer) promoteCVarArg(val hir.Value) hir.Value {
	// C varargs are passed in machine-word slots. This is deliberately
	// separate from ordinary Hike variadic calls: the latter pass a typed
	// slice, while a C function receives only ABI-promoted scalar values.
	wordType := sema.TypeInt
	if sema.LLVMTypeOf(sema.TypeInt) == sema.LLVMTypeOf(sema.TypeInt32) {
		wordType = sema.TypeInt32
	}
	var target sema.Type
	switch {
	case val.Type() == sema.TypeBool || sema.LLVMTypeOf(val.Type()) == "i1":
		target = wordType
	case val.Type() == sema.TypeByte || val.Type() == sema.TypeInt8 || val.Type() == sema.TypeInt16 || sema.LLVMTypeOf(val.Type()) == "i8" || sema.LLVMTypeOf(val.Type()) == "i16":
		target = wordType
	case val.Type() == sema.TypeUint8 || val.Type() == sema.TypeUint16 || val.Type() == sema.TypeUint32:
		// C's integer promotions use the signed int width when the value
		// fits in int.  Hike's fixed-width integer values are represented
		// in the same machine-word slot for this C ABI path.
		target = wordType
	case val.Type() == sema.TypeInt32 && wordType == sema.TypeInt:
		target = wordType
	case val.Type() == sema.TypeFloat32 || sema.LLVMTypeOf(val.Type()) == "float":
		target = sema.TypeFloat64
	default:
		return val
	}
	if sema.LLVMTypeOf(val.Type()) == sema.LLVMTypeOf(target) {
		return val
	}
	extReg := c.root.nextReg(target)
	c.root.emit(&hir.InstrCast{Dst: extReg, Val: val, ToType: target})
	return extReg
}

// -------------------------------------------------------------
// ジェネリクス関数のオンデマンド特殊化 (Monomorphization)
// -------------------------------------------------------------

// -------------------------------------------------------------
// 関数・メソッド呼び出し (Call)
// -------------------------------------------------------------

func (c *CallLowerer) lowerPackageMemberCall(call *ast.CallExpr, mem *ast.MemberExpr, pkgIdent *ast.Identifier) hir.Value {
	methodName := mem.Field.Value
	if c.root.semaCtx.GoHikeMode && pkgIdent.Value == "sort" && methodName == "Strings" {
		if len(call.Args) != 1 {
			panic(fmt.Sprintf("[Lower Error] sort.Strings expects one argument, got %d", len(call.Args)))
		}
		arg := c.root.Expr.LowerExpr(call.Args[0])
		c.root.emit(&hir.InstrCallStatic{CalleeName: "__hike_sort_strings", Args: []hir.Value{arg}})
		return nil
	}
	targetFnName := pkgIdent.Value + "_" + methodName
	targetFn, canonicalName := c.root.semaCtx.LookupFunction(targetFnName)
	if targetFn == nil {
		targetFn, canonicalName = c.root.semaCtx.LookupFunction(methodName)
	}
	if targetFn == nil {
		panic(fmt.Sprintf("[Lower Error] undefined function: %s.%s", pkgIdent.Value, methodName))
	}

	params := c.getFuncParams(targetFn, canonicalName)
	callArgs := c.fillDefaultArgs(call.Args, params)
	if semaFuncIsVariadic(targetFn) && semaFuncVariadicElem(targetFn) == nil {
		if fnDecl := c.findFuncDecl(canonicalName); fnDecl != nil && fnDecl.Body != nil {
			return c.inlineVariadicCall(fnDecl, &ast.CallExpr{
				Token:       call.Token,
				Function:    call.Function,
				Args:        callArgs,
				HasEllipsis: call.HasEllipsis,
			})
		}
	}

	isCVarArg := semaFuncCFunc(targetFn) || semaFuncExtern(targetFn) || (semaFuncIsVariadic(targetFn) && semaFuncVariadicElem(targetFn) == nil)
	args := c.lowerArgs(callArgs, targetFn.ParamTypes, semaFuncIsVariadic(targetFn), isCVarArg, semaFuncVariadicElem(targetFn), call.HasEllipsis)
	var retType sema.Type = sema.TypeVoid
	if len(targetFn.ReturnTypes) == 1 {
		retType = targetFn.ReturnTypes[0]
	} else if len(targetFn.ReturnTypes) > 1 {
		retType = &sema.TupleType{Types: targetFn.ReturnTypes}
	}
	var dst *hir.Reg
	if retType != sema.TypeVoid {
		dst = c.root.nextReg(retType)
	}

	callee := canonicalName
	if semaFuncCFunc(targetFn) {
		if semaFuncCFuncAst(targetFn) != nil && !semaFuncCFuncAst(targetFn).IsAlias() {
			callee = "__hike_impl_" + semaFuncName(targetFn)
		} else if semaFuncCFuncTarget(targetFn) != "" {
			callee = semaFuncCFuncTarget(targetFn)
		} else {
			callee = "c_" + canonicalName
		}
	} else if semaFuncExtern(targetFn) && semaFuncIRName(targetFn) != "" {
		callee = semaFuncIRName(targetFn)
	}
	c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: callee, Args: args})
	return dst
}

func (c *CallLowerer) lowerCStringBuiltin(call *ast.CallExpr) (hir.Value, bool) {
	if len(call.Args) == 0 {
		return nil, false
	}
	// cstring(ptr, len) uses the same NUL-terminated runtime
	// representation, but preserves the cstring static type.
	if len(call.Args) == 2 {
		ptrVal := c.root.Expr.LowerExpr(call.Args[0])
		lenVal := c.root.Expr.LowerExpr(call.Args[1])
		dst := c.root.nextReg(sema.TypeCString)
		c.root.emit(&hir.InstrCallStatic{
			Dst:        dst,
			CalleeName: c.root.BuiltinName("__hike_slice_to_str"),
			Args:       []hir.Value{ptrVal, lenVal},
		})
		return dst, true
	}
	argVal := c.root.Expr.LowerExpr(call.Args[0])
	if argVal.Type() == sema.TypeCString {
		return argVal, true
	}
	if argVal.Type() == sema.TypeString || semaTypeName(argVal.Type()) == "string" {
		return c.lowerStringToCString(argVal), true
	}
	if strings.HasPrefix(sema.LLVMTypeOf(argVal.Type()), "{") {
		return c.lowerStringToCString(argVal), true
	}
	dst := c.root.nextReg(sema.TypeCString)
	c.root.emit(&hir.InstrCast{Dst: dst, Val: argVal, ToType: sema.TypeCString})
	return dst, true
}

func (c *CallLowerer) lowerStringBuiltin(call *ast.CallExpr) (hir.Value, bool) {
	if len(call.Args) == 0 {
		return nil, false
	}
	// Construct a string directly from a byte pointer and explicit length.
	// This avoids the temporary []byte and its extra copy.
	if len(call.Args) == 2 {
		ptrVal := c.root.Expr.LowerExpr(call.Args[0])
		lenVal := c.root.Expr.LowerExpr(call.Args[1])
		raw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		c.root.emit(&hir.InstrCallStatic{
			Dst:        raw,
			CalleeName: c.root.BuiltinName("__hike_slice_to_str"),
			Args:       []hir.Value{ptrVal, lenVal},
		})
		return c.root.makeString(raw, lenVal), true
	}
	argVal := c.root.Expr.LowerExpr(call.Args[0])
	if _, isSlice := argVal.Type().(*sema.SliceType); isSlice {
		rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		rawLen := c.root.nextReg(sema.TypeInt)
		c.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: argVal, Index: 0})
		c.root.emit(&hir.InstrExtractValue{Dst: rawLen, Agg: argVal, Index: 1})
		raw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		c.root.emit(&hir.InstrCallStatic{
			Dst:        raw,
			CalleeName: c.root.BuiltinName("__hike_slice_to_str"),
			Args:       []hir.Value{rawPtr, rawLen},
		})
		return c.root.makeString(raw, rawLen), true
	}
	if argVal.Type() == sema.TypeCString || semaTypeName(argVal.Type()) == "cstring" {
		return c.lowerCStringToString(argVal), true
	}
	if ptrType, ok := argVal.Type().(*sema.PointerType); ok && ptrType.Base == sema.TypeByte {
		return c.lowerCStringToString(argVal), true
	}
	if sema.LLVMTypeOf(argVal.Type()) == "i8*" {
		return c.lowerCStringToString(argVal), true
	}
	return argVal, true
}

// lowerDeepCopyBuiltin copies a string's bytes into the ordinary heap. This
// is deliberately different from string assignment, which preserves the
// backing storage and is therefore unsafe for values created in an area.
func (c *CallLowerer) lowerDeepCopyBuiltin(call *ast.CallExpr) (hir.Value, bool) {
	if len(call.Args) != 1 {
		return nil, false
	}
	arg := c.root.Expr.LowerExpr(call.Args[0])
	if arg == nil || sema.DeepCopyError(arg.Type()) != "" {
		return nil, false
	}
	if sl, ok := arg.Type().(*sema.SliceType); ok {
		rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte}, "deepcopy")
		length := c.root.nextReg(sema.TypeInt, "deepcopy_len")
		c.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: arg, Index: 0})
		c.root.emit(&hir.InstrExtractValue{Dst: length, Agg: arg, Index: 1})
		elemSize := sema.SizeOf(sl.Elem)
		if elemSize <= 0 {
			elemSize = 1
		}
		bytes := c.root.nextReg(sema.TypeInt, "deepcopy_bytes")
		c.root.emit(&hir.InstrBinary{Dst: bytes, Op: hir.OpMul, L: length, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})
		copyBuf := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte}, "deepcopy")
		c.root.emit(&hir.InstrHeapAlloc{Dst: copyBuf, Size: bytes, AllocType: sema.TypeByte, KeepOnHeap: true})
		c.root.emit(&hir.InstrCallStatic{CalleeName: c.root.BuiltinName("memcpy"), Args: []hir.Value{copyBuf, rawPtr, bytes}})
		result := c.root.nextReg(sl)
		v1 := c.root.nextReg(sl)
		c.root.emit(&hir.InstrInsertValue{Dst: v1, Agg: c.root.defaultConstValue(sl), Val: copyBuf, Index: 0})
		v2 := c.root.nextReg(sl)
		c.root.emit(&hir.InstrInsertValue{Dst: v2, Agg: v1, Val: length, Index: 1})
		c.root.emit(&hir.InstrInsertValue{Dst: result, Agg: v2, Val: length, Index: 2})
		return result, true
	}
	return c.lowerDeepCopyValue(arg, arg.Type(), make(map[sema.Type]bool)), true
}

// lowerDeepCopyValue emits a type-directed copy. Pointer fields are followed
// and copied into ordinary heap storage instead of preserving their address.
func (c *CallLowerer) lowerDeepCopyValue(value hir.Value, typ sema.Type, visiting map[sema.Type]bool) hir.Value {
	if c.root.isStringType(typ) {
		data, length := c.root.stringParts(value)
		return c.lowerDeepCopyBytes(data, length)
	}
	switch t := typ.(type) {
	case *sema.PointerType:
		if visiting[t.Base] {
			return value
		}
		loaded := c.root.nextReg(t.Base, "deepcopy_value")
		c.root.emit(&hir.InstrLoad{Dst: loaded, Ptr: value})
		copied := c.lowerDeepCopyValue(loaded, t.Base, visiting)
		buf := c.root.nextReg(value.Type(), "deepcopy_ptr")
		c.root.emit(&hir.InstrHeapAlloc{Dst: buf, Size: &hir.ConstInt{Val: int64(sema.SizeOf(t.Base)), Typ: sema.TypeInt}, AllocType: t.Base, KeepOnHeap: true})
		c.root.emit(&hir.InstrStore{Val: copied, Ptr: buf})
		return buf
	case *sema.StructType:
		if visiting[t] {
			return value
		}
		visiting[t] = true
		result := c.root.defaultConstValue(t)
		for i, field := range t.Fields {
			fieldVal := c.root.nextReg(field.Type, "deepcopy_field")
			c.root.emit(&hir.InstrExtractValue{Dst: fieldVal, Agg: value, Index: i})
			copied := c.lowerDeepCopyValue(fieldVal, field.Type, visiting)
			inserted := c.root.nextReg(t, "deepcopy_struct")
			c.root.emit(&hir.InstrInsertValue{Dst: inserted, Agg: result, Val: copied, Index: i})
			result = inserted
		}
		delete(visiting, t)
		return result
	case *sema.ArrayType:
		result := c.root.defaultConstValue(t)
		for i := 0; i < t.Len; i++ {
			item := c.root.nextReg(t.Elem, "deepcopy_array")
			c.root.emit(&hir.InstrExtractValue{Dst: item, Agg: value, Index: i})
			copied := c.lowerDeepCopyValue(item, t.Elem, visiting)
			inserted := c.root.nextReg(t, "deepcopy_array")
			c.root.emit(&hir.InstrInsertValue{Dst: inserted, Agg: result, Val: copied, Index: i})
			result = inserted
		}
		return result
	default:
		return value
	}
}

func (c *CallLowerer) lowerDeepCopyBytes(data, length hir.Value) hir.Value {
	copyBuf := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte}, "deepcopy")
	c.root.emit(&hir.InstrHeapAlloc{Dst: copyBuf, Size: length, AllocType: sema.TypeByte, KeepOnHeap: true})
	c.root.emit(&hir.InstrCallStatic{
		CalleeName: c.root.BuiltinName("memcpy"),
		Args:       []hir.Value{copyBuf, data, length},
	})
	return c.root.makeString(copyBuf, length)
}

func (c *CallLowerer) lowerSizeofCall(call *ast.CallExpr) hir.Value {
	logger.LogVerbose2("[Verbose2] CallLowerer LowerCall CallExpr: ast=%T (%+v)\n", call, call)
	if len(call.Args) == 1 {
		arg := call.Args[0]
		t := c.ResolveTypeFromExpr(arg)
		if t == nil || t == sema.TypeVoid {
			argVal := c.root.Expr.LowerExpr(arg)
			t = argVal.Type()
		}
		if t != nil && t != sema.TypeVoid {
			if pt, isPtr := t.(*sema.PointerType); isPtr {
				t = pt.Base
			}
			sz := int64(sema.SizeOf(t))
			if st, _ := c.root.findStruct(t); st != nil {
				stSz := int64(st.Size())
				if stSz > sz {
					sz = stSz
				}
				if sz <= int64(sema.PointerSize) && strings.Contains(st.Name, "__") {
					baseName := strings.Split(strings.TrimPrefix(st.Name, "*"), "__")[0]
					if baseSt, _ := c.root.findStructByName(baseName); baseSt != nil && len(baseSt.Fields) > 0 {
						baseSz := int64(baseSt.Size())
						if baseSz > sz {
							sz = baseSz
						}
					}
				}
			}
			return &hir.ConstInt{Val: sz, Typ: sema.TypeInt}
		}
	}
	return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
}

func (c *CallLowerer) lowerMakeCall(call *ast.CallExpr) hir.Value {
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
	var resSliceType *sema.SliceType
	if slNode, okSlice := call.Args[0].(*ast.SliceType); okSlice {
		resSliceType = &sema.SliceType{Elem: c.root.semaCtx.ResolveType(slNode.Elem)}
	} else if id, okNamed := call.Args[0].(*ast.Identifier); okNamed {
		// Go permits make to receive a named slice type, for example
		// make(charAndCountArray, 64). Resolve the alias before lowering so
		// the result retains the slice representation.
		resolved := c.root.semaCtx.ResolveType(&ast.NamedType{Token: id.Token, Name: id})
		if slice, ok := resolved.(*sema.SliceType); ok {
			resSliceType = slice
		}
	}
	if resSliceType != nil {
		elemType := resSliceType.Elem
		lenVal := c.root.Expr.LowerExpr(call.Args[1])
		capVal := lenVal
		if len(call.Args) >= 3 {
			capVal = c.root.Expr.LowerExpr(call.Args[2])
		}
		elemSize := sema.SizeOf(elemType)
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
	return nil
}

func (c *CallLowerer) lowerInterfaceMethodCall(call *ast.CallExpr, mem *ast.MemberExpr, objPtr hir.Value, iface *sema.InterfaceType) hir.Value {
	methodIdx := 0
	var targetMethod sema.Method
	foundMethod := false
	for idx, m := range iface.Methods {
		if m.Name == mem.Field.Value {
			methodIdx = idx
			targetMethod = m
			foundMethod = true
			break
		}
	}
	if !foundMethod {
		panic(fmt.Sprintf("[Lower Error] method '%s' not found on interface", mem.Field.Value))
	}

	callArgs := call.Args
	if len(callArgs) < len(targetMethod.ParamTypes) {
		if fnDecl := c.findFuncDecl(mem.Field.Value); fnDecl != nil {
			callArgs = c.fillDefaultArgs(callArgs, fnDecl.Params)
		}
		for len(callArgs) < len(targetMethod.ParamTypes) {
			if targetMethod.ParamTypes[len(callArgs)] != sema.TypeInt {
				break
			}
			callArgs = append(callArgs, &ast.IntegerLiteral{
				Token: token.Token{Type: token.INT, Literal: "-1"},
				Value: -1,
			})
		}
	}

	args := c.lowerArgs(callArgs, targetMethod.ParamTypes, targetMethod.IsVariadic, false, targetMethod.VariadicElem, call.HasEllipsis)
	var retType sema.Type = sema.TypeVoid
	if len(targetMethod.ReturnTypes) == 1 {
		retType = targetMethod.ReturnTypes[0]
	} else if len(targetMethod.ReturnTypes) > 1 {
		retType = &sema.TupleType{Types: targetMethod.ReturnTypes}
	}
	var dst *hir.Reg
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
