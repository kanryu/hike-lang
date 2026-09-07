package sema

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/token"
)

type Context struct {
	Structs        map[string]*StructType
	Interfaces     map[string]*InterfaceType
	Functions      map[string]*FuncType
	Globals        map[string]Type
	Constants      map[string]int64
	FloatConstants map[string]float64
	Aliases        map[string]Type
	GenericTypes   map[string]*ast.TypeDecl
	GenericFuncs   map[string]*ast.FuncDecl
	TypeParams     map[string]*TypeParamType
	typeIDs        map[string]int64
	nextTypeID     int64
	HasMapImport   bool
	Verbose        bool
}

func NewContext() *Context {
	ctx := &Context{
		Structs:        make(map[string]*StructType),
		Interfaces:     make(map[string]*InterfaceType),
		Functions:      make(map[string]*FuncType),
		Globals:        make(map[string]Type),
		Constants:      make(map[string]int64),
		FloatConstants: make(map[string]float64),
		Aliases:        make(map[string]Type),
		GenericTypes:   make(map[string]*ast.TypeDecl),
		GenericFuncs:   make(map[string]*ast.FuncDecl),
		TypeParams:     make(map[string]*TypeParamType),
		typeIDs:        make(map[string]int64),
		nextTypeID:     1,
		Verbose:        false,
	}
	ctx.typeIDs["int"] = 1
	ctx.typeIDs["byte"] = 2
	ctx.typeIDs["bool"] = 3
	ctx.typeIDs["string"] = 4
	ctx.typeIDs["float32"] = 5
	ctx.typeIDs["float64"] = 6
	ctx.typeIDs["cstring"] = 7
	ctx.nextTypeID = 8

	errorIface := &InterfaceType{
		Name:            "error",
		Specializations: make(map[string]*InterfaceType),
		Methods: []Method{
			{Name: "Error", ParamTypes: []Type{}, ReturnTypes: []Type{TypeString}},
		},
	}
	ctx.Interfaces["error"] = errorIface
	ctx.Aliases["error"] = errorIface

	return ctx
}

func (c *Context) log(msg string) {
	if c.Verbose {
		fmt.Printf("[SEMA] %s\n", msg)
	}
}

func (c *Context) GetTypeID(t Type) int64 {
	if t == nil {
		return 0
	}
	name := t.TypeName()
	if id, exists := c.typeIDs[name]; exists {
		return id
	}
	id := c.nextTypeID
	c.nextTypeID++
	c.typeIDs[name] = id
	return id
}

// -----------------------------------------------------------------------------
// シンボル検索 (Lookup)
// -----------------------------------------------------------------------------

func (c *Context) LookupStruct(name string) (*StructType, string) {
	if st, ok := c.Structs[name]; ok {
		return st, name
	}
	for k, v := range c.Structs {
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
			return v, k
		}
	}
	return nil, ""
}

func (c *Context) LookupInterface(name string) (*InterfaceType, string) {
	if iface, ok := c.Interfaces[name]; ok {
		return iface, name
	}
	for k, v := range c.Interfaces {
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
			return v, k
		}
	}
	return nil, ""
}

func (c *Context) LookupFunction(name string) (*FuncType, string) {
	if fn, ok := c.Functions[name]; ok {
		return fn, name
	}
	for k, v := range c.Functions {
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
			return v, k
		}
	}
	return nil, ""
}

func (c *Context) LookupAlias(name string) (Type, string) {
	if a, ok := c.Aliases[name]; ok {
		return a, name
	}
	for k, v := range c.Aliases {
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
			return v, k
		}
	}
	return nil, ""
}

func (c *Context) LookupConstant(name string) (int64, bool) {
	if val, ok := c.Constants[name]; ok {
		return val, true
	}
	for k, v := range c.Constants {
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
			return v, true
		}
	}
	return 0, false
}

func (c *Context) LookupFloatConstant(name string) (float64, bool) {
	if val, ok := c.FloatConstants[name]; ok {
		return val, true
	}
	for k, v := range c.FloatConstants {
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
			return v, true
		}
	}
	return 0.0, false
}

// -----------------------------------------------------------------------------
// 型解決 (Type Resolution)
// -----------------------------------------------------------------------------

