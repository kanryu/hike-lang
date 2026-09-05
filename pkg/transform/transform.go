package transform

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

type Transformer struct {
	prog                  *ast.Program
	semaCtx               *sema.Context
	specializedQueue      []*sema.FuncType
	emittedSpecialization map[string]bool
	newSpecializedDecls   []ast.Decl
	localTypes            map[string]ast.TypeExpr
}

func New(prog *ast.Program, semaCtx *sema.Context) *Transformer {
	return &Transformer{
		prog:                  prog,
		semaCtx:               semaCtx,
		specializedQueue:      []*sema.FuncType{},
		emittedSpecialization: make(map[string]bool),
		newSpecializedDecls:   []ast.Decl{},
		localTypes:            make(map[string]ast.TypeExpr),
	}
}

// Transform: AST全体のジェネリクス単相化および構文正規化を実行
func (t *Transformer) Transform() (*ast.Program, error) {
	// Pass 1: 既存の具象関数の走査とジェネリクス呼び出しの単相化
	for _, decl := range t.prog.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Body != nil {
			if sema.IsGenericFuncDecl(fnDecl) {
				continue
			}
			t.transformFuncDecl(fnDecl)
		} else if cfnDecl, ok := decl.(*ast.CFuncDecl); ok && cfnDecl.Body != nil {
			t.transformCFuncDecl(cfnDecl)
		}
	}

	// Pass 2: 単相化によって要求された特殊化関数のワークリストを固定点まで消費
	for len(t.specializedQueue) > 0 {
		fnMeta := t.specializedQueue[0]
		t.specializedQueue = t.specializedQueue[1:]

		if t.emittedSpecialization[fnMeta.Name] || fnMeta.SpecializedAst == nil {
			continue
		}
		t.emittedSpecialization[fnMeta.Name] = true

		// 特殊化関数内部でさらに別のジェネリクス呼び出しが発生している可能性を走査
		t.transformFuncDecl(fnMeta.SpecializedAst)
		t.newSpecializedDecls = append(t.newSpecializedDecls, fnMeta.SpecializedAst)
	}

	// Pass 3: 未具象化のジェネリックテンプレート宣言を除去し、単相化済み具象関数を追加
	concreteDecls := []ast.Decl{}
	for _, decl := range t.prog.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok {
			if sema.IsGenericFuncDecl(fnDecl) {
				continue
			}
		}
		if typeDecl, ok := decl.(*ast.TypeDecl); ok {
			if len(typeDecl.TypeParams) > 0 {
				continue
			}
		}
		concreteDecls = append(concreteDecls, decl)
	}

	concreteDecls = append(concreteDecls, t.newSpecializedDecls...)
	t.prog.Decls = concreteDecls

	return t.prog, nil
}

// -----------------------------------------------------------------------------
// ASTノード走査と呼び出し書き換え
// -----------------------------------------------------------------------------
func (t *Transformer) transformCFuncDecl(cfn *ast.CFuncDecl) {
	if cfn.Body == nil {
		return
	}
	t.localTypes = make(map[string]ast.TypeExpr)
	for _, p := range cfn.Params {
		t.localTypes[p.Name.Value] = p.Type
	}
	t.transformBlock(cfn.Body)
}

func (t *Transformer) transformFuncDecl(fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
	}
	t.localTypes = make(map[string]ast.TypeExpr)
	if fn.Receiver != nil {
		t.localTypes[fn.Receiver.Name.Value] = fn.Receiver.Type
	}
	for _, p := range fn.Params {
		t.localTypes[p.Name.Value] = p.Type
	}
	t.transformBlock(fn.Body)
}

func (t *Transformer) transformBlock(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	for _, stmt := range b.Statements {
		t.transformStmt(stmt)
	}
}

func (t *Transformer) transformStmt(s ast.Statement) {
	if s == nil {
		return
	}
	switch stmt := s.(type) {
	case *ast.VarDecl:
		stmt.Value = t.transformExpr(stmt.Value)
		if stmt.Type != nil {
			t.localTypes[stmt.Name.Value] = stmt.Type
		} else if stmt.Value != nil {
			if inferred := t.inferExprTypeExpr(stmt.Value); inferred != nil {
				t.localTypes[stmt.Name.Value] = inferred
			}
		}
	case *ast.AssignStmt:
		for i, l := range stmt.Left {
			stmt.Left[i] = t.transformExpr(l)
		}
		for i, r := range stmt.Right {
			stmt.Right[i] = t.transformExpr(r)
		}
		if len(stmt.Left) == 1 && len(stmt.Right) == 1 {
			if id, ok := stmt.Left[0].(*ast.Identifier); ok {
				if inferred := t.inferExprTypeExpr(stmt.Right[0]); inferred != nil {
					t.localTypes[id.Value] = inferred
				}
			}
		}
	case *ast.ExprStmt:
		stmt.Expr = t.transformExpr(stmt.Expr)
	case *ast.BlockStmt:
		t.transformBlock(stmt)
	case *ast.IfStmt:
		if stmt.Init != nil {
			t.transformStmt(stmt.Init)
		}
		stmt.Condition = t.transformExpr(stmt.Condition)
		t.transformBlock(stmt.Consequence)
		if stmt.Alternative != nil {
			t.transformStmt(stmt.Alternative)
		}
	case *ast.ForStmt:
		if stmt.Init != nil {
			t.transformStmt(stmt.Init)
		}
		stmt.Cond = t.transformExpr(stmt.Cond)
		if stmt.Post != nil {
			t.transformStmt(stmt.Post)
		}
		t.transformBlock(stmt.Body)
	case *ast.ForRangeStmt:
		stmt.X = t.transformExpr(stmt.X)
		t.transformBlock(stmt.Body)
	case *ast.SwitchStmt:
		if stmt.Init != nil {
			t.transformStmt(stmt.Init)
		}
		stmt.Value = t.transformExpr(stmt.Value)
		for _, cc := range stmt.Cases {
			for j, v := range cc.Values {
				cc.Values[j] = t.transformExpr(v)
			}
			for _, bs := range cc.Body {
				t.transformStmt(bs)
			}
		}
	case *ast.ReturnStmt:
		for i, val := range stmt.Values {
			stmt.Values[i] = t.transformExpr(val)
		}
	case *ast.DeferStmt:
		if stmt.Call != nil {
			if call, ok := t.transformExpr(stmt.Call).(*ast.CallExpr); ok {
				stmt.Call = call
			}
		}
	}
}

