package sema

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
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
	Name   string
	Fields []Field
}

func (t *StructType) TypeName() string { return t.Name }
func (t *StructType) LLVMType() string { return "%struct." + t.Name }
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
	Name    string
	Methods []Method
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

func (t *InterfaceType) Size() int   { return 16 }
func (t *InterfaceType) IsAny() bool { return len(t.Methods) == 0 }

type FuncType struct {
	Name        string
	Receiver    Type
	ParamTypes  []Type
	ReturnTypes []Type
	IsVariadic  bool
	IsExtern    bool
}

func (t *FuncType) TypeName() string { return "func" }
func (t *FuncType) LLVMType() string { return "{ i8*, i8* }" }
func (t *FuncType) Size() int        { return 16 }

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
	typeIDs        map[string]int64
	nextTypeID     int64
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
		typeIDs:        make(map[string]int64),
		nextTypeID:     1,
	}

	ctx.typeIDs["int"] = 1
	ctx.typeIDs["byte"] = 2
	ctx.typeIDs["bool"] = 3
	ctx.typeIDs["string"] = 4
	ctx.typeIDs["float32"] = 5
	ctx.typeIDs["float64"] = 6
	ctx.nextTypeID = 7

	errorIface := &InterfaceType{
		Name: "error",
		Methods: []Method{
			{Name: "Error", ParamTypes: []Type{}, ReturnTypes: []Type{TypeString}},
		},
	}
	ctx.Interfaces["error"] = errorIface
	ctx.Aliases["error"] = errorIface

	return ctx
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

		// ジェネリック型引数の適用 (例: Vector[int], Pair[string, int])
		if len(t.TypeArgs) > 0 {
			baseName := t.Name.Value
			genDecl, exists := c.GenericTypes[baseName]
			if exists {
				argNames := []string{}
				typeMap := make(map[string]Type)
				for i, arg := range t.TypeArgs {
					resolvedArg := c.ResolveType(arg)
					argNames = append(argNames, strings.ReplaceAll(resolvedArg.TypeName(), "*", "Ptr"))
					if i < len(genDecl.TypeParams) {
						typeMap[genDecl.TypeParams[i].Name.Value] = resolvedArg
					}
				}

				specializedName := fmt.Sprintf("%s__%s", baseName, strings.Join(argNames, "_"))
				if st, ok := c.Structs[specializedName]; ok {
					return st
				}

				// 特殊化構造体の生成
				if stType, ok := genDecl.Type.(*ast.StructType); ok {
					newSt := &StructType{Name: specializedName, Fields: []Field{}}
					c.Structs[specializedName] = newSt

					for _, f := range stType.Fields {
						fType := c.ResolveTypeWithSubst(f.Type, typeMap)
						newSt.Fields = append(newSt.Fields, Field{
							Name:       f.Name.Value,
							Type:       fType,
							IsEmbedded: f.IsEmbedded,
						})
					}
					return newSt
				}
			}
		}

		if st, ok := c.Structs[name]; ok {
			return st
		}
		if t.Package != nil {
			qualified := t.Package.Value + "_" + name
			if iface, ok := c.Interfaces[qualified]; ok {
				return iface
			}
			if alias, ok := c.Aliases[qualified]; ok {
				return alias
			}
			if st, ok := c.Structs[qualified]; ok {
				return st
			}
			name = qualified
		}

		if iface, ok := c.Interfaces[name]; ok {
			return iface
		}
		if alias, ok := c.Aliases[name]; ok {
			return alias
		}
		if st, ok := c.Structs[name]; ok {
			return st
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
			return &InterfaceType{Name: "any"}
		case "error":
			return c.Interfaces["error"]
		default:
			return &BasicType{Name: name, ByteSize: 8, LLVM: "%struct." + name}
		}
	case *ast.PointerType:
		base := c.ResolveType(t.Base)
		return &PointerType{Base: base}
	case *ast.SliceType:
		elem := c.ResolveType(t.Elem)
		return &SliceType{Elem: elem}
	case *ast.ArrayType:
		elem := c.ResolveType(t.Elem)
		return &ArrayType{Len: int(t.Len), Elem: elem}
	case *ast.MapType:
		k := c.ResolveType(t.Key)
		v := c.ResolveType(t.Value)
		return &MapType{Key: k, Value: v}
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
		return &InterfaceType{Name: "", Methods: methods}
	case *ast.FuncType:
		fnType := &FuncType{
			ParamTypes:  []Type{},
			ReturnTypes: []Type{},
		}
		for _, pt := range t.ParamTypes {
			fnType.ParamTypes = append(fnType.ParamTypes, c.ResolveType(pt))
		}
		for _, rt := range t.ReturnTypes {
			fnType.ReturnTypes = append(fnType.ReturnTypes, c.ResolveType(rt))
		}
		return fnType
	}
	return TypeVoid
}