func (c *Context) ResolveType(expr ast.TypeExpr) Type {
	if expr == nil {
		return TypeVoid
	}

	switch t := expr.(type) {
	case *ast.NamedType:
		name := t.Name.Value
		if t.Package != nil {
			name = t.Package.Value + "_" + t.Name.Value
		}

		if strings.HasPrefix(name, "*") {
			baseName := strings.TrimPrefix(name, "*")
			return &PointerType{Base: c.ResolveType(&ast.NamedType{Token: t.Token, Name: &ast.Identifier{Value: baseName}})}
		}
		if strings.HasPrefix(name, "[]") {
			elemName := strings.TrimPrefix(name, "[]")
			return &SliceType{Elem: c.ResolveType(&ast.NamedType{Token: t.Token, Name: &ast.Identifier{Value: elemName}})}
		}

		if tp, ok := c.TypeParams[name]; ok {
			return tp
		}
		if tp, ok := c.TypeParams[t.Name.Value]; ok && t.Package == nil {
			return tp
		}

		if builtinT, ok := LookupBuiltinType(name); ok {
			return builtinT
		}
		if name == "any" {
			return &InterfaceType{Name: "any", Specializations: make(map[string]*InterfaceType)}
		}
		if name == "error" {
			return c.Interfaces["error"]
		}

		if st, canonicalName := c.LookupStruct(name); st != nil {
			if st.IsGeneric() {
				if len(t.TypeArgs) == 0 && len(c.TypeParams) > 0 {
					return st
				}

				if len(t.TypeArgs) == 0 {
					panic(fmt.Sprintf("[Sema Error] line %d:%d: generic struct '%s' requires type arguments (e.g. %s[...])",
						t.Token.Line, t.Token.Col, name, name))
				}
				if len(t.TypeArgs) != len(st.TypeParams) {
					panic(fmt.Sprintf("[Sema Error] line %d:%d: generic struct '%s' expects %d type arguments, got %d",
						t.Token.Line, t.Token.Col, name, len(st.TypeParams), len(t.TypeArgs)))
				}

				resolvedArgs := make([]Type, len(t.TypeArgs))
				argNames := []string{}
				typeMap := make(map[string]Type)
				for i, arg := range t.TypeArgs {
					resolvedArg := c.ResolveType(arg)
					resolvedArgs[i] = resolvedArg
					argNames = append(argNames, strings.ReplaceAll(resolvedArg.TypeName(), "*", "Ptr"))
					typeMap[st.TypeParams[i]] = resolvedArg
				}

				specKey := strings.Join(argNames, "_")
				if existingSt, ok := st.Specializations[specKey]; ok {
					return existingSt
				}

				specializedName := fmt.Sprintf("%s__%s", canonicalName, specKey)
				if existingSt, ok := c.Structs[specializedName]; ok {
					st.Specializations[specKey] = existingSt
					return existingSt
				}

				newSt := &StructType{
					Name:            specializedName,
					TypeParams:      st.TypeParams,
					TypeArgs:        resolvedArgs,
					Fields:          []Field{},
					Template:        st.Template,
					IsSpecialized:   true,
					Specializations: make(map[string]*StructType),
				}
				c.Structs[specializedName] = newSt
				st.Specializations[specKey] = newSt

				if st.Template != nil {
					if stAst, ok := st.Template.Type.(*ast.StructType); ok {
						for _, f := range stAst.Fields {
							fType := c.ResolveTypeWithSubst(f.Type, typeMap)
							newSt.Fields = append(newSt.Fields, Field{
								Name:       f.Name.Value,
								Type:       fType,
								IsEmbedded: f.IsEmbedded,
							})
						}
					}
				}
				return newSt
			}

			if len(t.TypeArgs) > 0 {
				panic(fmt.Sprintf("[Sema Error] line %d:%d: non-generic struct '%s' cannot have type arguments",
					t.Token.Line, t.Token.Col, name))
			}
			return st
		}

		if iface, canonicalName := c.LookupInterface(name); iface != nil {
			if iface.IsGeneric() {
				if len(t.TypeArgs) == 0 && len(c.TypeParams) > 0 {
					return iface
				}

				if len(t.TypeArgs) == 0 {
					panic(fmt.Sprintf("[Sema Error] line %d:%d: generic interface '%s' requires type arguments",
						t.Token.Line, t.Token.Col, name))
				}
				if len(t.TypeArgs) != len(iface.TypeParams) {
					panic(fmt.Sprintf("[Sema Error] line %d:%d: generic interface '%s' expects %d type arguments, got %d",
						t.Token.Line, t.Token.Col, name, len(iface.TypeParams), len(t.TypeArgs)))
				}

				resolvedArgs := make([]Type, len(t.TypeArgs))
				argNames := []string{}
				typeMap := make(map[string]Type)
				for i, arg := range t.TypeArgs {
					resolvedArg := c.ResolveType(arg)
					resolvedArgs[i] = resolvedArg
					argNames = append(argNames, strings.ReplaceAll(resolvedArg.TypeName(), "*", "Ptr"))
					typeMap[iface.TypeParams[i]] = resolvedArg
				}

				specKey := strings.Join(argNames, "_")
				if existingIface, ok := iface.Specializations[specKey]; ok {
					return existingIface
				}

				specializedName := fmt.Sprintf("%s__%s", canonicalName, specKey)
				if existingIface, ok := c.Interfaces[specializedName]; ok {
					iface.Specializations[specKey] = existingIface
					return existingIface
				}

				newIface := &InterfaceType{
					Name:            specializedName,
					TypeParams:      iface.TypeParams,
					TypeArgs:        resolvedArgs,
					Methods:         []Method{},
					Template:        iface.Template,
					IsSpecialized:   true,
					Specializations: make(map[string]*InterfaceType),
				}
				c.Interfaces[specializedName] = newIface
				iface.Specializations[specKey] = newIface

				if iface.Template != nil {
					if itAst, ok := iface.Template.Type.(*ast.InterfaceType); ok {
						for _, m := range itAst.Methods {
							pts := []Type{}
							for _, p := range m.ParamTypes {
								pts = append(pts, c.ResolveTypeWithSubst(p, typeMap))
							}
							rts := []Type{}
							for _, r := range m.ReturnTypes {
								rts = append(rts, c.ResolveTypeWithSubst(r, typeMap))
							}
							newIface.Methods = append(newIface.Methods, Method{
								Name:        m.Name.Value,
								ParamTypes:  pts,
								ReturnTypes: rts,
							})
						}
					}
				}
				return newIface
			}

			if len(t.TypeArgs) > 0 {
				panic(fmt.Sprintf("[Sema Error] line %d:%d: non-generic interface '%s' cannot have type arguments",
					t.Token.Line, t.Token.Col, name))
			}
			return iface
		}

		if alias, _ := c.LookupAlias(name); alias != nil {
			return alias
		}

		panic(fmt.Sprintf("[Sema Error] line %d:%d: undefined type '%s'",
			t.Token.Line, t.Token.Col, name))

	case *ast.PointerType:
		return &PointerType{Base: c.ResolveType(t.Base)}
	case *ast.SliceType:
		return &SliceType{Elem: c.ResolveType(t.Elem)}
	case *ast.EllipsisType:
		// Go仕様の可変長パラメータ ...T は内部的にスライス []T として解決
		return &SliceType{Elem: c.ResolveType(t.Elem)}
	case *ast.ArrayType:
		return &ArrayType{Len: int(t.Len), Elem: c.ResolveType(t.Elem)}
	case *ast.MapType:
		return &MapType{Key: c.ResolveType(t.Key), Value: c.ResolveType(t.Value)}
	case *ast.ChanType:
		return &ChanType{Elem: c.ResolveType(t.Elem)}
	case *ast.FutureType:
		rts := make([]Type, len(t.ReturnTypes))
		for i, rt := range t.ReturnTypes {
			rts[i] = c.ResolveType(rt)
		}
		return &FutureType{ReturnTypes: rts}
	case *ast.InterfaceType:
		methods := []Method{}
		for _, m := range t.Methods {
			pts := []Type{}
			var varElem Type = nil
			for i, p := range m.ParamTypes {
				resolved := c.ResolveType(p)
				if m.IsVariadic && i == len(m.ParamTypes)-1 {
					if _, isSl := resolved.(*SliceType); !isSl {
						resolved = &SliceType{Elem: resolved}
					}
					varElem = resolved.(*SliceType).Elem
				}
				pts = append(pts, resolved)
			}
			rts := []Type{}
			for _, r := range m.ReturnTypes {
				rts = append(rts, c.ResolveType(r))
			}
			methods = append(methods, Method{
				Name:         m.Name.Value,
				ParamTypes:   pts,
				IsVariadic:   m.IsVariadic,
				VariadicElem: varElem,
				ReturnTypes:  rts,
			})
		}
		return &InterfaceType{Name: "", Methods: methods, Specializations: make(map[string]*InterfaceType)}
	case *ast.FuncType:
		fnType := &FuncType{
			ParamTypes:      []Type{},
			ReturnTypes:     []Type{},
			IsVariadic:      t.IsVariadic,
			Specializations: make(map[string]*FuncType),
		}
		for i, pt := range t.ParamTypes {
			resolved := c.ResolveType(pt)
			if t.IsVariadic && i == len(t.ParamTypes)-1 {
				if _, isSl := resolved.(*SliceType); !isSl {
					resolved = &SliceType{Elem: resolved}
				}
				fnType.VariadicElem = resolved.(*SliceType).Elem
			}
			fnType.ParamTypes = append(fnType.ParamTypes, resolved)
		}
		for _, rt := range t.ReturnTypes {
			fnType.ReturnTypes = append(fnType.ReturnTypes, c.ResolveType(rt))
		}
		return fnType
	}

	panic(fmt.Sprintf("[Sema Error] unknown type expression node %T", expr))
}