func (t *Transformer) transformExpr(e ast.Expression) ast.Expression {
	if e == nil {
		return nil
	}
	switch expr := e.(type) {
	case *ast.BinaryExpr:
		expr.Left = t.transformExpr(expr.Left)
		expr.Right = t.transformExpr(expr.Right)
		return expr

	case *ast.PrefixExpr:
		expr.Right = t.transformExpr(expr.Right)
		return expr

	// 追加: 受信演算子 (<-expr) の走査
	case *ast.ReceiveExpr:
		expr.Expr = t.transformExpr(expr.Expr)
		return expr

	// 追加: Async(fn) 式の走査
	case *ast.AsyncExpr:
		expr.Fn = t.transformExpr(expr.Fn)
		return expr

	case *ast.IndexExpr:
		expr.Left = t.transformExpr(expr.Left)
		expr.Index = t.transformExpr(expr.Index)
		return expr

	case *ast.MemberExpr:
		expr.Object = t.transformExpr(expr.Object)
		return expr

	case *ast.SliceExpr:
		expr.Left = t.transformExpr(expr.Left)
		expr.Low = t.transformExpr(expr.Low)
		expr.High = t.transformExpr(expr.High)
		return expr

	case *ast.TypeAssertExpr:
		expr.Expr = t.transformExpr(expr.Expr)
		return expr

	case *ast.StructLiteral:
		for _, f := range expr.Fields {
			f.Value = t.transformExpr(f.Value)
		}
		return expr

	case *ast.ArrayLiteral:
		for i, el := range expr.Elements {
			expr.Elements[i] = t.transformExpr(el)
		}
		return expr

	case *ast.SliceLiteral:
		for i, el := range expr.Elements {
			expr.Elements[i] = t.transformExpr(el)
		}
		return expr

	case *ast.FuncLit:
		t.transformBlock(expr.Body)
		return expr

	case *ast.CallExpr:
		return t.transformCallExpr(expr)
	}

	return e
}

// -----------------------------------------------------------------------------
// ジェネリクス呼び出しの単相化判定と書き換え
// -----------------------------------------------------------------------------

