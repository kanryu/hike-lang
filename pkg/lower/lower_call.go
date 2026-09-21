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
	c.root.emit(&hir.InstrHeapAlloc{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte})

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
	c.root.emit(&hir.InstrHeapAlloc{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte})

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
	case *ast.GenericInstExpr:
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
			return c.root.semaCtx.ResolveType(&ast.NamedType{
				Token:    node.Token,
				Package:  pkgId,
				Name:     typeId,
				TypeArgs: node.TypeArgs,
			})
		}
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

func (c *CallLowerer) getOrSpecializeFunc(baseName string, typeArgs []sema.Type) (string, *sema.FuncType) {
	// Generic monomorphization belongs to Transform. Lower only resolves the
	// already materialized function recorded in the semantic context.
	var typeSuffixes []string
	for _, t := range typeArgs {
		cleanName := ""
		if cv, ok := t.(*sema.ConstValueType); ok {
			cleanName = fmt.Sprintf("const_%d", cv.Value)
		} else {
			cleanName = strings.ReplaceAll(semaTypeName(t), "*", "ptr_")
		}
		cleanName = strings.ReplaceAll(cleanName, "[]", "slice_")
		typeSuffixes = append(typeSuffixes, cleanName)
	}
	specName := baseName + "_" + strings.Join(typeSuffixes, "_")

	if fn, canonical := c.root.semaCtx.LookupFunction(specName); fn != nil {
		if canonical != "" {
			return canonical, fn
		}
		return specName, fn
	}
	return "", nil
}

func (c *CallLowerer) substFuncDecl(tmpl *ast.FuncDecl, newName string, subst map[string]sema.Type) *ast.FuncDecl {
	newParams := make([]*ast.ParamDecl, len(tmpl.Params))
	for i, p := range tmpl.Params {
		newParams[i] = &ast.ParamDecl{
			Token:      p.Token,
			Name:       p.Name,
			Type:       substTypeExpr(p.Type, subst),
			Default:    p.Default,
			IsVariadic: p.IsVariadic,
			IsEscaped:  p.IsEscaped,
		}
	}

	newReturns := make([]ast.TypeExpr, len(tmpl.ReturnTypes))
	for i, rt := range tmpl.ReturnTypes {
		newReturns[i] = substTypeExpr(rt, subst)
	}

	newBody := substBlockStmt(tmpl.Body, subst)

	return &ast.FuncDecl{
		Token:       tmpl.Token,
		Name:        &ast.Identifier{Token: tmpl.Name.Token, Value: newName},
		Params:      newParams,
		IsVariadic:  tmpl.IsVariadic,
		ReturnTypes: newReturns,
		Body:        newBody,
		InternalKey: sema.BuildInternalKey(c.root.prog.Package, newName, ""),
	}
}

func substTypeExpr(t ast.TypeExpr, subst map[string]sema.Type) ast.TypeExpr {
	if t == nil {
		return nil
	}
	switch node := t.(type) {
	case *ast.NamedType:
		if node.Package == nil && len(node.TypeArgs) == 0 {
			if concreteT, ok := subst[node.Name.Value]; ok {
				return semaTypeToTypeExpr(concreteT)
			}
		}
		newArgs := make([]ast.TypeExpr, len(node.TypeArgs))
		for i, ta := range node.TypeArgs {
			newArgs[i] = substTypeExpr(ta, subst)
		}
		return &ast.NamedType{
			Token:    node.Token,
			Package:  node.Package,
			Name:     node.Name,
			TypeArgs: newArgs,
		}
	case *ast.PointerType:
		return &ast.PointerType{Token: node.Token, Base: substTypeExpr(node.Base, subst)}
	case *ast.SliceType:
		return &ast.SliceType{Token: node.Token, Elem: substTypeExpr(node.Elem, subst)}
	case *ast.ArrayType:
		return &ast.ArrayType{Token: node.Token, Len: node.Len, Elem: substTypeExpr(node.Elem, subst)}
	case *ast.EllipsisType:
		return &ast.EllipsisType{Token: node.Token, Elem: substTypeExpr(node.Elem, subst)}
	case *ast.MapType:
		return &ast.MapType{Token: node.Token, Key: substTypeExpr(node.Key, subst), Value: substTypeExpr(node.Value, subst)}
	}
	return t
}

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

