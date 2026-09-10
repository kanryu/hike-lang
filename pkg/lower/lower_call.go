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
	copy(filled, callArgs)
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
// 文字列相互変換ヘルパー (string <-> cstring)
// -----------------------------------------------------------------------------

func (c *CallLowerer) lowerStringToCString(strVal hir.Value) hir.Value {
	lenReg := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrCallStatic{
		Dst:        lenReg,
		CalleeName: c.root.BuiltinName("strlen"),
		Args:       []hir.Value{strVal},
	})
	oneVal := &hir.ConstInt{Val: 1, Typ: sema.TypeInt}
	sizeReg := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: sizeReg, Op: hir.OpAdd, L: lenReg, R: oneVal})

	bufReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrHeapAlloc{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte})

	c.root.emit(&hir.InstrCallStatic{
		CalleeName: c.root.BuiltinName("memcpy"),
		Args:       []hir.Value{bufReg, strVal, lenReg},
	})

	endPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrGetElemPtr{Dst: endPtr, BasePtr: bufReg, Index: lenReg})
	c.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeByte}, Ptr: endPtr})

	dst := c.root.nextReg(sema.TypeCString)
	c.root.emit(&hir.InstrCast{Dst: dst, Val: bufReg, ToType: sema.TypeCString})
	return dst
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

	dst := c.root.nextReg(sema.TypeString)
	c.root.emit(&hir.InstrCast{Dst: dst, Val: bufReg, ToType: sema.TypeString})
	return dst
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
// 引数パッキング & ロワリングヘルパー
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
// ジェネリクス関数のオンデマンド特殊化 (Monomorphization)
// -----------------------------------------------------------------------------