func (c *Context) ResolveTypeWithSubst(t ast.TypeExpr, subst map[string]Type) Type {
	if t == nil {
		return TypeVoid
	}
	switch node := t.(type) {
	case *ast.NamedType:
		if node.Package == nil && len(node.TypeArgs) == 0 {
			if replacement, ok := subst[node.Name.Value]; ok {
				return replacement
			}
		}

		name := node.Name.Value
		if node.Package != nil {
			name = node.Package.Value + "_" + node.Name.Value
		}

		if st, canonicalName := c.LookupStruct(name); st != nil && st.IsGeneric() {
			var argNames []string
			var resolvedArgs []Type
			typeMap := make(map[string]Type)

			if len(node.TypeArgs) > 0 {
				for i, arg := range node.TypeArgs {
					resolvedArg := c.ResolveTypeWithSubst(arg, subst)
					resolvedArgs = append(resolvedArgs, resolvedArg)
					argNames = append(argNames, strings.ReplaceAll(resolvedArg.TypeName(), "*", "Ptr"))
					if i < len(st.TypeParams) {
						typeMap[st.TypeParams[i]] = resolvedArg
					}
				}
			} else if len(subst) > 0 {
				for _, tp := range st.TypeParams {
					if resolvedArg, ok := subst[tp]; ok {
						resolvedArgs = append(resolvedArgs, resolvedArg)
						argNames = append(argNames, strings.ReplaceAll(resolvedArg.TypeName(), "*", "Ptr"))
						typeMap[tp] = resolvedArg
					}
				}
			}

			if len(argNames) == len(st.TypeParams) {
				specKey := strings.Join(argNames, "_")
				if existingSt, ok := st.Specializations[specKey]; ok {
					return existingSt
				}

				specializedName := fmt.Sprintf("%s__%s", canonicalName, specKey)
				if existingSt, ok := c.Structs[specializedName]; ok {
					st.Specializations[specKey] = existingSt
					return existingSt
				}

				newSt := &StructType{
					Name:            specializedName,
					TypeParams:      st.TypeParams,
					TypeArgs:        resolvedArgs,
					Fields:          []Field{},
					Template:        st.Template,
					IsSpecialized:   true,
					Specializations: make(map[string]*StructType),
				}
				c.Structs[specializedName] = newSt
				st.Specializations[specKey] = newSt

				if st.Template != nil {
					if stAst, ok := st.Template.Type.(*ast.StructType); ok {
						for _, f := range stAst.Fields {
							fType := c.ResolveTypeWithSubst(f.Type, typeMap)
							newSt.Fields = append(newSt.Fields, Field{
								Name:       f.Name.Value,
								Type:       fType,
								IsEmbedded: f.IsEmbedded,
							})
						}
					}
				}
				return newSt
			}
		}

		return c.ResolveType(node)
	case *ast.FuncType:
		fnType := &FuncType{
			ParamTypes:      []Type{},
			ReturnTypes:     []Type{},
			IsVariadic:      node.IsVariadic,
			Specializations: make(map[string]*FuncType),
		}
		for i, pt := range node.ParamTypes {
			resolved := c.ResolveTypeWithSubst(pt, subst)
			if node.IsVariadic && i == len(node.ParamTypes)-1 {
				if _, isSl := resolved.(*SliceType); !isSl {
					resolved = &SliceType{Elem: resolved}
				}
				fnType.VariadicElem = resolved.(*SliceType).Elem
			}
			fnType.ParamTypes = append(fnType.ParamTypes, resolved)
		}
		for _, rt := range node.ReturnTypes {
			fnType.ReturnTypes = append(fnType.ReturnTypes, c.ResolveTypeWithSubst(rt, subst))
		}
		return fnType
	case *ast.PointerType:
		return &PointerType{Base: c.ResolveTypeWithSubst(node.Base, subst)}
	case *ast.SliceType:
		return &SliceType{Elem: c.ResolveTypeWithSubst(node.Elem, subst)}
	case *ast.EllipsisType:
		return &SliceType{Elem: c.ResolveTypeWithSubst(node.Elem, subst)}
	case *ast.ArrayType:
		return &ArrayType{Len: int(node.Len), Elem: c.ResolveTypeWithSubst(node.Elem, subst)}
	case *ast.MapType:
		return &MapType{
			Key:   c.ResolveTypeWithSubst(node.Key, subst),
			Value: c.ResolveTypeWithSubst(node.Value, subst),
		}
	case *ast.ChanType:
		return &ChanType{Elem: c.ResolveTypeWithSubst(node.Elem, subst)}
	}
	return c.ResolveType(t)
}