func substBlockStmt(b *ast.BlockStmt, subst map[string]sema.Type) *ast.BlockStmt {
	if b == nil {
		return nil
	}
	newStmts := make([]ast.Statement, len(b.Statements))
	for i, s := range b.Statements {
		newStmts[i] = substStmt(s, subst)
	}
	return &ast.BlockStmt{Token: b.Token, Statements: newStmts}
}

func substStmt(s ast.Statement, subst map[string]sema.Type) ast.Statement {
	if s == nil {
		return nil
	}
	switch stmt := s.(type) {
	case *ast.ReturnStmt:
		newVals := make([]ast.Expression, len(stmt.Values))
		for i, v := range stmt.Values {
			newVals[i] = substExpr(v, subst)
		}
		return &ast.ReturnStmt{Token: stmt.Token, Values: newVals}
	case *ast.AssignStmt:
		newLefts := make([]ast.Expression, len(stmt.Left))
		for i, l := range stmt.Left {
			newLefts[i] = substExpr(l, subst)
		}
		newRights := make([]ast.Expression, len(stmt.Right))
		for i, r := range stmt.Right {
			newRights[i] = substExpr(r, subst)
		}
		return &ast.AssignStmt{Token: stmt.Token, Left: newLefts, Right: newRights, Type: substTypeExpr(stmt.Type, subst)}
	case *ast.VarDecl:
		return &ast.VarDecl{
			Token:     stmt.Token,
			Name:      stmt.Name,
			Type:      substTypeExpr(stmt.Type, subst),
			Value:     substExpr(stmt.Value, subst),
			IsEscaped: stmt.IsEscaped,
		}
	case *ast.ExprStmt:
		return &ast.ExprStmt{Token: stmt.Token, Expr: substExpr(stmt.Expr, subst)}
	case *ast.BlockStmt:
		return substBlockStmt(stmt, subst)
	}
	return s
}