func (t *Transformer) transformCallExpr(call *ast.CallExpr) ast.Expression {
	call.Function = t.transformExpr(call.Function)
	for i, arg := range call.Args {
		call.Args[i] = t.transformExpr(arg)
	}

	var funcName string
	var funcToken token.Token
	var explicitTypeArgs []sema.Type = nil
	var isMethodCall bool
	var methodReceiverExpr ast.Expression
	var methodReceiverIsPointer bool

	if genExpr, ok := call.Function.(*ast.GenericInstExpr); ok {
		funcToken = genExpr.Token
		funcName = t.resolveTargetName(genExpr.Left)
		for _, tArg := range genExpr.TypeArgs {
			explicitTypeArgs = append(explicitTypeArgs, t.semaCtx.ResolveType(tArg))
		}
	} else if idxExpr, ok := call.Function.(*ast.IndexExpr); ok {
		funcToken = idxExpr.Token
		funcName = t.resolveTargetName(idxExpr.Left)
		if tExpr := exprToTypeExpr(idxExpr.Index); tExpr != nil {
			if tArg := t.semaCtx.ResolveType(tExpr); tArg != nil && tArg != sema.TypeVoid {
				explicitTypeArgs = append(explicitTypeArgs, tArg)
			}
		}
	} else if id, ok := call.Function.(*ast.Identifier); ok {
		if genTemplate := t.findGenericTemplate(id.Value); genTemplate != nil {
			funcName = id.Value
			funcToken = id.Token
		}
	} else if mem, ok := call.Function.(*ast.MemberExpr); ok {
		objTypeExpr := t.inferExprTypeExpr(mem.Object)
		if objTypeExpr != nil {
			structName, typeArgs, isPtr := extractStructAndTypeArgs(objTypeExpr)
			if structName != "" {
				methodCandidate := structName + "_" + mem.Field.Value
				if genTemplate := t.findGenericTemplate(methodCandidate); genTemplate != nil {
					funcName = methodCandidate
					funcToken = mem.Field.Token
					isMethodCall = true
					methodReceiverExpr = mem.Object
					methodReceiverIsPointer = isPtr

					for _, targ := range typeArgs {
						if resolved := t.semaCtx.ResolveType(targ); resolved != nil && resolved != sema.TypeVoid {
							explicitTypeArgs = append(explicitTypeArgs, resolved)
						}
					}
				}
			}
		}

		if funcName == "" {
			if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
				if _, isLocalVar := t.localTypes[pkgId.Value]; !isLocalVar {
					pkgFnName := pkgId.Value + "_" + mem.Field.Value
					if genTemplate := t.findGenericTemplate(pkgFnName); genTemplate != nil {
						funcName = pkgFnName
						funcToken = mem.Field.Token
					}
				}
			}
		}
	}

	if funcName != "" {
		if genTemplate := t.findGenericTemplate(funcName); genTemplate != nil {
			var typeArgs []sema.Type
			if len(explicitTypeArgs) > 0 {
				typeArgs = explicitTypeArgs
			} else {
				for i, arg := range call.Args {
					if i < len(genTemplate.Params) {
						argType := t.semaCtx.InferExprType(arg, nil)
						typeArgs = append(typeArgs, argType)
					}
				}
			}

			if len(typeArgs) > 0 {
				specName := t.getOrCreateSpecializedFunc(funcName, typeArgs)
				call.Function = &ast.Identifier{Token: funcToken, Value: specName}

				if isMethodCall && methodReceiverExpr != nil {
					receiverArg := methodReceiverExpr
					if genTemplate.Receiver != nil {
						_, templateRecvIsPtr := genTemplate.Receiver.Type.(*ast.PointerType)
						if templateRecvIsPtr && !methodReceiverIsPointer {
							receiverArg = &ast.PrefixExpr{
								Token:    funcToken,
								Operator: "&",
								Right:    methodReceiverExpr,
							}
						} else if !templateRecvIsPtr && methodReceiverIsPointer {
							receiverArg = &ast.PrefixExpr{
								Token:    funcToken,
								Operator: "*",
								Right:    methodReceiverExpr,
							}
						}
					}
					call.Args = append([]ast.Expression{receiverArg}, call.Args...)
				}
			}
		}
	}

	return call
}

func (t *Transformer) resolveTargetName(e ast.Expression) string {
	if id, ok := e.(*ast.Identifier); ok {
		return id.Value
	}
	if mem, ok := e.(*ast.MemberExpr); ok {
		if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
			return pkgId.Value + "_" + mem.Field.Value
		}
	}
	return ""
}

// -----------------------------------------------------------------------------
// 特殊化（単相化）AST複製ロジック
// -----------------------------------------------------------------------------

func (t *Transformer) findGenericTemplate(name string) *ast.FuncDecl {
	switch name {
	case "int", "byte", "bool", "float32", "float64", "float", "string", "void", "any", "error":
		return nil
	}

	if strings.Contains(name, "_") {
		parts := strings.SplitN(name, "_", 2)
		structName := parts[0]
		methodName := parts[1]
		for _, decl := range t.prog.Decls {
			if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Receiver != nil {
				if getBaseTypeName(fnDecl.Receiver.Type) == structName && fnDecl.Name.Value == methodName {
					return fnDecl
				}
			}
		}
	}

	if tmpl, ok := t.semaCtx.GenericFuncs[name]; ok && tmpl != nil {
		return tmpl
	}
	for k, v := range t.semaCtx.GenericFuncs {
		if v != nil && (k == name || strings.HasSuffix(k, "_"+name)) {
			return v
		}
	}
	if fn, ok := t.semaCtx.Functions[name]; ok && fn != nil && fn.Template != nil && fn.IsGeneric() {
		return fn.Template
	}
	for k, fn := range t.semaCtx.Functions {
		if fn != nil && fn.Template != nil && fn.IsGeneric() {
			if k == name || strings.HasSuffix(k, "_"+name) {
				return fn.Template
			}
		}
	}
	return nil
}

