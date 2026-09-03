package sema

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/token"
)

type Type interface {
	TypeName() string
	LLVMType() string
	Size() int
}

type BasicType struct {
	Name     string
	ByteSize int
	LLVM     string
}

func (t *BasicType) TypeName() string { return t.Name }
func (t *BasicType) LLVMType() string { return t.LLVM }
func (t *BasicType) Size() int        { return t.ByteSize }

var (
	TypeInt     = &BasicType{Name: "int", ByteSize: 8, LLVM: "i64"}
	TypeByte    = &BasicType{Name: "byte", ByteSize: 1, LLVM: "i8"}
	TypeBool    = &BasicType{Name: "bool", ByteSize: 1, LLVM: "i1"}
	TypeFloat32 = &BasicType{Name: "float32", ByteSize: 4, LLVM: "float"}
	TypeFloat64 = &BasicType{Name: "float64", ByteSize: 8, LLVM: "double"}
	TypeString  = &BasicType{Name: "string", ByteSize: 8, LLVM: "i8*"}
	TypeVoid    = &BasicType{Name: "void", ByteSize: 0, LLVM: "void"}
)

type TypeParamType struct {
	Name string
}

func (t *TypeParamType) TypeName() string { return t.Name }
func (t *TypeParamType) LLVMType() string { return "i8*" }
func (t *TypeParamType) Size() int        { return 8 }

type PointerType struct {
	Base Type
}

func (t *PointerType) TypeName() string { return "*" + t.Base.TypeName() }
func (t *PointerType) LLVMType() string { return t.Base.LLVMType() + "*" }
func (t *PointerType) Size() int        { return 8 }

type SliceType struct {
	Elem Type
}

func (t *SliceType) TypeName() string { return "[]" + t.Elem.TypeName() }
func (t *SliceType) LLVMType() string { return "{ i8*, i64, i64 }" }
func (t *SliceType) Size() int        { return 24 }

type ArrayType struct {
	Len  int
	Elem Type
}

func (t *ArrayType) TypeName() string { return fmt.Sprintf("[%d]%s", t.Len, t.Elem.TypeName()) }
func (t *ArrayType) LLVMType() string { return fmt.Sprintf("[%d x %s]", t.Len, t.Elem.LLVMType()) }
func (t *ArrayType) Size() int        { return t.Len * t.Elem.Size() }

type Field struct {
	Name       string
	Type       Type
	IsEmbedded bool
}

type StructType struct {
	Name            string
	TypeParams      []string
	TypeArgs        []Type
	Fields          []Field
	Template        *ast.TypeDecl
	IsSpecialized   bool
	Specializations map[string]*StructType
}

func (t *StructType) TypeName() string { return t.Name }
func (t *StructType) LLVMType() string { return "%struct." + t.Name }
func (t *StructType) IsGeneric() bool  { return len(t.TypeParams) > 0 && !t.IsSpecialized }
func (t *StructType) Size() int {
	sz := 0
	for _, f := range t.Fields {
		fsz := f.Type.Size()
		if fsz <= 0 {
			fsz = 8
		}
		sz += fsz
	}
	if sz == 0 {
		return 8
	}
	return sz
}

type Method struct {
	Name        string
	ParamTypes  []Type
	ReturnTypes []Type
}

type InterfaceType struct {
	Name            string
	TypeParams      []string
	TypeArgs        []Type
	Methods         []Method
	Template        *ast.TypeDecl
	IsSpecialized   bool
	Specializations map[string]*InterfaceType
}

func (t *InterfaceType) TypeName() string {
	if t.Name != "" {
		return t.Name
	}
	return "interface"
}

func (t *InterfaceType) LLVMType() string {
	if t.IsAny() {
		return "{ i8*, i64 }"
	}
	return "{ i8*, i8* }"
}

func (t *InterfaceType) Size() int       { return 16 }
func (t *InterfaceType) IsAny() bool     { return len(t.Methods) == 0 }
func (t *InterfaceType) IsGeneric() bool { return len(t.TypeParams) > 0 && !t.IsSpecialized }

type FuncType struct {
	Name            string
	TypeParams      []string
	TypeArgs        []Type
	IsMethod        bool
	ParamTypes      []Type
	ReturnTypes     []Type
	IsVariadic      bool
	IsExtern        bool
	Template        *ast.FuncDecl
	IsSpecialized   bool
	SpecializedAst  *ast.FuncDecl
	Emitted         bool
	Specializations map[string]*FuncType
}

func (t *FuncType) TypeName() string { return "func" }
func (t *FuncType) LLVMType() string { return "{ i8*, i8* }" }
func (t *FuncType) Size() int        { return 16 }
func (t *FuncType) IsGeneric() bool  { return len(t.TypeParams) > 0 && !t.IsSpecialized }

type TupleType struct {
	Types []Type
}

func (t *TupleType) TypeName() string { return "tuple" }
func (t *TupleType) LLVMType() string {
	types := []string{}
	for _, el := range t.Types {
		types = append(types, el.LLVMType())
	}
	return fmt.Sprintf("{ %s }", strings.Join(types, ", "))
}
func (t *TupleType) Size() int {
	sz := 0
	for _, el := range t.Types {
		sz += el.Size()
	}
	return sz
}

type MapType struct {
	Key   Type
	Value Type
}

func (t *MapType) TypeName() string {
	return fmt.Sprintf("map[%s]%s", t.Key.TypeName(), t.Value.TypeName())
}
func (t *MapType) LLVMType() string {
	return "%struct.__hike_map*"
}
func (t *MapType) Size() int {
	return 8
}

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
	ctx.nextTypeID = 7

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