func (c *Context) resolveTypeFromExpr(e ast.Expression) Type {
	if e == nil {
		return nil
	}
	if te, ok := e.(ast.TypeExpr); ok {
		return c.ResolveType(te)
	}
	if id, ok := e.(*ast.Identifier); ok {
		switch id.Value {
		case "int":
			return TypeInt
		case "int64":
			return TypeInt64
		case "int32":
			return TypeInt32
		case "int16":
			return TypeInt16
		case "int8":
			return TypeInt8
		case "uint":
			return TypeUint
		case "uint64":
			return TypeUint64
		case "uint32":
			return TypeUint32
		case "uint16":
			return TypeUint16
		case "uint8":
			return TypeUint8
		case "uintptr":
			return TypeUintptr
		case "byte":
			return TypeByte
		case "bool":
			return TypeBool
		case "float32":
			return TypeFloat32
		case "float64", "float":
			return TypeFloat64
		case "string":
			return TypeString
		case "cstring":
			return TypeCString
		case "void":
			return TypeVoid
		case "any":
			return &InterfaceType{Name: "any", Specializations: make(map[string]*InterfaceType)}
		}
		if st, _ := c.LookupStruct(id.Value); st != nil {
			return st
		}
		if iface, _ := c.LookupInterface(id.Value); iface != nil {
			return iface
		}
		if alias, _ := c.LookupAlias(id.Value); alias != nil {
			return alias
		}
	}
	if pref, ok := e.(*ast.PrefixExpr); ok && pref.Operator == "*" {
		base := c.resolveTypeFromExpr(pref.Right)
		if base != nil && base != TypeVoid {
			return &PointerType{Base: base}
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// 型推論 (Type Inference) & 暗黙キャスト
// -----------------------------------------------------------------------------

func (c *Context) InferExprType(expr ast.Expression, locals map[string]Type) Type {
	if expr == nil {
		return TypeVoid
	}

	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return TypeInt
	case *ast.FloatLiteral:
		return TypeFloat64
	case *ast.StringLiteral:
		return TypeString
	case *ast.NilLiteral:
		return &PointerType{Base: TypeByte}
	case *ast.Identifier:
		if e.Value == "..." {
			return TypeVoid
		}
		switch e.Value {
		case "true", "false":
			return TypeBool
		}
		if t, ok := locals[e.Value]; ok {
			return t
		}
		if t, ok := c.Globals[e.Value]; ok {
			return t
		}
		if _, ok := c.LookupConstant(e.Value); ok {
			return TypeInt
		}
		if _, ok := c.LookupFloatConstant(e.Value); ok {
			return TypeFloat64
		}
		if fn, ok := c.Functions[e.Value]; ok {
			return fn
		}
		return TypeInt

	case *ast.ImplicitCastExpr:
		return c.ResolveType(e.TargetType)

	case *ast.AsyncExpr:
		fnType := c.InferExprType(e.Fn, locals)
		if ft, ok := fnType.(*FuncType); ok {
			return &FutureType{ReturnTypes: ft.ReturnTypes}
		}
		return &FutureType{ReturnTypes: []Type{TypeVoid}}

	case *ast.ReceiveExpr:
		innerType := c.InferExprType(e.Expr, locals)
		if fut, ok := innerType.(*FutureType); ok {
			if len(fut.ReturnTypes) == 1 {
				return fut.ReturnTypes[0]
			} else if len(fut.ReturnTypes) > 1 {
				return &TupleType{Types: fut.ReturnTypes}
			}
			return TypeVoid
		}
		if ch, ok := innerType.(*ChanType); ok {
			return ch.Elem
		}
		return innerType

	case *ast.PrefixExpr:
		base := c.InferExprType(e.Right, locals)
		switch e.Operator {
		case "&":
			return &PointerType{Base: base}
		case "*":
			if pt, ok := base.(*PointerType); ok {
				return pt.Base
			}
			return TypeInt
		case "!":
			return TypeBool
		case "-", "^":
			return base
		}

	case *ast.BinaryExpr:
		switch e.Operator {
		case "==", "!=", "<", "<=", ">", ">=":
			return TypeBool
		case "&&", "||":
			return TypeBool
		case "+":
			lt := c.InferExprType(e.Left, locals)
			rt := c.InferExprType(e.Right, locals)
			if lt == TypeString || rt == TypeString {
				return TypeString
			}
			if lt == TypeFloat64 || rt == TypeFloat64 {
				return TypeFloat64
			}
			return lt
		default:
			lt := c.InferExprType(e.Left, locals)
			rt := c.InferExprType(e.Right, locals)
			if lt == TypeFloat64 || rt == TypeFloat64 {
				return TypeFloat64
			}
			return lt
		}

	case *ast.MemberExpr:
		if pkgId, okPkg := e.Object.(*ast.Identifier); okPkg {
			qualified := pkgId.Value + "_" + e.Field.Value
			if t, ok := c.Globals[qualified]; ok {
				return t
			}
			if _, ok := c.LookupConstant(qualified); ok {
				return TypeInt
			}
			if _, ok := c.LookupFloatConstant(qualified); ok {
				return TypeFloat64
			}
			if fn, ok := c.Functions[qualified]; ok {
				return fn
			}
		}
		objType := c.InferExprType(e.Object, locals)
		if pt, ok := objType.(*PointerType); ok {
			objType = pt.Base
		}
		if st, ok := objType.(*StructType); ok {
			for _, f := range st.Fields {
				if f.Name == e.Field.Value {
					return f.Type
				}
			}
		}

	case *ast.IndexExpr:
		lt := c.InferExprType(e.Left, locals)
		if t, err := c.ResolveIndexExprType(lt, e.Index); err == nil {
			return t
		}

	case *ast.SliceExpr:
		lt := c.InferExprType(e.Left, locals)
		if lt == TypeString {
			return TypeString
		}
		if sl, ok := lt.(*SliceType); ok {
			return sl
		}
		if ar, ok := lt.(*ArrayType); ok {
			return &SliceType{Elem: ar.Elem}
		}
		return lt

	case *ast.TypeAssertExpr:
		if e.Target != nil {
			return c.ResolveType(e.Target)
		}
		return &InterfaceType{Name: "any", Specializations: make(map[string]*InterfaceType)}

	case *ast.CallExpr:
		if len(e.Args) == 1 {
			if castT := c.resolveTypeFromExpr(e.Function); castT != nil && castT != TypeVoid {
				if _, isFn := castT.(*FuncType); !isFn {
					return castT
				}
			}
		}
		if id, ok := e.Function.(*ast.Identifier); ok {
			switch id.Value {
			case "len", "cap":
				return TypeInt
			case "string":
				return TypeString
			case "make":
				if len(e.Args) > 0 {
					return c.ResolveType(e.Args[0].(ast.TypeExpr))
				}
			case "append":
				if len(e.Args) > 0 {
					return c.InferExprType(e.Args[0], locals)
				}
			}
		}
		fnType := c.InferExprType(e.Function, locals)
		if ft, ok := fnType.(*FuncType); ok {
			if len(ft.ReturnTypes) == 1 {
				return ft.ReturnTypes[0]
			} else if len(ft.ReturnTypes) > 1 {
				return &TupleType{Types: ft.ReturnTypes}
			}
			return TypeVoid
		}

	case *ast.StructLiteral:
		return c.ResolveType(e.Type)

	case *ast.ArrayLiteral:
		return c.ResolveType(e.Type)

	case *ast.SliceLiteral:
		return c.ResolveType(e.Type)

	case *ast.FuncLit:
		ft := &FuncType{
			ParamTypes:      make([]Type, len(e.Params)),
			ReturnTypes:     make([]Type, len(e.ReturnTypes)),
			IsVariadic:      e.IsVariadic,
			Specializations: make(map[string]*FuncType),
		}
		var variadicElem Type = nil
		for i, p := range e.Params {
			pType := c.ResolveType(p.Type)
			if p.IsVariadic || (e.IsVariadic && i == len(e.Params)-1) {
				if _, isSlice := pType.(*SliceType); !isSlice {
					pType = &SliceType{Elem: pType}
				}
				variadicElem = pType.(*SliceType).Elem
			}
			ft.ParamTypes[i] = pType
		}
		for i, rt := range e.ReturnTypes {
			ft.ReturnTypes[i] = c.ResolveType(rt)
		}
		ft.VariadicElem = variadicElem
		return ft
	}

	return TypeInt
}

func (c *Context) CoerceExpr(expr ast.Expression, targetType Type, locals map[string]Type) ast.Expression {
	if expr == nil || targetType == nil {
		return expr
	}
	actualType := c.InferExprType(expr, locals)
	kind, needed := DetermineCast(actualType, targetType)
	if !needed {
		return expr
	}

	targetNode := typeToTypeExpr(targetType)

	return &ast.ImplicitCastExpr{
		Token: token.Token{
			Type:    token.IMPLICIT_CAST,
			Literal: "cast",
		},
		Expr:       expr,
		Kind:       kind,
		TargetType: targetNode,
	}
}

// -----------------------------------------------------------------------------
// 定数評価 (Constant Folding)
// -----------------------------------------------------------------------------

func (c *Context) evalConstInt(expr ast.Expression) (int64, bool) {
	if expr == nil {
		return 0, false
	}
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return e.Value, true
	case *ast.Identifier:
		if val, ok := c.LookupConstant(e.Value); ok {
			return val, true
		}
	case *ast.PrefixExpr:
		val, ok := c.evalConstInt(e.Right)
		if !ok {
			return 0, false
		}
		switch e.Operator {
		case "-":
			return -val, true
		case "+":
			return val, true
		case "^":
			return ^val, true
		}
	case *ast.BinaryExpr:
		l, okL := c.evalConstInt(e.Left)
		r, okR := c.evalConstInt(e.Right)
		if !okL || !okR {
			return 0, false
		}
		switch e.Operator {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			if r == 0 {
				return 0, false
			}
			return l / r, true
		case "%":
			if r == 0 {
				return 0, false
			}
			return l % r, true
		case "<<":
			return l << r, true
		case ">>":
			return l >> r, true
		case "&":
			return l & r, true
		case "|":
			return l | r, true
		case "^":
			return l ^ r, true
		}
	case *ast.CallExpr:
		if len(e.Args) == 1 {
			return c.evalConstInt(e.Args[0])
		}
	case *ast.MemberExpr:
		if pkgId, okPkg := e.Object.(*ast.Identifier); okPkg {
			qualified := pkgId.Value + "_" + e.Field.Value
			if val, ok := c.LookupConstant(qualified); ok {
				return val, true
			}
			if val, ok := c.LookupConstant(e.Field.Value); ok {
				return val, true
			}
		}
	}
	return 0, false
}

func (c *Context) evalConstFloat(expr ast.Expression) (float64, bool) {
	if expr == nil {
		return 0, false
	}
	switch e := expr.(type) {
	case *ast.FloatLiteral:
		return e.Value, true
	case *ast.IntegerLiteral:
		return float64(e.Value), true
	case *ast.Identifier:
		if val, ok := c.LookupFloatConstant(e.Value); ok {
			return val, true
		}
		if val, ok := c.LookupConstant(e.Value); ok {
			return float64(val), true
		}
	case *ast.PrefixExpr:
		val, ok := c.evalConstFloat(e.Right)
		if !ok {
			return 0, false
		}
		if e.Operator == "-" {
			return -val, true
		} else if e.Operator == "+" {
			return val, true
		}
	case *ast.BinaryExpr:
		l, okL := c.evalConstFloat(e.Left)
		r, okR := c.evalConstFloat(e.Right)
		if !okL || !okR {
			return 0, false
		}
		switch e.Operator {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			if r == 0 {
				return 0, false
			}
			return l / r, true
		}
	case *ast.CallExpr:
		if len(e.Args) == 1 {
			return c.evalConstFloat(e.Args[0])
		}
	}
	return 0, false
}

// -----------------------------------------------------------------------------
// マップビヘイビア・インデックス解決
// -----------------------------------------------------------------------------

func (c *Context) EnsureMapSupported(line, col int) error {
	if !c.HasMapImport {
		return fmt.Errorf("line %d:%d: map syntax requires importing 'std/maps'", line, col)
	}
	return nil
}

func (c *Context) CheckMapBehavior(t Type) (Type, Type, bool) {
	if t == nil {
		return nil, nil, false
	}

	typeName := t.TypeName()
	typeName = strings.TrimPrefix(typeName, "*")

	baseTypeName := typeName
	if idx := strings.Index(typeName, "__"); idx != -1 {
		baseTypeName = typeName[:idx]
	}

	st, _ := c.LookupStruct(typeName)
	if st == nil {
		st, _ = c.LookupStruct(baseTypeName)
	}
	if st == nil {
		return nil, nil, false
	}

	if len(st.TypeArgs) >= 2 {
		return st.TypeArgs[0], st.TypeArgs[1], true
	}

	setFn, _ := c.LookupFunction(typeName + "_Set")
	if setFn == nil {
		setFn, _ = c.LookupFunction(baseTypeName + "_Set")
	}
	getFn, _ := c.LookupFunction(typeName + "_Get")
	if getFn == nil {
		getFn, _ = c.LookupFunction(baseTypeName + "_Get")
	}
	delFn, _ := c.LookupFunction(typeName + "_Delete")
	if delFn == nil {
		delFn, _ = c.LookupFunction(baseTypeName + "_Delete")
	}
	lenFn, _ := c.LookupFunction(typeName + "_Len")
	if lenFn == nil {
		lenFn, _ = c.LookupFunction(baseTypeName + "_Len")
	}

	if setFn == nil || getFn == nil || delFn == nil || lenFn == nil {
		return nil, nil, false
	}

	keyIdx := 1
	valIdx := 2
	if !setFn.IsMethod && len(setFn.ParamTypes) == 2 {
		keyIdx = 0
		valIdx = 1
	}

	if len(setFn.ParamTypes) <= valIdx {
		return nil, nil, false
	}
	keyType := setFn.ParamTypes[keyIdx]
	valType := setFn.ParamTypes[valIdx]

	return keyType, valType, true
}

func (c *Context) ResolveIndexExprType(leftType Type, indexExpr ast.Expression) (Type, error) {
	if leftType == nil {
		return TypeVoid, fmt.Errorf("cannot index nil type")
	}

	if mp, ok := leftType.(*MapType); ok {
		return mp.Value, nil
	}

	if _, valType, ok := c.CheckMapBehavior(leftType); ok {
		return valType, nil
	}

	if sl, ok := leftType.(*SliceType); ok {
		return sl.Elem, nil
	}
	if ar, ok := leftType.(*ArrayType); ok {
		return ar.Elem, nil
	}
	if pt, ok := leftType.(*PointerType); ok {
		return pt.Base, nil
	}
	if leftType == TypeString {
		return TypeByte, nil
	}

	return TypeVoid, fmt.Errorf("type '%s' does not support indexing or MapBehavior interface", leftType.TypeName())
}