func (t *Transformer) getOrCreateSpecializedFunc(baseName string, typeArgs []sema.Type) string {
	template := t.findGenericTemplate(baseName)
	if template == nil {
		return baseName
	}

	origFnMeta, _ := t.semaCtx.LookupFunction(baseName)

	argNames := []string{}
	for _, typ := range typeArgs {
		name := strings.ReplaceAll(typ.TypeName(), "*", "Ptr")
		name = strings.ReplaceAll(name, "[]", "Slice_")
		argNames = append(argNames, name)
	}
	specKey := strings.Join(argNames, "_")
	specializedName := fmt.Sprintf("%s__%s", baseName, specKey)

	if origFnMeta != nil && origFnMeta.Specializations != nil {
		if existing, ok := origFnMeta.Specializations[specKey]; ok {
			return existing.Name
		}
	}

	if existing, ok := t.semaCtx.Functions[specializedName]; ok {
		if origFnMeta != nil && origFnMeta.Specializations != nil {
			origFnMeta.Specializations[specKey] = existing
		}
		return specializedName
	}

	typeParamNames := []string{}
	for _, tp := range template.TypeParams {
		typeParamNames = append(typeParamNames, tp.Name.Value)
	}
	if len(typeParamNames) == 0 && template.Receiver != nil {
		recvTypeName := getBaseTypeName(template.Receiver.Type)
		if st, _ := t.semaCtx.LookupStruct(recvTypeName); st != nil && len(st.TypeParams) > 0 {
			typeParamNames = append(typeParamNames, st.TypeParams...)
		} else {
			if named := getNamedTypeFromExpr(template.Receiver.Type); named != nil {
				for _, tArg := range named.TypeArgs {
					if id, ok := tArg.(*ast.NamedType); ok {
						typeParamNames = append(typeParamNames, id.Name.Value)
					}
				}
			}
		}
	}
	if len(typeParamNames) == 0 && origFnMeta != nil {
		typeParamNames = origFnMeta.TypeParams
	}

	typeMap := make(map[string]ast.TypeExpr)
	orderedTypeArgs := []ast.TypeExpr{}
	for i, tpName := range typeParamNames {
		if i < len(typeArgs) {
			tNode := &ast.NamedType{
				Token: template.Token,
				Name:  &ast.Identifier{Token: template.Token, Value: typeArgs[i].TypeName()},
			}
			typeMap[tpName] = tNode
			orderedTypeArgs = append(orderedTypeArgs, tNode)
		}
	}

	cloned := t.cloneFuncDecl(template, specializedName, typeMap, orderedTypeArgs)

	fnType := &sema.FuncType{
		Name:            specializedName,
		TypeParams:      typeParamNames,
		TypeArgs:        typeArgs,
		IsMethod:        false,
		ParamTypes:      []sema.Type{},
		ReturnTypes:     []sema.Type{},
		IsVariadic:      cloned.IsVariadic,
		IsExtern:        (cloned.Body == nil),
		Template:        template,
		IsSpecialized:   true,
		SpecializedAst:  cloned,
		Emitted:         false,
		Specializations: make(map[string]*sema.FuncType),
	}

	for _, p := range cloned.Params {
		fnType.ParamTypes = append(fnType.ParamTypes, t.semaCtx.ResolveType(p.Type))
	}
	for _, rt := range cloned.ReturnTypes {
		fnType.ReturnTypes = append(fnType.ReturnTypes, t.semaCtx.ResolveType(rt))
	}

	t.semaCtx.Functions[specializedName] = fnType
	if origFnMeta != nil && origFnMeta.Specializations != nil {
		origFnMeta.Specializations[specKey] = fnType
	}

	t.specializedQueue = append(t.specializedQueue, fnType)
	return specializedName
}

func (t *Transformer) cloneFuncDecl(fn *ast.FuncDecl, newName string, typeMap map[string]ast.TypeExpr, orderedTypeArgs []ast.TypeExpr) *ast.FuncDecl {
	newParams := []*ast.ParamDecl{}
	if fn.Receiver != nil {
		newParams = append(newParams, &ast.ParamDecl{
			Token: fn.Receiver.Token,
			Name:  fn.Receiver.Name,
			Type:  t.substituteAstType(fn.Receiver.Type, typeMap, orderedTypeArgs),
		})
	}
	for _, p := range fn.Params {
		newParams = append(newParams, &ast.ParamDecl{
			Token: p.Token,
			Name:  p.Name,
			Type:  t.substituteAstType(p.Type, typeMap, orderedTypeArgs),
		})
	}
	newReturns := []ast.TypeExpr{}
	for _, rt := range fn.ReturnTypes {
		newReturns = append(newReturns, t.substituteAstType(rt, typeMap, orderedTypeArgs))
	}
	var newBody *ast.BlockStmt = nil
	if fn.Body != nil {
		newBody = t.substituteAstBlock(fn.Body, typeMap, orderedTypeArgs)
	}
	return &ast.FuncDecl{
		Token:       fn.Token,
		Receiver:    nil,
		Name:        &ast.Identifier{Token: fn.Name.Token, Value: newName},
		TypeParams:  nil,
		Params:      newParams,
		IsVariadic:  fn.IsVariadic,
		ReturnTypes: newReturns,
		Body:        newBody,
	}
}