func getBaseTypeName(t ast.TypeExpr) string {
	if t == nil {
		return ""
	}
	switch node := t.(type) {
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

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func collectTypeParamsFromNode(t ast.TypeExpr, out map[string]bool) {
	if t == nil {
		return
	}
	switch node := t.(type) {
	case *ast.PointerType:
		collectTypeParamsFromNode(node.Base, out)
	case *ast.SliceType:
		collectTypeParamsFromNode(node.Elem, out)
	case *ast.ArrayType:
		collectTypeParamsFromNode(node.Elem, out)
	case *ast.MapType:
		collectTypeParamsFromNode(node.Key, out)
		collectTypeParamsFromNode(node.Value, out)
	case *ast.NamedType:
		for _, ta := range node.TypeArgs {
			collectTypeParamsFromNode(ta, out)
		}
		name := node.Name.Value
		switch name {
		case "int", "byte", "bool", "float32", "float64", "float", "string", "void", "any", "error":
			return
		}
		if len(name) <= 2 && node.Package == nil {
			out[name] = true
		}
	}
}

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

		// プレフィックスによる安全網の展開
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

		switch name {
		case "int":
			return TypeInt
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
		case "void":
			return TypeVoid
		case "any":
			return &InterfaceType{Name: "any", Specializations: make(map[string]*InterfaceType)}
		case "error":
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

		if alias, ok := c.Aliases[name]; ok {
			return alias
		}

		panic(fmt.Sprintf("[Sema Error] line %d:%d: undefined type '%s'",
			t.Token.Line, t.Token.Col, name))

	case *ast.PointerType:
		return &PointerType{Base: c.ResolveType(t.Base)}
	case *ast.SliceType:
		return &SliceType{Elem: c.ResolveType(t.Elem)}
	case *ast.ArrayType:
		return &ArrayType{Len: int(t.Len), Elem: c.ResolveType(t.Elem)}
	case *ast.MapType:
		return &MapType{Key: c.ResolveType(t.Key), Value: c.ResolveType(t.Value)}
	case *ast.InterfaceType:
		methods := []Method{}
		for _, m := range t.Methods {
			pts := []Type{}
			for _, p := range m.ParamTypes {
				pts = append(pts, c.ResolveType(p))
			}
			rts := []Type{}
			for _, r := range m.ReturnTypes {
				rts = append(rts, c.ResolveType(r))
			}
			methods = append(methods, Method{Name: m.Name.Value, ParamTypes: pts, ReturnTypes: rts})
		}
		return &InterfaceType{Name: "", Methods: methods, Specializations: make(map[string]*InterfaceType)}
	case *ast.FuncType:
		fnType := &FuncType{
			ParamTypes:      []Type{},
			ReturnTypes:     []Type{},
			Specializations: make(map[string]*FuncType),
		}
		for _, pt := range t.ParamTypes {
			fnType.ParamTypes = append(fnType.ParamTypes, c.ResolveType(pt))
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
			Specializations: make(map[string]*FuncType),
		}
		for _, pt := range node.ParamTypes {
			fnType.ParamTypes = append(fnType.ParamTypes, c.ResolveTypeWithSubst(pt, subst))
		}
		for _, rt := range node.ReturnTypes {
			fnType.ReturnTypes = append(fnType.ReturnTypes, c.ResolveTypeWithSubst(rt, subst))
		}
		return fnType
	case *ast.PointerType:
		return &PointerType{Base: c.ResolveTypeWithSubst(node.Base, subst)}
	case *ast.SliceType:
		return &SliceType{Elem: c.ResolveTypeWithSubst(node.Elem, subst)}
	case *ast.ArrayType:
		return &ArrayType{Len: int(node.Len), Elem: c.ResolveTypeWithSubst(node.Elem, subst)}
	case *ast.MapType:
		return &MapType{
			Key:   c.ResolveTypeWithSubst(node.Key, subst),
			Value: c.ResolveTypeWithSubst(node.Value, subst),
		}
	}
	return c.ResolveType(t)
}

func DetermineCast(from, to Type) (ast.CastKind, bool) {
	if from == nil || to == nil {
		return 0, false
	}
	if from == to || from.TypeName() == to.TypeName() {
		return 0, false
	}

	if targetIface, isIface := to.(*InterfaceType); isIface {
		if _, srcIsIface := from.(*InterfaceType); !srcIsIface {
			return ast.CastBoxInterface, true
		}
		if targetIface.IsAny() {
			return ast.CastBoxInterface, true
		}
	}

	fromLLVM := from.LLVMType()
	toLLVM := to.LLVMType()
	if fromLLVM == toLLVM {
		return 0, false
	}

	if (fromLLVM == "double" || fromLLVM == "float") && (toLLVM == "i64" || toLLVM == "i32") {
		return ast.CastFloatToInt, true
	}
	if (fromLLVM == "i64" || fromLLVM == "i32") && (toLLVM == "double" || toLLVM == "float") {
		return ast.CastIntToFloat, true
	}
	if fromLLVM == "i64" && (toLLVM == "i32" || toLLVM == "i8" || toLLVM == "i1") {
		return ast.CastTrunc, true
	}
	if (fromLLVM == "i32" || fromLLVM == "i8" || fromLLVM == "i1") && toLLVM == "i64" {
		return ast.CastZExt, true
	}
	if strings.HasSuffix(fromLLVM, "*") && strings.HasSuffix(toLLVM, "*") {
		return ast.CastBitcast, true
	}
	if strings.HasSuffix(fromLLVM, "*") && toLLVM == "i64" {
		return ast.CastPtrToInt, true
	}
	if fromLLVM == "i64" && strings.HasSuffix(toLLVM, "*") {
		return ast.CastIntToPtr, true
	}

	return 0, false
}

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
		if _, ok := c.Constants[e.Value]; ok {
			return TypeInt
		}
		if _, ok := c.FloatConstants[e.Value]; ok {
			return TypeFloat64
		}
		if fn, ok := c.Functions[e.Value]; ok {
			return fn
		}
		return TypeInt

	case *ast.ImplicitCastExpr:
		return c.ResolveType(e.TargetType)

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
			if _, ok := c.Constants[qualified]; ok {
				return TypeInt
			}
			if _, ok := c.FloatConstants[qualified]; ok {
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
		for i, p := range e.Params {
			ft.ParamTypes[i] = c.ResolveType(p.Type)
		}
		for i, rt := range e.ReturnTypes {
			ft.ReturnTypes[i] = c.ResolveType(rt)
		}
		return ft
	}

	return TypeInt
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
	}
	if pref, ok := e.(*ast.PrefixExpr); ok && pref.Operator == "*" {
		base := c.resolveTypeFromExpr(pref.Right)
		if base != nil && base != TypeVoid {
			return &PointerType{Base: base}
		}
	}
	return nil
}