func substExpr(e ast.Expression, subst map[string]sema.Type) ast.Expression {
	if e == nil {
		return nil
	}
	switch expr := e.(type) {
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Token:    expr.Token,
			Left:     substExpr(expr.Left, subst),
			Operator: expr.Operator,
			Right:    substExpr(expr.Right, subst),
		}
	case *ast.CallExpr:
		newArgs := make([]ast.Expression, len(expr.Args))
		for i, a := range expr.Args {
			newArgs[i] = substExpr(a, subst)
		}
		return &ast.CallExpr{
			Token:       expr.Token,
			Function:    substExpr(expr.Function, subst),
			Args:        newArgs,
			HasEllipsis: expr.HasEllipsis,
		}
	case *ast.GenericInstExpr:
		newArgs := make([]ast.TypeExpr, len(expr.TypeArgs))
		for i, ta := range expr.TypeArgs {
			newArgs[i] = substTypeExpr(ta, subst)
		}
		return &ast.GenericInstExpr{
			Token:    expr.Token,
			Left:     substExpr(expr.Left, subst),
			TypeArgs: newArgs,
		}
	}
	return e
}

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
	if slNode, okSlice := call.Args[0].(*ast.SliceType); okSlice {
		elemType := c.root.semaCtx.ResolveType(slNode.Elem)
		resSliceType := &sema.SliceType{Elem: elemType}
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

func (c *CallLowerer) LowerCall(call *ast.CallExpr) hir.Value {
	if call != nil {
		restoreLocation := c.root.setTokenLocation(c.root.sourceFile, call.Token)
		defer restoreLocation()
	}
	logger.LogVerbose2("[Verbose2] Lower call input: function=%T (%+v) args=%d\\n", call.Function, call.Function, len(call.Args))
	// 0. ジェネリクス関数の明示的型引数適用呼び出し (例: Add[float64](a, b))
	if genInst, ok := call.Function.(*ast.GenericInstExpr); ok {
		var baseName string
		if id, okId := genInst.Left.(*ast.Identifier); okId {
			baseName = astIDValue(id)
		} else if mem, okMem := genInst.Left.(*ast.MemberExpr); okMem {
			if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
				baseName = pkgId.Value + "_" + mem.Field.Value
			} else {
				baseName = mem.Field.Value
			}
		}

		if baseName != "" {
			logger.LogVerbose2("[Verbose2] Lower generic call: base=%s typeArgs=%v\\n", baseName, genInst.TypeArgs)
			typeArgs := make([]sema.Type, len(genInst.TypeArgs))
			for i, ta := range genInst.TypeArgs {
				typeArgs[i] = c.root.semaCtx.ResolveType(ta)
			}

			specName, specFn := c.getOrSpecializeFunc(baseName, typeArgs)
			if specFn != nil {
				logger.LogVerbose2("[Verbose2] Lower generic call resolved: callee=%s returnTypes=%v\\n", specName, specFn.ReturnTypes)
				callArgs := c.fillDefaultArgs(call.Args, c.getFuncParams(specFn, specName))
				isCVarArg := semaFuncCFunc(specFn) || semaFuncExtern(specFn) || (semaFuncIsVariadic(specFn) && semaFuncVariadicElem(specFn) == nil)
				args := c.lowerArgs(callArgs, specFn.ParamTypes, semaFuncIsVariadic(specFn), isCVarArg, semaFuncVariadicElem(specFn), call.HasEllipsis)

				var retType sema.Type = sema.TypeVoid
				if len(specFn.ReturnTypes) == 1 {
					retType = specFn.ReturnTypes[0]
				} else if len(specFn.ReturnTypes) > 1 {
					retType = &sema.TupleType{Types: specFn.ReturnTypes}
				}

				var dst *hir.Reg = nil
				if retType != sema.TypeVoid {
					dst = c.root.nextReg(retType)
				}
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: specName, Args: args})
				return dst
			}
		}
	}

	// 1. 型キャスト呼び出し (例: Duration(ns), int64(x), string(cs), cstring(s), uint('a'))
	if len(call.Args) == 1 {
		targetType := c.ResolveTypeFromExpr(call.Function)
		if targetType != nil && targetType != sema.TypeVoid {
			if _, isFunc := targetType.(*sema.FuncType); !isFunc {
				if cl, ok := call.Args[0].(*ast.CharLiteral); ok {
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
					if intRank(sema.LLVMTypeOf(targetType)) > 0 {
						return &hir.ConstInt{Val: int64(cl.CodePoint), Typ: targetType}
					}
				}

				argVal := c.root.Expr.LowerExpr(call.Args[0])

				if (argVal.Type() == sema.TypeString || semaTypeName(argVal.Type()) == "string") && targetType == sema.TypeCString {
					return c.lowerStringToCString(argVal)
				}

				if (argVal.Type() == sema.TypeCString || semaTypeName(argVal.Type()) == "cstring") && targetType == sema.TypeString {
					return c.lowerCStringToString(argVal)
				}

				if _, isSlice := argVal.Type().(*sema.SliceType); isSlice && targetType == sema.TypeString {
					rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
					rawLen := c.root.nextReg(sema.TypeInt)
					c.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: argVal, Index: 0})
					c.root.emit(&hir.InstrExtractValue{Dst: rawLen, Agg: argVal, Index: 1})
					// A Hike string is a pointer/offset/length view.  The slice
					// backing store already has the exact pointer and length needed
					// for that view; copying through __hike_slice_to_str here used
					// the C-string allocation path and could leave the generated
					// native image with an invalid backing pointer.  C-string
					// consumers still receive a terminated copy in
					// lowerStringToCString.
					return c.root.makeString(rawPtr, rawLen)
				}

				if sema.LLVMTypeOf(argVal.Type()) == sema.LLVMTypeOf(targetType) && argVal.Type() == targetType {
					return argVal
				}

				dst := c.root.nextReg(targetType)
				c.root.emit(&hir.InstrCast{Dst: dst, Val: argVal, ToType: targetType})
				return dst
			}
		}
	}

	return c.lowerCallRemainder(call)
}