func (t *Transformer) substituteAstType(typ ast.TypeExpr, typeMap map[string]ast.TypeExpr, orderedTypeArgs []ast.TypeExpr) ast.TypeExpr {
	if typ == nil {
		return nil
	}
	switch node := typ.(type) {
	case *ast.NamedType:
		if node.Package == nil && len(node.TypeArgs) == 0 {
			if rep, ok := typeMap[node.Name.Value]; ok {
				return rep
			}
		}

		newArgs := []ast.TypeExpr{}
		for _, arg := range node.TypeArgs {
			newArgs = append(newArgs, t.substituteAstType(arg, typeMap, orderedTypeArgs))
		}

		typeName := node.Name.Value
		if node.Package != nil {
			typeName = node.Package.Value + "_" + node.Name.Value
		}
		if len(newArgs) == 0 && len(orderedTypeArgs) > 0 {
			if st, _ := t.semaCtx.LookupStruct(typeName); st != nil && st.IsGeneric() {
				if len(orderedTypeArgs) == len(st.TypeParams) {
					newArgs = orderedTypeArgs
				}
			} else if iface, _ := t.semaCtx.LookupInterface(typeName); iface != nil && iface.IsGeneric() {
				if len(orderedTypeArgs) == len(iface.TypeParams) {
					newArgs = orderedTypeArgs
				}
			}
		}

		if len(newArgs) > 0 {
			resolvedType := t.semaCtx.ResolveType(&ast.NamedType{
				Token:    node.Token,
				Package:  node.Package,
				Name:     node.Name,
				TypeArgs: newArgs,
			})
			if resolvedType != nil && resolvedType != sema.TypeVoid {
				return &ast.NamedType{
					Token:    node.Token,
					Package:  node.Package,
					Name:     &ast.Identifier{Token: node.Token, Value: resolvedType.TypeName()},
					TypeArgs: nil,
				}
			}
		}

		return &ast.NamedType{
			Token:    node.Token,
			Package:  node.Package,
			Name:     node.Name,
			TypeArgs: newArgs,
		}

	case *ast.PointerType:
		return &ast.PointerType{Token: node.Token, Base: t.substituteAstType(node.Base, typeMap, orderedTypeArgs)}
	case *ast.SliceType:
		return &ast.SliceType{Token: node.Token, Elem: t.substituteAstType(node.Elem, typeMap, orderedTypeArgs)}
	case *ast.ArrayType:
		return &ast.ArrayType{Token: node.Token, Len: node.Len, Elem: t.substituteAstType(node.Elem, typeMap, orderedTypeArgs)}
	case *ast.MapType:
		return &ast.MapType{
			Token: node.Token,
			Key:   t.substituteAstType(node.Key, typeMap, orderedTypeArgs),
			Value: t.substituteAstType(node.Value, typeMap, orderedTypeArgs),
		}

	// 追加: チャネル型の置換
	case *ast.ChanType:
		return &ast.ChanType{
			Token: node.Token,
			Elem:  t.substituteAstType(node.Elem, typeMap, orderedTypeArgs),
		}

	// 追加: Future型の置換
	case *ast.FutureType:
		newReturns := make([]ast.TypeExpr, len(node.ReturnTypes))
		for i, rt := range node.ReturnTypes {
			newReturns[i] = t.substituteAstType(rt, typeMap, orderedTypeArgs)
		}
		return &ast.FutureType{
			Token:       node.Token,
			ReturnTypes: newReturns,
		}

	case *ast.FuncType:
		newParams := make([]ast.TypeExpr, len(node.ParamTypes))
		for i, pt := range node.ParamTypes {
			newParams[i] = t.substituteAstType(pt, typeMap, orderedTypeArgs)
		}
		newReturns := make([]ast.TypeExpr, len(node.ReturnTypes))
		for i, rt := range node.ReturnTypes {
			newReturns[i] = t.substituteAstType(rt, typeMap, orderedTypeArgs)
		}
		return &ast.FuncType{
			Token:       node.Token,
			ParamTypes:  newParams,
			IsVariadic:  node.IsVariadic,
			ReturnTypes: newReturns,
		}
	}
	return typ
}

func (t *Transformer) substituteAstBlock(b *ast.BlockStmt, typeMap map[string]ast.TypeExpr, orderedTypeArgs []ast.TypeExpr) *ast.BlockStmt {
	if b == nil {
		return nil
	}
	newStmts := make([]ast.Statement, len(b.Statements))
	for i, s := range b.Statements {
		newStmts[i] = t.substituteAstStmt(s, typeMap, orderedTypeArgs)
	}
	return &ast.BlockStmt{Token: b.Token, Statements: newStmts}
}

func (t *Transformer) substituteAstStmt(s ast.Statement, typeMap map[string]ast.TypeExpr, orderedTypeArgs []ast.TypeExpr) ast.Statement {
	if s == nil {
		return nil
	}
	switch st := s.(type) {
	case *ast.VarDecl:
		return &ast.VarDecl{
			Token:     st.Token,
			Name:      st.Name,
			Type:      t.substituteAstType(st.Type, typeMap, orderedTypeArgs),
			Value:     t.substituteAstExpr(st.Value, typeMap, orderedTypeArgs),
			IsEscaped: st.IsEscaped,
		}
	case *ast.AssignStmt:
		newLeft := make([]ast.Expression, len(st.Left))
		for i, l := range st.Left {
			newLeft[i] = t.substituteAstExpr(l, typeMap, orderedTypeArgs)
		}
		newRight := make([]ast.Expression, len(st.Right))
		for i, r := range st.Right {
			newRight[i] = t.substituteAstExpr(r, typeMap, orderedTypeArgs)
		}
		return &ast.AssignStmt{
			Token: st.Token,
			Left:  newLeft,
			Right: newRight,
			Type:  t.substituteAstType(st.Type, typeMap, orderedTypeArgs),
		}
	case *ast.ExprStmt:
		return &ast.ExprStmt{Token: st.Token, Expr: t.substituteAstExpr(st.Expr, typeMap, orderedTypeArgs)}
	case *ast.BlockStmt:
		return t.substituteAstBlock(st, typeMap, orderedTypeArgs)
	case *ast.IfStmt:
		return &ast.IfStmt{
			Token:       st.Token,
			Init:        t.substituteAstStmt(st.Init, typeMap, orderedTypeArgs),
			Condition:   t.substituteAstExpr(st.Condition, typeMap, orderedTypeArgs),
			Consequence: t.substituteAstBlock(st.Consequence, typeMap, orderedTypeArgs),
			Alternative: t.substituteAstStmt(st.Alternative, typeMap, orderedTypeArgs),
		}
	case *ast.ForStmt:
		return &ast.ForStmt{
			Token: st.Token,
			Init:  t.substituteAstStmt(st.Init, typeMap, orderedTypeArgs),
			Cond:  t.substituteAstExpr(st.Cond, typeMap, orderedTypeArgs),
			Post:  t.substituteAstStmt(st.Post, typeMap, orderedTypeArgs),
			Body:  t.substituteAstBlock(st.Body, typeMap, orderedTypeArgs),
		}
	case *ast.ForRangeStmt:
		return &ast.ForRangeStmt{
			Token: st.Token,
			Key:   t.substituteAstExpr(st.Key, typeMap, orderedTypeArgs),
			Value: t.substituteAstExpr(st.Value, typeMap, orderedTypeArgs),
			X:     t.substituteAstExpr(st.X, typeMap, orderedTypeArgs),
			Body:  t.substituteAstBlock(st.Body, typeMap, orderedTypeArgs),
		}
	case *ast.ReturnStmt:
		newVals := make([]ast.Expression, len(st.Values))
		for i, v := range st.Values {
			newVals[i] = t.substituteAstExpr(v, typeMap, orderedTypeArgs)
		}
		return &ast.ReturnStmt{Token: st.Token, Values: newVals}
	}
	return s
}