func typeToTypeExpr(t Type) ast.TypeExpr {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case *PointerType:
		return &ast.PointerType{
			Base: typeToTypeExpr(v.Base),
		}
	case *SliceType:
		return &ast.SliceType{
			Elem: typeToTypeExpr(v.Elem),
		}
	case *ArrayType:
		return &ast.ArrayType{
			Len:  int64(v.Len),
			Elem: typeToTypeExpr(v.Elem),
		}
	case *MapType:
		return &ast.MapType{
			Key:   typeToTypeExpr(v.Key),
			Value: typeToTypeExpr(v.Value),
		}
	default:
		return &ast.NamedType{
			Name: &ast.Identifier{Value: v.TypeName()},
		}
	}
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

func Analyze(prog *ast.Program) (*Context, error) {
	ctx := NewContext()

	for _, imp := range prog.Imports {
		if imp.Path == "std/map" || imp.Path == "map" || imp.Path == "std/maps" || imp.Path == "maps" {
			ctx.HasMapImport = true
		}
	}

	for _, decl := range prog.Decls {
		if err := validateMapUsage(decl, ctx); err != nil {
			return nil, err
		}
	}

	// Pass 1: 全ての型宣言と関数宣言を登録
	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			tpSet := make(map[string]bool)
			for _, tp := range td.TypeParams {
				tpSet[tp.Name.Value] = true
			}

			if it, ok := td.Type.(*ast.InterfaceType); ok {
				for _, m := range it.Methods {
					for _, p := range m.ParamTypes {
						collectTypeParamsFromNode(p, tpSet)
					}
					for _, r := range m.ReturnTypes {
						collectTypeParamsFromNode(r, tpSet)
					}
				}
			} else if st, ok := td.Type.(*ast.StructType); ok {
				for _, f := range st.Fields {
					collectTypeParamsFromNode(f.Type, tpSet)
				}
			}

			tParams := []string{}
			for _, tp := range td.TypeParams {
				tParams = append(tParams, tp.Name.Value)
			}
			if len(tParams) == 0 {
				for tp := range tpSet {
					tParams = append(tParams, tp)
				}
			}

			if _, ok := td.Type.(*ast.InterfaceType); ok {
				iface := &InterfaceType{
					Name:            td.Name.Value,
					TypeParams:      tParams,
					Methods:         []Method{},
					Template:        td,
					Specializations: make(map[string]*InterfaceType),
				}
				ctx.Interfaces[td.Name.Value] = iface
				ctx.Aliases[td.Name.Value] = iface
				if len(tParams) > 0 {
					ctx.GenericTypes[td.Name.Value] = td
				}
			} else if _, ok := td.Type.(*ast.StructType); ok {
				structType := &StructType{
					Name:            td.Name.Value,
					TypeParams:      tParams,
					Fields:          []Field{},
					Template:        td,
					Specializations: make(map[string]*StructType),
				}
				ctx.Structs[td.Name.Value] = structType
				ctx.Aliases[td.Name.Value] = structType
				if len(tParams) > 0 {
					ctx.GenericTypes[td.Name.Value] = td
				}
			}
		} else if fd, ok := decl.(*ast.FuncDecl); ok {
			fnName := fd.Name.Value
			isMethod := (fd.Receiver != nil)
			tpSet := make(map[string]bool)
			for _, tp := range fd.TypeParams {
				tpSet[tp.Name.Value] = true
			}

			if fd.Receiver != nil {
				collectTypeParamsFromNode(fd.Receiver.Type, tpSet)
				t := fd.Receiver.Type
				if pt, ok := t.(*ast.PointerType); ok {
					t = pt.Base
				}
				if nt, ok := t.(*ast.NamedType); ok {
					recvName := nt.Name.Value
					if nt.Package != nil {
						recvName = nt.Package.Value + "_" + nt.Name.Value
					}
					if !strings.Contains(fnName, recvName) {
						fnName = recvName + "_" + fnName
					}
				}
			}
			for _, p := range fd.Params {
				collectTypeParamsFromNode(p.Type, tpSet)
			}
			for _, r := range fd.ReturnTypes {
				collectTypeParamsFromNode(r, tpSet)
			}

			tParams := []string{}
			for _, tp := range fd.TypeParams {
				tParams = append(tParams, tp.Name.Value)
			}
			if len(tParams) == 0 && fd.Receiver != nil {
				recvTypeName := getBaseTypeName(fd.Receiver.Type)
				if st, _ := ctx.LookupStruct(recvTypeName); st != nil && len(st.TypeParams) > 0 {
					tParams = append(tParams, st.TypeParams...)
				}
			}
			if len(tParams) == 0 {
				for tp := range tpSet {
					tParams = append(tParams, tp)
				}
			}

			fnType := &FuncType{
				Name:            fnName,
				TypeParams:      tParams,
				IsMethod:        isMethod,
				ParamTypes:      []Type{},
				ReturnTypes:     []Type{},
				IsVariadic:      fd.IsVariadic,
				IsExtern:        (fd.Body == nil),
				Template:        fd,
				Specializations: make(map[string]*FuncType),
			}
			ctx.Functions[fnName] = fnType
			if len(tParams) > 0 {
				ctx.GenericFuncs[fnName] = fd
				ctx.GenericFuncs[fd.Name.Value] = fd
			}
		}
	}

	// Pass 1.1: 不動点反復による型パラメータ伝播
	changed := true
	for changed {
		changed = false
		for _, st := range ctx.Structs {
			if st.Template != nil {
				if stAst, ok := st.Template.Type.(*ast.StructType); ok {
					for _, f := range stAst.Fields {
						typeName := getBaseTypeName(f.Type)
						if targetSt, _ := ctx.LookupStruct(typeName); targetSt != nil && targetSt.IsGeneric() {
							for _, tp := range targetSt.TypeParams {
								if !contains(st.TypeParams, tp) {
									st.TypeParams = append(st.TypeParams, tp)
									ctx.GenericTypes[st.Name] = st.Template
									changed = true
								}
							}
						}
					}
				}
			}
		}

		for _, fn := range ctx.Functions {
			var recvTypeName string = ""
			if fn.Template != nil {
				if fn.Template.Receiver != nil {
					recvTypeName = getBaseTypeName(fn.Template.Receiver.Type)
				} else if len(fn.Template.Params) > 0 {
					recvTypeName = getBaseTypeName(fn.Template.Params[0].Type)
				}
			}

			if recvTypeName == "" && strings.Contains(fn.Name, "_") {
				parts := strings.Split(fn.Name, "_")
				if len(parts) >= 2 {
					possibleStruct := strings.Join(parts[:len(parts)-1], "_")
					if st, _ := ctx.LookupStruct(possibleStruct); st != nil {
						recvTypeName = possibleStruct
					}
				}
			}

			if recvTypeName != "" {
				if st, _ := ctx.LookupStruct(recvTypeName); st != nil {
					for _, tp := range fn.TypeParams {
						if !contains(st.TypeParams, tp) {
							st.TypeParams = append(st.TypeParams, tp)
							ctx.GenericTypes[st.Name] = st.Template
							changed = true
						}
					}
					for _, tp := range st.TypeParams {
						if !contains(fn.TypeParams, tp) {
							fn.TypeParams = append(fn.TypeParams, tp)
							ctx.GenericFuncs[fn.Name] = fn.Template
							changed = true
						}
					}
				}
			}
		}

		for _, fn := range ctx.Functions {
			if fn.Template != nil {
				for _, p := range fn.Template.Params {
					pTypeName := getBaseTypeName(p.Type)
					if targetSt, _ := ctx.LookupStruct(pTypeName); targetSt != nil && targetSt.IsGeneric() {
						for _, tp := range targetSt.TypeParams {
							if !contains(fn.TypeParams, tp) {
								fn.TypeParams = append(fn.TypeParams, tp)
								ctx.GenericFuncs[fn.Name] = fn.Template
								changed = true
							}
						}
					}
				}
				for _, rt := range fn.Template.ReturnTypes {
					rTypeName := getBaseTypeName(rt)
					if targetSt, _ := ctx.LookupStruct(rTypeName); targetSt != nil && targetSt.IsGeneric() {
						for _, tp := range targetSt.TypeParams {
							if !contains(fn.TypeParams, tp) {
								fn.TypeParams = append(fn.TypeParams, tp)
								ctx.GenericFuncs[fn.Name] = fn.Template
								changed = true
							}
						}
					}
				}
			}
		}
	}

	// Pass 1.5: 具象型のみ先行解決
	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if st, _ := ctx.LookupStruct(td.Name.Value); st != nil && st.IsGeneric() {
				continue
			}
			if iface, _ := ctx.LookupInterface(td.Name.Value); iface != nil && iface.IsGeneric() {
				continue
			}

			if it, ok := td.Type.(*ast.InterfaceType); ok {
				methods := []Method{}
				for _, m := range it.Methods {
					pts := []Type{}
					for _, p := range m.ParamTypes {
						pts = append(pts, ctx.ResolveType(p))
					}
					rts := []Type{}
					for _, r := range m.ReturnTypes {
						rts = append(rts, ctx.ResolveType(r))
					}
					methods = append(methods, Method{Name: m.Name.Value, ParamTypes: pts, ReturnTypes: rts})
				}
				ctx.Interfaces[td.Name.Value].Methods = methods
			} else if st, ok := td.Type.(*ast.StructType); ok {
				fields := []Field{}
				for _, f := range st.Fields {
					fields = append(fields, Field{
						Name:       f.Name.Value,
						Type:       ctx.ResolveType(f.Type),
						IsEmbedded: f.IsEmbedded,
					})
				}
				ctx.Structs[td.Name.Value].Fields = fields
			} else {
				ctx.Aliases[td.Name.Value] = ctx.ResolveType(td.Type)
			}
		}
	}

	// Pass 2: 定数、グローバル変数、非ジェネリック関数の確定
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.ConstDecl:
			if il, ok := d.Value.(*ast.IntegerLiteral); ok {
				ctx.Constants[d.Name.Value] = il.Value
			} else if fl, ok := d.Value.(*ast.FloatLiteral); ok {
				ctx.FloatConstants[d.Name.Value] = fl.Value
			} else if pe, ok := d.Value.(*ast.PrefixExpr); ok && pe.Operator == "-" {
				if il, ok := pe.Right.(*ast.IntegerLiteral); ok {
					ctx.Constants[d.Name.Value] = -il.Value
				} else if fl, ok := pe.Right.(*ast.FloatLiteral); ok {
					ctx.FloatConstants[d.Name.Value] = -fl.Value
				}
			}

		case *ast.VarDecl:
			var gType Type = TypeInt
			if d.Type != nil {
				gType = ctx.ResolveType(d.Type)
			}
			ctx.Globals[d.Name.Value] = gType

		case *ast.FuncDecl:
			fnName := d.Name.Value
			if d.Receiver != nil {
				recvTypeName := ""
				t := d.Receiver.Type
				if pt, ok := t.(*ast.PointerType); ok {
					t = pt.Base
				}
				if nt, ok := t.(*ast.NamedType); ok {
					recvTypeName = nt.Name.Value
					if nt.Package != nil {
						recvTypeName = nt.Package.Value + "_" + nt.Name.Value
					}
				}
				if recvTypeName != "" && !strings.Contains(fnName, recvTypeName) {
					fnName = recvTypeName + "_" + fnName
				}
			}

			fnType := ctx.Functions[fnName]
			if fnType == nil {
				fnType = ctx.Functions[d.Name.Value]
			}
			if fnType == nil || fnType.IsGeneric() || IsGenericFuncDecl(d) {
				continue
			}

			isMethod := (d.Receiver != nil)
			paramTypes := []Type{}

			if d.Receiver != nil {
				recvType := ctx.ResolveType(d.Receiver.Type)
				paramTypes = append(paramTypes, recvType)
			}

			for _, p := range d.Params {
				paramTypes = append(paramTypes, ctx.ResolveType(p.Type))
			}

			returnTypes := []Type{}
			for _, rt := range d.ReturnTypes {
				returnTypes = append(returnTypes, ctx.ResolveType(rt))
			}

			fnType.IsMethod = isMethod
			fnType.ParamTypes = paramTypes
			fnType.ReturnTypes = returnTypes
		}
	}

	// Pass 3: エスケープ解析
	runEscapeAnalysis(prog)

	// Pass 4: 暗黙キャスト挿入
	insertImplicitCasts(prog, ctx)

	return ctx, nil
}