// LowerCall's remaining dispatch paths live in a separate helper to keep the entry point small.
func (c *CallLowerer) lowerCallRemainder(call *ast.CallExpr) hir.Value {
	// 2. 言語組み込み関数 (make, close, delete, len, cap, append, string, cstring, )
	if fnId, ok := call.Function.(*ast.Identifier); ok {
		switch fnId.Value {
		case "panic":
			if len(call.Args) > 0 {
				c.root.Expr.LowerExpr(call.Args[0])
			}
			c.root.terminate(&hir.InstrUnreachable{})
			return nil

		case "make":
			return c.lowerMakeCall(call)

		case "sizeof":
			return c.lowerSizeofCall(call)

		case "recover":
			// Go-Hike has no host panic value, but compiler packages use recover
			// only to normalize failures.  A nil opaque interface is sufficient.
			return &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}

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
			if fnId.Value == "len" && (argVal.Type() == sema.TypeString || argVal.Type() == sema.TypeCString) {
				if argVal.Type() == sema.TypeString {
					_, length := c.root.stringParts(argVal)
					return length
				}
				dst := c.root.nextReg(sema.TypeInt)
				c.root.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: c.root.BuiltinName("strlen"), Args: []hir.Value{argVal}})
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
			if value, handled := c.lowerStringBuiltin(call); handled {
				return value
			}

		case "cstring":
			if value, handled := c.lowerCStringBuiltin(call); handled {
				return value
			}
		}
	}

	// 3. メンバー式経由の呼び出し (パッケージ関数呼び出し、またはオブジェクトメソッド呼び出し)
	if mem, ok := call.Function.(*ast.MemberExpr); ok {
		// 3A. パッケージ名修飾による関数呼び出し (例: fmt.Printf, time.Now, japanese.NewShiftJIS)
		if pkgIdent, isIdent := mem.Object.(*ast.Identifier); isIdent && c.isPackageName(pkgIdent.Value) {
			return c.lowerPackageMemberCall(call, mem, pkgIdent)
		}

		// 3B. インターフェースまたは構造体/基本型のメソッド呼び出し
		objPtr := c.root.Expr.LowerStructPtr(mem.Object)
		objType := objPtr.Type().(*sema.PointerType).Base

		// インターフェースメソッド呼び出し (動的ディスパッチ)
		if iface, isIface := objType.(*sema.InterfaceType); isIface && !iface.IsAny() {
			return c.lowerInterfaceMethodCall(call, mem, objPtr, iface)
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

			params := c.getFuncParams(targetFn, targetFnName)
			callArgs := c.fillDefaultArgs(call.Args, params)

			methodParamTypes := []sema.Type{}
			if len(targetFn.ParamTypes) > 1 {
				methodParamTypes = targetFn.ParamTypes[1:]
			}
			isCVarArg := semaFuncCFunc(targetFn) || semaFuncExtern(targetFn) || (semaFuncIsVariadic(targetFn) && semaFuncVariadicElem(targetFn) == nil)
			callArgVals := c.lowerArgs(callArgs, methodParamTypes, semaFuncIsVariadic(targetFn), isCVarArg, semaFuncVariadicElem(targetFn), call.HasEllipsis)
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

		// 3C. 構造体フィールドに関数ポインタが格納されている場合
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

		file := c.root.sourceFile
		if file == "" {
			file = "input.hike"
		}
		panic(fmt.Sprintf("%s:%d:%d: method or function field '%s' not found on type '%s'", file, mem.Field.Token.Line, mem.Field.Token.Col, mem.Field.Value, semaTypeName(objType)))
	}

	// 4. 単一識別子によるトップレベル関数呼び出し (例: myFunc())
	if fnId, ok := call.Function.(*ast.Identifier); ok {
		_, isLocal := c.root.symbols[astIDValue(fnId)]
		_, isGlobal := c.root.semaCtx.Globals[astIDValue(fnId)]
		if !isLocal && !isGlobal {
			// パッケージ内の未修飾関数呼び出しは、現在の関数名から
			// パッケージ接頭辞を補って先に完全修飾名で解決する。
			// LookupFunction のサフィックス検索だけに任せると、同名の
			// md5_compress / sha256_compress がmapの反復順で入れ替わる。
			lookupName := astIDValue(fnId)
			var targetFn *sema.FuncType
			var canonicalName string
			if c.root.curFunc != nil {
				if sep := strings.IndexByte(c.root.curFunc.Name, '_'); sep > 0 {
					qualified := c.root.curFunc.Name[:sep] + "_" + astIDValue(fnId)
					targetFn, canonicalName = c.root.semaCtx.LookupFunction(qualified)
				}
			}
			if targetFn == nil {
				targetFn, canonicalName = c.root.semaCtx.LookupFunction(lookupName)
			}
			if targetFn != nil {
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

				var dst *hir.Reg = nil
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
				} else if semaFuncExtern(targetFn) {
					if semaFuncIRName(targetFn) != "" {
						callee = semaFuncIRName(targetFn)
					}
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
		variadicElem = semaFuncVariadicElem(ft)
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
	if call.HasEllipsis && len(call.Args) == 2 {
		srcVal := c.root.Expr.LowerExpr(call.Args[1])
		if srcType, ok := srcVal.Type().(*sema.SliceType); ok {
			return c.lowerAppendSlice(sliceVal, slType, srcVal, srcType)
		}
	}
	elemSize := sema.SizeOf(slType.Elem)
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
	c.root.emit(&hir.InstrCallStatic{Dst: memcpyTmp, CalleeName: c.root.BuiltinName("memcpy"), Args: []hir.Value{newRawPtr, oldRawBytePtr, oldBytes}})

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

// lowerAppendSlice handles append(dst, src...) without treating src itself as
// one element. Both slices use the same fat-pointer layout, so the elements
// can be copied in one operation after allocating the combined backing store.
func (c *CallLowerer) lowerAppendSlice(dst hir.Value, dstType *sema.SliceType, src hir.Value, srcType *sema.SliceType) hir.Value {
	if semaTypeName(dstType.Elem) != semaTypeName(srcType.Elem) {
		return dst
	}
	elemSize := sema.SizeOf(dstType.Elem)
	if elemSize <= 0 {
		elemSize = 1
	}
	oldPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	oldLen := c.root.nextReg(sema.TypeInt)
	srcPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	srcLen := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrExtractValue{Dst: oldPtr, Agg: dst, Index: 0})
	c.root.emit(&hir.InstrExtractValue{Dst: oldLen, Agg: dst, Index: 1})
	c.root.emit(&hir.InstrExtractValue{Dst: srcPtr, Agg: src, Index: 0})
	c.root.emit(&hir.InstrExtractValue{Dst: srcLen, Agg: src, Index: 1})
	totalLen := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: totalLen, Op: hir.OpAdd, L: oldLen, R: srcLen})
	totalBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: totalBytes, Op: hir.OpMul, L: totalLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})
	raw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{Dst: raw, Size: totalBytes, AllocType: sema.TypeByte})
	oldBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: oldBytes, Op: hir.OpMul, L: oldLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})
	srcBytes := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: srcBytes, Op: hir.OpMul, L: srcLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})
	copyDst := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCallStatic{Dst: copyDst, CalleeName: c.root.BuiltinName("memcpy"), Args: []hir.Value{raw, oldPtr, oldBytes}})
	appendPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrGetElemPtr{Dst: appendPtr, BasePtr: raw, Index: oldBytes})
	copySrc := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCallStatic{Dst: copySrc, CalleeName: c.root.BuiltinName("memcpy"), Args: []hir.Value{appendPtr, srcPtr, srcBytes}})
	t1 := c.root.nextReg(dstType)
	c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(dstType), Val: raw, Index: 0})
	t2 := c.root.nextReg(dstType)
	c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: totalLen, Index: 1})
	t3 := c.root.nextReg(dstType)
	c.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: totalLen, Index: 2})
	return t3
}