func (t *Transformer) substituteAstExpr(e ast.Expression, typeMap map[string]ast.TypeExpr, orderedTypeArgs []ast.TypeExpr) ast.Expression {
	if e == nil {
		return nil
	}

	if te, ok := e.(ast.TypeExpr); ok {
		if substituted := t.substituteAstType(te, typeMap, orderedTypeArgs); substituted != nil {
			if expr, okExpr := substituted.(ast.Expression); okExpr {
				return expr
			}
		}
	}

	switch node := e.(type) {
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Token:    node.Token,
			Left:     t.substituteAstExpr(node.Left, typeMap, orderedTypeArgs),
			Operator: node.Operator,
			Right:    t.substituteAstExpr(node.Right, typeMap, orderedTypeArgs),
		}
	case *ast.PrefixExpr:
		return &ast.PrefixExpr{
			Token:    node.Token,
			Operator: node.Operator,
			Right:    t.substituteAstExpr(node.Right, typeMap, orderedTypeArgs),
		}

	// 追加: 受信演算子 (<-expr) の複製置換
	case *ast.ReceiveExpr:
		return &ast.ReceiveExpr{
			Token: node.Token,
			Expr:  t.substituteAstExpr(node.Expr, typeMap, orderedTypeArgs),
		}

	// 追加: Async(fn) 式の複製置換
	case *ast.AsyncExpr:
		return &ast.AsyncExpr{
			Token: node.Token,
			Fn:    t.substituteAstExpr(node.Fn, typeMap, orderedTypeArgs),
		}

	case *ast.CallExpr:
		newArgs := make([]ast.Expression, len(node.Args))
		for i, arg := range node.Args {
			newArgs[i] = t.substituteAstExpr(arg, typeMap, orderedTypeArgs)
		}
		return &ast.CallExpr{
			Token:       node.Token,
			Function:    t.substituteAstExpr(node.Function, typeMap, orderedTypeArgs),
			Args:        newArgs,
			HasEllipsis: node.HasEllipsis,
		}
	case *ast.MemberExpr:
		return &ast.MemberExpr{
			Token:  node.Token,
			Object: t.substituteAstExpr(node.Object, typeMap, orderedTypeArgs),
			Field:  node.Field,
		}
	case *ast.IndexExpr:
		return &ast.IndexExpr{
			Token: node.Token,
			Left:  t.substituteAstExpr(node.Left, typeMap, orderedTypeArgs),
			Index: t.substituteAstExpr(node.Index, typeMap, orderedTypeArgs),
		}
	case *ast.SliceExpr:
		return &ast.SliceExpr{
			Token: node.Token,
			Left:  t.substituteAstExpr(node.Left, typeMap, orderedTypeArgs),
			Low:   t.substituteAstExpr(node.Low, typeMap, orderedTypeArgs),
			High:  t.substituteAstExpr(node.High, typeMap, orderedTypeArgs),
		}
	case *ast.GenericInstExpr:
		newArgs := make([]ast.TypeExpr, len(node.TypeArgs))
		for i, arg := range node.TypeArgs {
			newArgs[i] = t.substituteAstType(arg, typeMap, orderedTypeArgs)
		}
		return &ast.GenericInstExpr{
			Token:    node.Token,
			Left:     t.substituteAstExpr(node.Left, typeMap, orderedTypeArgs),
			TypeArgs: newArgs,
		}
	case *ast.StructLiteral:
		newType := node.Type
		if substituted := t.substituteAstType(node.Type, typeMap, orderedTypeArgs); substituted != nil {
			if named, ok := substituted.(*ast.NamedType); ok {
				newType = named
			}
		}
		newFields := make([]*ast.StructFieldValue, len(node.Fields))
		for i, f := range node.Fields {
			newFields[i] = &ast.StructFieldValue{
				Name:  f.Name,
				Value: t.substituteAstExpr(f.Value, typeMap, orderedTypeArgs),
			}
		}
		return &ast.StructLiteral{
			Token:  node.Token,
			Type:   newType,
			Fields: newFields,
		}
	case *ast.ArrayLiteral:
		newType := node.Type
		if substituted := t.substituteAstType(node.Type, typeMap, orderedTypeArgs); substituted != nil {
			if at, ok := substituted.(*ast.ArrayType); ok {
				newType = at
			}
		}
		newElems := make([]ast.Expression, len(node.Elements))
		for i, el := range node.Elements {
			newElems[i] = t.substituteAstExpr(el, typeMap, orderedTypeArgs)
		}
		return &ast.ArrayLiteral{
			Token:    node.Token,
			Type:     newType,
			Elements: newElems,
		}
	case *ast.SliceLiteral:
		newType := node.Type
		if substituted := t.substituteAstType(node.Type, typeMap, orderedTypeArgs); substituted != nil {
			if st, ok := substituted.(*ast.SliceType); ok {
				newType = st
			}
		}
		newElems := make([]ast.Expression, len(node.Elements))
		for i, el := range node.Elements {
			newElems[i] = t.substituteAstExpr(el, typeMap, orderedTypeArgs)
		}
		return &ast.SliceLiteral{
			Token:    node.Token,
			Type:     newType,
			Elements: newElems,
		}
	case *ast.TypeAssertExpr:
		return &ast.TypeAssertExpr{
			Token:  node.Token,
			Expr:   t.substituteAstExpr(node.Expr, typeMap, orderedTypeArgs),
			Target: t.substituteAstType(node.Target, typeMap, orderedTypeArgs),
		}
	case *ast.FuncLit:
		newParams := make([]*ast.ParamDecl, len(node.Params))
		for i, p := range node.Params {
			newParams[i] = &ast.ParamDecl{
				Token:     p.Token,
				Name:      p.Name,
				Type:      t.substituteAstType(p.Type, typeMap, orderedTypeArgs),
				IsEscaped: p.IsEscaped,
			}
		}
		newReturns := make([]ast.TypeExpr, len(node.ReturnTypes))
		for i, rt := range node.ReturnTypes {
			newReturns[i] = t.substituteAstType(rt, typeMap, orderedTypeArgs)
		}
		var newBody *ast.BlockStmt = nil
		if node.Body != nil {
			newBody = t.substituteAstBlock(node.Body, typeMap, orderedTypeArgs)
		}
		return &ast.FuncLit{
			Token:       node.Token,
			Params:      newParams,
			IsVariadic:  node.IsVariadic,
			ReturnTypes: newReturns,
			Body:        newBody,
		}
	}
	return e
}