func runEscapeAnalysis(prog *ast.Program) {
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			varDecls := make(map[string]*ast.VarDecl)
			paramDecls := make(map[string]*ast.ParamDecl)

			if fn.Receiver != nil {
				paramDecls[fn.Receiver.Name.Value] = fn.Receiver
			}
			for _, p := range fn.Params {
				paramDecls[p.Name.Value] = p
			}

			collectDeclsInBlock(fn.Body, varDecls)
			capturedNames := CollectAllCapturesInBlock(fn.Body)

			for name := range capturedNames {
				if vd, ok := varDecls[name]; ok {
					vd.IsEscaped = true
				}
				if pd, ok := paramDecls[name]; ok {
					pd.IsEscaped = true
				}
			}
		}
	}
}

func collectDeclsInBlock(b *ast.BlockStmt, out map[string]*ast.VarDecl) {
	if b == nil {
		return
	}
	for _, stmt := range b.Statements {
		collectDeclsInStmt(stmt, out)
	}
}

func collectDeclsInStmt(stmt ast.Statement, out map[string]*ast.VarDecl) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.VarDecl:
		out[s.Name.Value] = s
	case *ast.BlockStmt:
		collectDeclsInBlock(s, out)
	case *ast.IfStmt:
		if s.Init != nil {
			collectDeclsInStmt(s.Init, out)
		}
		if s.Consequence != nil {
			collectDeclsInBlock(s.Consequence, out)
		}
		if s.Alternative != nil {
			collectDeclsInStmt(s.Alternative, out)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			collectDeclsInStmt(s.Init, out)
		}
		if s.Body != nil {
			collectDeclsInBlock(s.Body, out)
		}
	case *ast.ForRangeStmt:
		if s.Body != nil {
			collectDeclsInBlock(s.Body, out)
		}
	case *ast.SwitchStmt:
		if s.Init != nil {
			collectDeclsInStmt(s.Init, out)
		}
		for _, cc := range s.Cases {
			for _, bs := range cc.Body {
				collectDeclsInStmt(bs, out)
			}
		}
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			collectDeclsInStmt(s.Init, out)
		}
		for _, cc := range s.Cases {
			for _, bs := range cc.Body {
				collectDeclsInStmt(bs, out)
			}
		}
	}
}