func (c *CallLowerer) getOrSpecializeFunc(baseName string, typeArgs []sema.Type) (string, *sema.FuncType) {
	var typeSuffixes []string
	for _, t := range typeArgs {
		cleanName := strings.ReplaceAll(t.TypeName(), "*", "ptr_")
		cleanName = strings.ReplaceAll(cleanName, "[]", "slice_")
		typeSuffixes = append(typeSuffixes, cleanName)
	}
	specName := baseName + "_" + strings.Join(typeSuffixes, "_")

	if fn, _ := c.root.semaCtx.LookupFunction(specName); fn != nil {
		return specName, fn
	}

	tmplDecl := c.root.semaCtx.GenericFuncs[baseName]
	if tmplDecl == nil {
		if fn, _ := c.root.semaCtx.LookupFunction(baseName); fn != nil && fn.Template != nil {
			tmplDecl = fn.Template
		}
	}

	if tmplDecl == nil {
		return "", nil
	}

	subst := make(map[string]sema.Type)
	for i, tp := range tmplDecl.TypeParams {
		if i < len(typeArgs) {
			subst[tp.Name.Value] = typeArgs[i]
		}
	}

	specAst := c.substFuncDecl(tmplDecl, specName, subst)

	paramTypes := make([]sema.Type, len(specAst.Params))
	for i, p := range specAst.Params {
		paramTypes[i] = c.root.semaCtx.ResolveType(p.Type)
	}
	returnTypes := make([]sema.Type, len(specAst.ReturnTypes))
	for i, rt := range specAst.ReturnTypes {
		returnTypes[i] = c.root.semaCtx.ResolveType(rt)
	}

	specFnType := &sema.FuncType{
		Name:            specName,
		InternalKey:     sema.BuildInternalKey(c.root.prog.Package, specName, ""),
		IRName:          specName,
		ParamTypes:      paramTypes,
		ReturnTypes:     returnTypes,
		IsSpecialized:   true,
		SpecializedAst:  specAst,
		Specializations: make(map[string]*sema.FuncType),
	}

	c.root.semaCtx.Functions[specName] = specFnType

	prevCurFunc := c.root.curFunc
	c.LowerFunc(specAst)
	c.root.curFunc = prevCurFunc

	return specName, specFnType
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
	case *sema.PointerType:
		return &ast.PointerType{Base: semaTypeToTypeExpr(v.Base)}
	case *sema.SliceType:
		return &ast.SliceType{Elem: semaTypeToTypeExpr(v.Elem)}
	case *sema.ArrayType:
		return &ast.ArrayType{Len: int64(v.Len), Elem: semaTypeToTypeExpr(v.Elem)}
	case *sema.MapType:
		return &ast.MapType{Key: semaTypeToTypeExpr(v.Key), Value: semaTypeToTypeExpr(v.Value)}
	default:
		return &ast.NamedType{Name: &ast.Identifier{Value: v.TypeName()}}
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

// -----------------------------------------------------------------------------
// 関数・メソッド呼び出し (Call)
// -----------------------------------------------------------------------------

func (c *CallLowerer) LowerCall(call *ast.CallExpr) hir.Value {
	// 0. ジェネリクス関数の明示的型引数適用呼び出し (例: Add[float64](a, b))
	if genInst, ok := call.Function.(*ast.GenericInstExpr); ok {
		var baseName string
		if id, okId := genInst.Left.(*ast.Identifier); okId {
			baseName = id.Value
		} else if mem, okMem := genInst.Left.(*ast.MemberExpr); okMem {
			baseName = mem.Field.Value
		}

		if baseName != "" {
			typeArgs := make([]sema.Type, len(genInst.TypeArgs))
			for i, ta := range genInst.TypeArgs {
				typeArgs[i] = c.root.semaCtx.ResolveType(ta)
			}

			specName, specFn := c.getOrSpecializeFunc(baseName, typeArgs)
			if specFn != nil {
				callArgs := c.fillDefaultArgs(call.Args, c.getFuncParams(specFn, specName))
				isCVarArg := specFn.IsCFunc || (specFn.IsVariadic && specFn.VariadicElem == nil)
				args := c.lowerArgs(callArgs, specFn.ParamTypes, specFn.IsVariadic, isCVarArg, specFn.VariadicElem, call.HasEllipsis)

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
				// 文字リテラルから整数型 (uint, int 等) への直接キャスト
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
					if intRank(targetType.LLVMType()) > 0 {
						return &hir.ConstInt{Val: int64(cl.CodePoint), Typ: targetType}
					}
				}

				argVal := c.root.Expr.LowerExpr(call.Args[0])

				// string -> cstring
				if (argVal.Type() == sema.TypeString || argVal.Type().TypeName() == "string") && targetType == sema.TypeCString {
					return c.lowerStringToCString(argVal)
				}

				// cstring -> string
				if (argVal.Type() == sema.TypeCString || argVal.Type().TypeName() == "cstring") && targetType == sema.TypeString {
					return c.lowerCStringToString(argVal)
				}

				// []byte -> string
				if _, isSlice := argVal.Type().(*sema.SliceType); isSlice && targetType == sema.TypeString {
					rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
					rawLen := c.root.nextReg(sema.TypeInt)
					c.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: argVal, Index: 0})
					c.root.emit(&hir.InstrExtractValue{Dst: rawLen, Agg: argVal, Index: 1})
					dst := c.root.nextReg(sema.TypeString)
					c.root.emit(&hir.InstrCallStatic{
						Dst:        dst,
						CalleeName: c.root.BuiltinName("__hike_slice_to_str"),
						Args:       []hir.Value{rawPtr, rawLen},
					})
					return dst
				}

				if argVal.Type().LLVMType() == targetType.LLVMType() && argVal.Type() == targetType {
					return argVal
				}

				dst := c.root.nextReg(targetType)
				c.root.emit(&hir.InstrCast{Dst: dst, Val: argVal, ToType: targetType})
				return dst
			}
		}
	}

	// 2. 言語組み込み関数 (make, close, delete, len, cap, append, string, cstring)
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
			if fnId.Value == "len" && (argVal.Type() == sema.TypeString || argVal.Type() == sema.TypeCString) {
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
			if len(call.Args) > 0 {
				argVal := c.root.Expr.LowerExpr(call.Args[0])
				if _, isSlice := argVal.Type().(*sema.SliceType); isSlice {
					rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
					rawLen := c.root.nextReg(sema.TypeInt)
					c.root.emit(&hir.InstrExtractValue{Dst: rawPtr, Agg: argVal, Index: 0})
					c.root.emit(&hir.InstrExtractValue{Dst: rawLen, Agg: argVal, Index: 1})
					dst := c.root.nextReg(sema.TypeString)
					c.root.emit(&hir.InstrCallStatic{
						Dst:        dst,
						CalleeName: c.root.BuiltinName("__hike_slice_to_str"),
						Args:       []hir.Value{rawPtr, rawLen},
					})
					return dst
				}
				if argVal.Type() == sema.TypeCString || argVal.Type().TypeName() == "cstring" {
					return c.lowerCStringToString(argVal)
				}
				return argVal
			}

		case "cstring":
			if len(call.Args) > 0 {
				argVal := c.root.Expr.LowerExpr(call.Args[0])
				if argVal.Type() == sema.TypeCString {
					return argVal
				}
				if argVal.Type() == sema.TypeString || argVal.Type().TypeName() == "string" {
					return c.lowerStringToCString(argVal)
				}
				dst := c.root.nextReg(sema.TypeCString)
				c.root.emit(&hir.InstrCast{Dst: dst, Val: argVal, ToType: sema.TypeCString})
				return dst
			}
		}
	}

	// 3. メンバー式経由の呼び出し (パッケージ関数呼び出し、またはオブジェクトメソッド呼び出し)
	if mem, ok := call.Function.(*ast.MemberExpr); ok {
		// 3A. パッケージ名修飾による関数呼び出し (例: fmt.Printf, time.Now, sjis.Decode)
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

			params := c.getFuncParams(targetFn, canonicalName)
			callArgs := c.fillDefaultArgs(call.Args, params)

			if targetFn.IsVariadic && targetFn.VariadicElem == nil {
				if fnDecl := c.findFuncDecl(canonicalName); fnDecl != nil && fnDecl.Body != nil {
					return c.inlineVariadicCall(fnDecl, &ast.CallExpr{
						Token:       call.Token,
						Function:    call.Function,
						Args:        callArgs,
						HasEllipsis: call.HasEllipsis,
					})
				}
			}

			isCVarArg := targetFn.IsCFunc || (targetFn.IsVariadic && targetFn.VariadicElem == nil)
			args := c.lowerArgs(callArgs, targetFn.ParamTypes, targetFn.IsVariadic, isCVarArg, targetFn.VariadicElem, call.HasEllipsis)

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
			if targetFn.IsCFunc {
				if targetFn.CFuncAst != nil && !targetFn.CFuncAst.IsAlias() {
					callee = "__hike_impl_" + targetFn.Name
				} else if targetFn.CFuncTarget != "" {
					callee = targetFn.CFuncTarget
				} else {
					callee = "c_" + canonicalName
				}
			} else if targetFn.IsExtern {
				if targetFn.IRName != "" {
					callee = targetFn.IRName
				}
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

			callArgs := call.Args
			args := c.lowerArgs(callArgs, targetMethod.ParamTypes, targetMethod.IsVariadic, false, targetMethod.VariadicElem, call.HasEllipsis)

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

			params := c.getFuncParams(targetFn, targetFnName)
			callArgs := c.fillDefaultArgs(call.Args, params)

			methodParamTypes := []sema.Type{}
			if len(targetFn.ParamTypes) > 1 {
				methodParamTypes = targetFn.ParamTypes[1:]
			}
			isCVarArg := targetFn.IsCFunc || (targetFn.IsVariadic && targetFn.VariadicElem == nil)
			callArgVals := c.lowerArgs(callArgs, methodParamTypes, targetFn.IsVariadic, isCVarArg, targetFn.VariadicElem, call.HasEllipsis)
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
				params := c.getFuncParams(targetFn, canonicalName)
				callArgs := c.fillDefaultArgs(call.Args, params)

				if targetFn.IsVariadic && targetFn.VariadicElem == nil {
					if fnDecl := c.findFuncDecl(canonicalName); fnDecl != nil && fnDecl.Body != nil {
						return c.inlineVariadicCall(fnDecl, &ast.CallExpr{
							Token:       call.Token,
							Function:    call.Function,
							Args:        callArgs,
							HasEllipsis: call.HasEllipsis,
						})
					}
				}

				isCVarArg := targetFn.IsCFunc || (targetFn.IsVariadic && targetFn.VariadicElem == nil)
				args := c.lowerArgs(callArgs, targetFn.ParamTypes, targetFn.IsVariadic, isCVarArg, targetFn.VariadicElem, call.HasEllipsis)

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
				if targetFn.IsCFunc {
					if targetFn.CFuncAst != nil && !targetFn.CFuncAst.IsAlias() {
						callee = "__hike_impl_" + targetFn.Name
					} else if targetFn.CFuncTarget != "" {
						callee = targetFn.CFuncTarget
					} else {
						callee = "c_" + canonicalName
					}
				} else if targetFn.IsExtern {
					if targetFn.IRName != "" {
						callee = targetFn.IRName
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