func getBaseTypeName(typ ast.TypeExpr) string {
	if typ == nil {
		return ""
	}
	switch node := typ.(type) {
	case *ast.PointerType:
		return getBaseTypeName(node.Base)
	case *ast.SliceType:
		return getBaseTypeName(node.Elem)
	case *ast.ArrayType:
		return getBaseTypeName(node.Elem)
	case *ast.NamedType:
		if node.Package != nil {
			return node.Package.Value + "_" + node.Name.Value
		}
		return node.Name.Value
	}
	return ""
}

func exprToTypeExpr(e ast.Expression) ast.TypeExpr {
	if e == nil {
		return nil
	}
	if te, ok := e.(ast.TypeExpr); ok {
		return te
	}
	if id, ok := e.(*ast.Identifier); ok {
		return &ast.NamedType{Token: id.Token, Name: id}
	}
	if pref, ok := e.(*ast.PrefixExpr); ok && pref.Operator == "*" {
		base := exprToTypeExpr(pref.Right)
		if base != nil {
			return &ast.PointerType{Token: pref.Token, Base: base}
		}
	}
	if mem, ok := e.(*ast.MemberExpr); ok {
		if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
			return &ast.NamedType{
				Token:   pkgId.Token,
				Package: pkgId,
				Name:    mem.Field,
			}
		}
	}
	if fl, ok := e.(*ast.FuncLit); ok {
		pts := make([]ast.TypeExpr, len(fl.Params))
		for i, p := range fl.Params {
			pts[i] = p.Type
		}
		return &ast.FuncType{
			Token:       fl.Token,
			ParamTypes:  pts,
			ReturnTypes: fl.ReturnTypes,
		}
	}
	return nil
}