func CollectAllCapturesInBlock(b *ast.BlockStmt) map[string]bool {
	capturedSet := make(map[string]bool)
	if b == nil {
		return capturedSet
	}

	var walkExpr func(e ast.Expression)
	var walkStmt func(s ast.Statement)

	walkExpr = func(e ast.Expression) {
		if e == nil {
			return
		}
		if fl, ok := e.(*ast.FuncLit); ok {
			caps := ScanCapturesFromLit(fl)
			for _, c := range caps {
				capturedSet[c] = true
			}
			if fl.Body != nil {
				walkStmt(fl.Body)
			}
			return
		}
		switch n := e.(type) {
		case *ast.BinaryExpr:
			walkExpr(n.Left)
			walkExpr(n.Right)
		case *ast.PrefixExpr:
			walkExpr(n.Right)
		case *ast.CallExpr:
			walkExpr(n.Function)
			for _, arg := range n.Args {
				walkExpr(arg)
			}
		case *ast.MemberExpr:
			walkExpr(n.Object)
		case *ast.IndexExpr:
			walkExpr(n.Left)
			walkExpr(n.Index)
		case *ast.SliceExpr:
			walkExpr(n.Left)
			walkExpr(n.Low)
			walkExpr(n.High)
		case *ast.TypeAssertExpr:
			walkExpr(n.Expr)
		case *ast.ArrayLiteral:
			for _, el := range n.Elements {
				walkExpr(el)
			}
		case *ast.SliceLiteral:
			for _, el := range n.Elements {
				walkExpr(el)
			}
		case *ast.StructLiteral:
			for _, sf := range n.Fields {
				walkExpr(sf.Value)
			}
		}
	}

	walkStmt = func(s ast.Statement) {
		if s == nil {
			return
		}
		switch st := s.(type) {
		case *ast.BlockStmt:
			for _, inner := range st.Statements {
				walkStmt(inner)
			}
		case *ast.ExprStmt:
			walkExpr(st.Expr)
		case *ast.ReturnStmt:
			for _, v := range st.Values {
				walkExpr(v)
			}
		case *ast.DeferStmt:
			if st.Call != nil {
				walkExpr(st.Call)
			}
		case *ast.AssignStmt:
			for _, l := range st.Left {
				walkExpr(l)
			}
			for _, r := range st.Right {
				walkExpr(r)
			}
		case *ast.VarDecl:
			if st.Value != nil {
				walkExpr(st.Value)
			}
		case *ast.IfStmt:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Condition)
			walkStmt(st.Consequence)
			if st.Alternative != nil {
				walkStmt(st.Alternative)
			}
		case *ast.ForStmt:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Cond)
			if st.Post != nil {
				walkStmt(st.Post)
			}
			walkStmt(st.Body)
		case *ast.ForRangeStmt:
			walkExpr(st.X)
			walkStmt(st.Body)
		case *ast.SwitchStmt:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Value)
			for _, cc := range st.Cases {
				for _, v := range cc.Values {
					walkExpr(v)
				}
				for _, bs := range cc.Body {
					walkStmt(bs)
				}
			}
		case *ast.TypeSwitchStmt:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Expr)
			for _, cc := range st.Cases {
				for _, bs := range cc.Body {
					walkStmt(bs)
				}
			}
		}
	}

	for _, s := range b.Statements {
		walkStmt(s)
	}
	return capturedSet
}