// -------------------------------------------------------------
// itab 生成
// -------------------------------------------------------------

func (c *CallLowerer) GetOrCreateItab(concreteType sema.Type, iface *sema.InterfaceType) *hir.ItabDef {
	sName := strings.TrimPrefix(semaTypeName(concreteType), "*")
	sName = strings.ReplaceAll(sName, ".", "_")
	ifName := iface.Name
	if ifName == "" {
		ifName = "anon_iface"
	}
	ifName = strings.ReplaceAll(ifName, ".", "_")
	key := fmt.Sprintf("%s_%s", sName, ifName)
	if existing, ok := c.root.itabs[key]; ok {
		return existing
	}

	typeID := concreteType.TypeID(c.root.semaCtx)
	globalName := fmt.Sprintf("__itab_%s_%s", sName, ifName)
	itabStructName := fmt.Sprintf("__itab_%s", ifName)

	methods := []hir.ItabMethodEntry{}

	for _, m := range iface.Methods {
		targetFnName, fnMeta, _, found := c.ResolveMethod(concreteType, m.Name, nil)
		if !found && !strings.HasPrefix(semaTypeName(concreteType), "*") {
			targetFnName, fnMeta, _, found = c.ResolveMethod(&sema.PointerType{Base: concreteType}, m.Name, nil)
		}
		if found && fnMeta != nil {
			targetFnName = semaFuncIRName(fnMeta)
			if targetFnName == "" {
				targetFnName = semaFuncName(fnMeta)
			}
		} else if !found {
			targetFnName = sema.CanonicalMethodName(sName, m.Name)
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