// 型パラメータの置換マップ（subst）を適用して型を再帰的に解決する
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

		// 型引数自身が型パラメータを含む場合（例: Vector[T] で T が subst に存在する場合）
		if len(node.TypeArgs) > 0 {
			baseName := node.Name.Value
			genDecl, exists := c.GenericTypes[baseName]
			if exists {
				argNames := []string{}
				typeMap := make(map[string]Type)
				for i, arg := range node.TypeArgs {
					resolvedArg := c.ResolveTypeWithSubst(arg, subst)
					argNames = append(argNames, strings.ReplaceAll(resolvedArg.TypeName(), "*", "Ptr"))
					if i < len(genDecl.TypeParams) {
						typeMap[genDecl.TypeParams[i].Name.Value] = resolvedArg
					}
				}

				specializedName := fmt.Sprintf("%s__%s", baseName, strings.Join(argNames, "_"))
				if st, ok := c.Structs[specializedName]; ok {
					return st
				}

				if stType, ok := genDecl.Type.(*ast.StructType); ok {
					newSt := &StructType{Name: specializedName, Fields: []Field{}}
					c.Structs[specializedName] = newSt

					for _, f := range stType.Fields {
						fType := c.ResolveTypeWithSubst(f.Type, typeMap)
						newSt.Fields = append(newSt.Fields, Field{
							Name:       f.Name.Value,
							Type:       fType,
							IsEmbedded: f.IsEmbedded,
						})
					}
					return newSt
				}
			}
		}
		return c.ResolveType(node)
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

func Analyze(prog *ast.Program) (*Context, error) {
	ctx := NewContext()

	// Pass 1: 全ての名前付き型およびジェネリックテンプレートの先行登録
	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if len(td.TypeParams) > 0 {
				ctx.GenericTypes[td.Name.Value] = td
				continue
			}
			if _, ok := td.Type.(*ast.InterfaceType); ok {
				ctx.Interfaces[td.Name.Value] = &InterfaceType{Name: td.Name.Value, Methods: []Method{}}
				ctx.Aliases[td.Name.Value] = ctx.Interfaces[td.Name.Value]
			} else if _, ok := td.Type.(*ast.StructType); ok {
				ctx.Structs[td.Name.Value] = &StructType{Name: td.Name.Value, Fields: []Field{}}
				ctx.Aliases[td.Name.Value] = ctx.Structs[td.Name.Value]
			}
		} else if fd, ok := decl.(*ast.FuncDecl); ok {
			if len(fd.TypeParams) > 0 {
				ctx.GenericFuncs[fd.Name.Value] = fd
			}
		}
	}

	// Pass 1.5: 各 Struct のフィールド型および Interface のメソッドシグネチャを完全解決
	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if len(td.TypeParams) > 0 {
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

	// Pass 2: 定数、グローバル変数、関数のシグネチャを確定
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
			if len(d.TypeParams) > 0 {
				ctx.GenericFuncs[d.Name.Value] = d
				continue
			}
			fnName := d.Name.Value
			var recvType Type = nil
			var recvTypeName string = ""
			paramTypes := []Type{}

			if d.Receiver != nil {
				recvType = ctx.ResolveType(d.Receiver.Type)
				if named, ok := d.Receiver.Type.(*ast.NamedType); ok {
					recvTypeName = named.Name.Value
				} else if pt, ok := d.Receiver.Type.(*ast.PointerType); ok {
					if named, ok := pt.Base.(*ast.NamedType); ok {
						recvTypeName = named.Name.Value
					}
				}
				if recvTypeName != "" {
					fnName = recvTypeName + "_" + fnName
				}
				paramTypes = append(paramTypes, recvType)
			}

			for _, p := range d.Params {
				paramTypes = append(paramTypes, ctx.ResolveType(p.Type))
			}

			returnTypes := []Type{}
			for _, rt := range d.ReturnTypes {
				returnTypes = append(returnTypes, ctx.ResolveType(rt))
			}

			ctx.Functions[fnName] = &FuncType{
				Name:        fnName,
				Receiver:    recvType,
				ParamTypes:  paramTypes,
				ReturnTypes: returnTypes,
				IsVariadic:  d.IsVariadic,
				IsExtern:    (d.Body == nil),
			}
		}
	}

	return ctx, nil
}