func ScanCapturesFromLit(fl *ast.FuncLit) []string {
	params := make(map[string]bool)
	for _, p := range fl.Params {
		params[p.Name.Value] = true
	}

	locals := make(map[string]bool)
	captured := []string{}
	seen := make(map[string]bool)

	var walkStmt func(s ast.Statement)
	var walkExpr func(e ast.Expression)

	walkExpr = func(e ast.Expression) {
		if e == nil {
			return
		}
		switch node := e.(type) {
		case *ast.Identifier:
			name := node.Value
			if !params[name] && !locals[name] && !seen[name] {
				switch name {
				case "true", "false", "nil", "len", "cap", "append", "delete", "make",
					"int", "byte", "string", "bool", "float32", "float64", "void", "any", "error":
					return
				}
				seen[name] = true
				captured = append(captured, name)
			}
		case *ast.BinaryExpr:
			walkExpr(node.Left)
			walkExpr(node.Right)
		case *ast.PrefixExpr:
			walkExpr(node.Right)
		case *ast.CallExpr:
			walkExpr(node.Function)
			for _, arg := range node.Args {
				walkExpr(arg)
			}
		case *ast.MemberExpr:
			walkExpr(node.Object)
		case *ast.IndexExpr:
			walkExpr(node.Left)
			walkExpr(node.Index)
		case *ast.SliceExpr:
			walkExpr(node.Left)
			walkExpr(node.Low)
			walkExpr(node.High)
		case *ast.TypeAssertExpr:
			walkExpr(node.Expr)
		case *ast.ArrayLiteral:
			for _, el := range node.Elements {
				walkExpr(el)
			}
		case *ast.SliceLiteral:
			for _, el := range node.Elements {
				walkExpr(el)
			}
		case *ast.StructLiteral:
			for _, sf := range node.Fields {
				walkExpr(sf.Value)
			}
		case *ast.FuncLit:
			if node.Body != nil {
				for _, s := range node.Body.Statements {
					walkStmt(s)
				}
			}
		}
	}

	walkStmt = func(s ast.Statement) {
		if s == nil {
			return
		}
		switch st := s.(type) {
		case *ast.BlockStmt:
			for _, inner := range st.Statements {
				walkStmt(inner)
			}
		case *ast.ExprStmt:
			walkExpr(st.Expr)
		case *ast.ReturnStmt:
			for _, v := range st.Values {
				walkExpr(v)
			}
		case *ast.DeferStmt:
			if st.Call != nil {
				walkExpr(st.Call)
			}
		case *ast.AssignStmt:
			for _, r := range st.Right {
				walkExpr(r)
			}
			for _, l := range st.Left {
				if st.Token.Literal == ":=" {
					if ident, ok := l.(*ast.Identifier); ok {
						locals[ident.Value] = true
					}
				} else {
					walkExpr(l)
				}
			}
		case *ast.VarDecl:
			locals[st.Name.Value] = true
			if st.Value != nil {
				walkExpr(st.Value)
			}
		case *ast.IfStmt:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Condition)
			walkStmt(st.Consequence)
			if st.Alternative != nil {
				walkStmt(st.Alternative)
			}
		case *ast.ForStmt:
			if st.Init != nil {
				walkStmt(st.Init)
			}
			walkExpr(st.Cond)
			if st.Post != nil {
				walkStmt(st.Post)
			}
			walkStmt(st.Body)
		case *ast.ForRangeStmt:
			if kIdent, ok := st.Key.(*ast.Identifier); ok && kIdent != nil {
				locals[kIdent.Value] = true
			}
			if vIdent, ok := st.Value.(*ast.Identifier); ok && vIdent != nil {
				locals[vIdent.Value] = true
			}
			walkExpr(st.X)
			walkStmt(st.Body)
		}
	}

	if fl.Body != nil {
		for _, s := range fl.Body.Statements {
			walkStmt(s)
		}
	}
	return captured
}

