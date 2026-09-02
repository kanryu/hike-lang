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
}

func New(prog *ast.Program, semaCtx *sema.Context) *Transformer {
	return &Transformer{
		prog:                  prog,
		semaCtx:               semaCtx,
		specializedQueue:      []*sema.FuncType{},
		emittedSpecialization: make(map[string]bool),
		newSpecializedDecls:   []ast.Decl{},
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

func (t *Transformer) transformFuncDecl(fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
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
	case *ast.AssignStmt:
		for i, l := range stmt.Left {
			stmt.Left[i] = t.transformExpr(l)
		}
		for i, r := range stmt.Right {
			stmt.Right[i] = t.transformExpr(r)
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

	// 1. 明示的型引数を持つジェネリクス適用: fn[int, string](...)
	if genExpr, ok := call.Function.(*ast.GenericInstExpr); ok {
		funcToken = genExpr.Token
		funcName = t.resolveTargetName(genExpr.Left)
		for _, tArg := range genExpr.TypeArgs {
			explicitTypeArgs = append(explicitTypeArgs, t.semaCtx.ResolveType(tArg))
		}
	} else if idxExpr, ok := call.Function.(*ast.IndexExpr); ok {
		// 単一型引数インデックス形式: Min[int](...)
		funcToken = idxExpr.Token
		funcName = t.resolveTargetName(idxExpr.Left)
		if tArg := t.semaCtx.ResolveType(idxExpr.Index.(ast.TypeExpr)); tArg != nil && tArg != sema.TypeVoid {
			explicitTypeArgs = append(explicitTypeArgs, tArg)
		}
	} else if id, ok := call.Function.(*ast.Identifier); ok {
		if genTemplate := t.findGenericTemplate(id.Value); genTemplate != nil {
			funcName = id.Value
			funcToken = id.Token
		}
	} else if mem, ok := call.Function.(*ast.MemberExpr); ok {
		if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
			pkgFnName := pkgId.Value + "_" + mem.Field.Value
			if genTemplate := t.findGenericTemplate(pkgFnName); genTemplate != nil {
				funcName = pkgFnName
				funcToken = mem.Field.Token
			}
		}
	}

	// 単相化と関数名の書き換え
	if funcName != "" {
		if genTemplate := t.findGenericTemplate(funcName); genTemplate != nil {
			var typeArgs []sema.Type
			if len(explicitTypeArgs) > 0 {
				typeArgs = explicitTypeArgs
			} else {
				// 引数からの型推論
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

	if tmpl, ok := t.semaCtx.GenericFuncs[name]; ok && tmpl != nil {
		return tmpl
	}
	for k, v := range t.semaCtx.GenericFuncs {
		if v != nil && (k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k)) {
			return v
		}
	}
	if fn, ok := t.semaCtx.Functions[name]; ok && fn != nil && fn.Template != nil && fn.IsGeneric() {
		return fn.Template
	}
	for k, fn := range t.semaCtx.Functions {
		if fn != nil && fn.Template != nil && fn.IsGeneric() {
			if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
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
					Token: node.Token,
					Name:  &ast.Identifier{Token: node.Token, Value: resolvedType.TypeName()},
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