func splitTypeArgs(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '<':
			depth++
		case ']', '>':
			depth--
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(s[start:i])
				if part != "" {
					parts = append(parts, part)
				}
				start = i + 1
			}
		}
	}
	if start < len(s) {
		part := strings.TrimSpace(s[start:])
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func parseSimpleTypeExpr(tok token.Token, typeName string) ast.TypeExpr {
	typeName = strings.TrimSpace(typeName)
	if strings.HasPrefix(typeName, "*") {
		return &ast.PointerType{
			Token: tok,
			Base:  parseSimpleTypeExpr(tok, strings.TrimPrefix(typeName, "*")),
		}
	}
	if strings.HasPrefix(typeName, "[]") {
		return &ast.SliceType{
			Token: tok,
			Elem:  parseSimpleTypeExpr(tok, strings.TrimPrefix(typeName, "[]")),
		}
	}
	idx := strings.Index(typeName, "[")
	if idx != -1 && strings.HasSuffix(typeName, "]") {
		base := typeName[:idx]
		argsStr := typeName[idx+1 : len(typeName)-1]
		var args []ast.TypeExpr
		for _, p := range splitTypeArgs(argsStr) {
			args = append(args, parseSimpleTypeExpr(tok, p))
		}
		return &ast.NamedType{
			Token:    tok,
			Name:     &ast.Identifier{Token: tok, Value: base},
			TypeArgs: args,
		}
	}
	return &ast.NamedType{
		Token: tok,
		Name:  &ast.Identifier{Token: tok, Value: typeName},
	}
}

func getNamedTypeFromExpr(typ ast.TypeExpr) *ast.NamedType {
	if typ == nil {
		return nil
	}
	switch node := typ.(type) {
	case *ast.PointerType:
		return getNamedTypeFromExpr(node.Base)
	case *ast.NamedType:
		return node
	}
	return nil
}

func (t *Transformer) inferExprTypeExpr(e ast.Expression) ast.TypeExpr {
	if e == nil {
		return nil
	}
	switch expr := e.(type) {
	case *ast.StructLiteral:
		return expr.Type
	case *ast.PrefixExpr:
		if expr.Operator == "&" {
			base := t.inferExprTypeExpr(expr.Right)
			if base != nil {
				return &ast.PointerType{Token: expr.Token, Base: base}
			}
		}
	case *ast.Identifier:
		if typ, ok := t.localTypes[expr.Value]; ok {
			return typ
		}
	case *ast.MemberExpr:
		objTyp := t.inferExprTypeExpr(expr.Object)
		if objTyp != nil {
			structName, _, _ := extractStructAndTypeArgs(objTyp)
			if st, _ := t.semaCtx.LookupStruct(structName); st != nil {
				for _, f := range st.Fields {
					if f.Name == expr.Field.Value {
						return parseSimpleTypeExpr(expr.Field.Token, f.Type.TypeName())
					}
				}
			}
		}
	case *ast.CallExpr:
		var targetName string
		var tArgs []ast.TypeExpr

		if id, ok := expr.Function.(*ast.Identifier); ok {
			targetName = id.Value
		} else if idxExpr, ok := expr.Function.(*ast.IndexExpr); ok {
			targetName = t.resolveTargetName(idxExpr.Left)
			if tExpr := exprToTypeExpr(idxExpr.Index); tExpr != nil {
				tArgs = append(tArgs, tExpr)
			}
		} else if genExpr, ok := expr.Function.(*ast.GenericInstExpr); ok {
			targetName = t.resolveTargetName(genExpr.Left)
			tArgs = append(tArgs, genExpr.TypeArgs...)
		}

		if targetName != "" {
			if fnMeta, ok := t.semaCtx.Functions[targetName]; ok && fnMeta != nil {
				if fnMeta.SpecializedAst != nil && len(fnMeta.SpecializedAst.ReturnTypes) > 0 {
					return fnMeta.SpecializedAst.ReturnTypes[0]
				}
				if len(fnMeta.ReturnTypes) > 0 {
					return parseSimpleTypeExpr(expr.Token, fnMeta.ReturnTypes[0].TypeName())
				}
			}
			if tmpl := t.findGenericTemplate(targetName); tmpl != nil && len(tmpl.ReturnTypes) > 0 {
				if len(tArgs) > 0 {
					typeMap := make(map[string]ast.TypeExpr)
					for i, tp := range tmpl.TypeParams {
						if i < len(tArgs) {
							typeMap[tp.Name.Value] = tArgs[i]
						}
					}
					return t.substituteAstType(tmpl.ReturnTypes[0], typeMap, tArgs)
				}
				return tmpl.ReturnTypes[0]
			}
		}
	}
	return nil
}

func extractStructAndTypeArgs(typ ast.TypeExpr) (structName string, typeArgs []ast.TypeExpr, isPtr bool) {
	if typ == nil {
		return "", nil, false
	}
	if ptr, ok := typ.(*ast.PointerType); ok {
		sName, tArgs, _ := extractStructAndTypeArgs(ptr.Base)
		return sName, tArgs, true
	}
	if named, ok := typ.(*ast.NamedType); ok {
		sName := named.Name.Value
		if named.Package != nil {
			sName = named.Package.Value + "_" + sName
		}

		if idx := strings.Index(sName, "["); idx != -1 && strings.HasSuffix(sName, "]") {
			base := sName[:idx]
			argsStr := sName[idx+1 : len(sName)-1]
			var args []ast.TypeExpr
			for _, p := range splitTypeArgs(argsStr) {
				args = append(args, parseSimpleTypeExpr(named.Token, p))
			}
			return base, args, false
		}

		if strings.Contains(sName, "__") {
			parts := strings.SplitN(sName, "__", 2)
			base := parts[0]
			if len(named.TypeArgs) > 0 {
				return base, named.TypeArgs, false
			}
			argsParts := strings.Split(parts[1], "_")
			var args []ast.TypeExpr
			for _, p := range argsParts {
				if p != "" {
					args = append(args, parseSimpleTypeExpr(named.Token, p))
				}
			}
			return base, args, false
		}

		if len(named.TypeArgs) > 0 {
			return sName, named.TypeArgs, false
		}
		return sName, nil, false
	}
	return "", nil, false
}