func insertImplicitCasts(prog *ast.Program, ctx *Context) {
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			// 未具象化のジェネリック関数テンプレートは単相化前のため型解決をスキップ
			if IsGenericFuncDecl(fn) {
				continue
			}

			locals := make(map[string]Type)
			if fn.Receiver != nil {
				locals[fn.Receiver.Name.Value] = ctx.ResolveType(fn.Receiver.Type)
			}
			for _, p := range fn.Params {
				locals[p.Name.Value] = ctx.ResolveType(p.Type)
			}
			insertCastsInBlock(fn.Body, locals, ctx, fn)
		}
	}
}

func insertCastsInBlock(b *ast.BlockStmt, locals map[string]Type, ctx *Context, currentFn *ast.FuncDecl) {
	if b == nil {
		return
	}
	for _, stmt := range b.Statements {
		switch s := stmt.(type) {
		case *ast.VarDecl:
			var targetType Type = TypeInt
			if s.Type != nil {
				targetType = ctx.ResolveType(s.Type)
			} else if s.Value != nil {
				targetType = ctx.InferExprType(s.Value, locals)
			}
			locals[s.Name.Value] = targetType
			if s.Value != nil {
				s.Value = ctx.CoerceExpr(s.Value, targetType, locals)
			}

		case *ast.AssignStmt:
			for i, left := range s.Left {
				var targetType Type = nil
				if id, ok := left.(*ast.Identifier); ok {
					if s.Token.Literal == ":=" {
						if i < len(s.Right) {
							targetType = ctx.InferExprType(s.Right[i], locals)
							locals[id.Value] = targetType
						}
					} else if t, ok := locals[id.Value]; ok {
						targetType = t
					} else if t, ok := ctx.Globals[id.Value]; ok {
						targetType = t
					}
				} else {
					targetType = ctx.InferExprType(left, locals)
				}

				if targetType != nil && i < len(s.Right) {
					s.Right[i] = ctx.CoerceExpr(s.Right[i], targetType, locals)
				}
			}

		case *ast.ReturnStmt:
			for i, val := range s.Values {
				if i < len(currentFn.ReturnTypes) {
					expected := ctx.ResolveType(currentFn.ReturnTypes[i])
					s.Values[i] = ctx.CoerceExpr(val, expected, locals)
				}
			}

		case *ast.ExprStmt:
			insertCastsInExpr(s.Expr, locals, ctx)

		case *ast.IfStmt:
			if s.Init != nil {
				if initBlock, ok := s.Init.(*ast.BlockStmt); ok {
					insertCastsInBlock(initBlock, locals, ctx, currentFn)
				}
			}
			insertCastsInExpr(s.Condition, locals, ctx)
			if s.Consequence != nil {
				insertCastsInBlock(s.Consequence, locals, ctx, currentFn)
			}
			if s.Alternative != nil {
				if altBlock, ok := s.Alternative.(*ast.BlockStmt); ok {
					insertCastsInBlock(altBlock, locals, ctx, currentFn)
				}
			}

		case *ast.ForStmt:
			insertCastsInExpr(s.Cond, locals, ctx)
			if s.Body != nil {
				insertCastsInBlock(s.Body, locals, ctx, currentFn)
			}

		case *ast.ForRangeStmt:
			insertCastsInExpr(s.X, locals, ctx)
			if s.Body != nil {
				insertCastsInBlock(s.Body, locals, ctx, currentFn)
			}
		}
	}
}

func insertCastsInExpr(e ast.Expression, locals map[string]Type, ctx *Context) {
	if e == nil {
		return
	}
	switch expr := e.(type) {
	case *ast.CallExpr:
		fnType := ctx.InferExprType(expr.Function, locals)
		if ft, ok := fnType.(*FuncType); ok {
			for i, arg := range expr.Args {
				if i < len(ft.ParamTypes) {
					expr.Args[i] = ctx.CoerceExpr(arg, ft.ParamTypes[i], locals)
				}
			}
		}
		insertCastsInExpr(expr.Function, locals, ctx)
		for _, arg := range expr.Args {
			insertCastsInExpr(arg, locals, ctx)
		}
	case *ast.BinaryExpr:
		insertCastsInExpr(expr.Left, locals, ctx)
		insertCastsInExpr(expr.Right, locals, ctx)
	case *ast.PrefixExpr:
		insertCastsInExpr(expr.Right, locals, ctx)
	}
}

func (c *Context) EnsureMapSupported(line, col int) error {
	if !c.HasMapImport {
		return fmt.Errorf("line %d:%d: map syntax requires importing 'std/maps'", line, col)
	}
	return nil
}

func validateMapUsage(node ast.Node, ctx *Context) error {
	if node == nil {
		return nil
	}

	var checkType func(t ast.TypeExpr) error
	var checkExpr func(e ast.Expression) error
	var checkStmt func(s ast.Statement) error

	checkType = func(t ast.TypeExpr) error {
		if t == nil {
			return nil
		}
		if mt, ok := t.(*ast.MapType); ok {
			if !ctx.HasMapImport {
				return fmt.Errorf("line %d:%d: map type 'map[%s]%s' requires importing 'std/maps'",
					mt.Token.Line, mt.Token.Col, mt.Key.TokenLiteral(), mt.Value.TokenLiteral())
			}
			if err := checkType(mt.Key); err != nil {
				return err
			}
			return checkType(mt.Value)
		}
		if pt, ok := t.(*ast.PointerType); ok {
			return checkType(pt.Base)
		}
		if sl, ok := t.(*ast.SliceType); ok {
			return checkType(sl.Elem)
		}
		if ar, ok := t.(*ast.ArrayType); ok {
			return checkType(ar.Elem)
		}
		if ft, ok := t.(*ast.FuncType); ok {
			for _, pt := range ft.ParamTypes {
				if err := checkType(pt); err != nil {
					return err
				}
			}
			for _, rt := range ft.ReturnTypes {
				if err := checkType(rt); err != nil {
					return err
				}
			}
		}
		return nil
	}

	checkExpr = func(e ast.Expression) error {
		if e == nil {
			return nil
		}
		if te, ok := e.(ast.TypeExpr); ok {
			return checkType(te)
		}
		switch n := e.(type) {
		case *ast.FuncLit:
			for _, p := range n.Params {
				if err := checkType(p.Type); err != nil {
					return err
				}
			}
			for _, rt := range n.ReturnTypes {
				if err := checkType(rt); err != nil {
					return err
				}
			}
			if n.Body != nil {
				return checkStmt(n.Body)
			}
			return nil

		case *ast.CallExpr:
			if id, ok := n.Function.(*ast.Identifier); ok && (id.Value == "make" || id.Value == "delete") {
				if len(n.Args) > 0 {
					if _, isMap := n.Args[0].(*ast.MapType); isMap && !ctx.HasMapImport {
						return fmt.Errorf("line %d:%d: '%s(map...)' requires importing 'std/maps'",
							n.Token.Line, n.Token.Col, id.Value)
					}
				}
			}
			if err := checkExpr(n.Function); err != nil {
				return err
			}
			for _, arg := range n.Args {
				if err := checkExpr(arg); err != nil {
					return err
				}
			}
		case *ast.BinaryExpr:
			if err := checkExpr(n.Left); err != nil {
				return err
			}
			return checkExpr(n.Right)
		case *ast.PrefixExpr:
			return checkExpr(n.Right)
		case *ast.IndexExpr:
			if err := checkExpr(n.Left); err != nil {
				return err
			}
			return checkExpr(n.Index)
		case *ast.MemberExpr:
			return checkExpr(n.Object)
		case *ast.StructLiteral:
			for _, f := range n.Fields {
				if err := checkExpr(f.Value); err != nil {
					return err
				}
			}
		}
		return nil
	}

	checkStmt = func(s ast.Statement) error {
		if s == nil {
			return nil
		}
		switch st := s.(type) {
		case *ast.VarDecl:
			if err := checkType(st.Type); err != nil {
				return err
			}
			return checkExpr(st.Value)
		case *ast.AssignStmt:
			if err := checkType(st.Type); err != nil {
				return err
			}
			for _, l := range st.Left {
				if err := checkExpr(l); err != nil {
					return err
				}
			}
			for _, r := range st.Right {
				if err := checkExpr(r); err != nil {
					return err
				}
			}
		case *ast.ExprStmt:
			return checkExpr(st.Expr)
		case *ast.BlockStmt:
			for _, inner := range st.Statements {
				if err := checkStmt(inner); err != nil {
					return err
				}
			}
		case *ast.IfStmt:
			if err := checkStmt(st.Init); err != nil {
				return err
			}
			if err := checkExpr(st.Condition); err != nil {
				return err
			}
			if err := checkStmt(st.Consequence); err != nil {
				return err
			}
			return checkStmt(st.Alternative)
		case *ast.ForStmt:
			if err := checkStmt(st.Init); err != nil {
				return err
			}
			if err := checkExpr(st.Cond); err != nil {
				return err
			}
			if err := checkStmt(st.Post); err != nil {
				return err
			}
			return checkStmt(st.Body)
		case *ast.ForRangeStmt:
			if err := checkExpr(st.X); err != nil {
				return err
			}
			return checkStmt(st.Body)
		case *ast.ReturnStmt:
			for _, v := range st.Values {
				if err := checkExpr(v); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if td, ok := node.(*ast.TypeDecl); ok {
		if len(td.TypeParams) > 0 {
			return nil
		}
		return checkType(td.Type)
	}
	if fd, ok := node.(*ast.FuncDecl); ok {
		if IsGenericFuncDecl(fd) {
			return nil
		}
		if fd.Receiver != nil {
			if err := checkType(fd.Receiver.Type); err != nil {
				return err
			}
		}
		for _, p := range fd.Params {
			if err := checkType(p.Type); err != nil {
				return err
			}
		}
		for _, rt := range fd.ReturnTypes {
			if err := checkType(rt); err != nil {
				return err
			}
		}
		if fd.Body != nil {
			return checkStmt(fd.Body)
		}
	}
	if vd, ok := node.(*ast.VarDecl); ok {
		return checkStmt(vd)
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

func isTypeParamExpr(t ast.TypeExpr) bool {
	if t == nil {
		return false
	}
	switch node := t.(type) {
	case *ast.NamedType:
		if node.Package == nil && len(node.TypeArgs) == 0 {
			name := node.Name.Value
			switch name {
			case "int", "byte", "bool", "float32", "float64", "float", "string", "void", "any", "error":
				return false
			}
			if len(name) <= 2 {
				return true
			}
		}
		for _, ta := range node.TypeArgs {
			if isTypeParamExpr(ta) {
				return true
			}
		}
	case *ast.PointerType:
		return isTypeParamExpr(node.Base)
	case *ast.SliceType:
		return isTypeParamExpr(node.Elem)
	}
	return false
}

func IsGenericFuncDecl(fd *ast.FuncDecl) bool {
	if fd == nil {
		return false
	}
	if len(fd.TypeParams) > 0 {
		return true
	}
	if fd.Receiver != nil {
		t := fd.Receiver.Type
		if pt, ok := t.(*ast.PointerType); ok {
			t = pt.Base
		}
		if nt, ok := t.(*ast.NamedType); ok {
			for _, ta := range nt.TypeArgs {
				if isTypeParamExpr(ta) {
					return true
				}
			}
		}
	}
	return false
}
