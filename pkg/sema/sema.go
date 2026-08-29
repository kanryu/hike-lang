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

func (b *BasicType) TypeName() string { return b.Name }
func (b *BasicType) LLVMType() string { return b.LLVM }
func (b *BasicType) Size() int        { return b.ByteSize }

var (
	TypeInt    = &BasicType{Name: "int", ByteSize: 8, LLVM: "i64"}
	TypeByte   = &BasicType{Name: "byte", ByteSize: 1, LLVM: "i8"}
	TypeBool   = &BasicType{Name: "bool", ByteSize: 1, LLVM: "i1"}
	TypeString = &BasicType{Name: "string", ByteSize: 8, LLVM: "i8*"}
	TypeVoid   = &BasicType{Name: "void", ByteSize: 0, LLVM: "void"}
)

type PointerType struct {
	Base Type
}

func (p *PointerType) TypeName() string { return "*" + p.Base.TypeName() }
func (p *PointerType) LLVMType() string { return p.Base.LLVMType() + "*" }
func (p *PointerType) Size() int        { return 8 }

type SliceType struct {
	Elem Type
}

func (s *SliceType) TypeName() string { return "[]" + s.Elem.TypeName() }
func (s *SliceType) LLVMType() string { return "{ i8*, i64, i64 }" }
func (s *SliceType) Size() int        { return 24 }

// 固定長配列型 [N]T
type ArrayType struct {
	Len  int
	Elem Type
}

func (a *ArrayType) TypeName() string { return fmt.Sprintf("[%d]%s", a.Len, a.Elem.TypeName()) }
func (a *ArrayType) LLVMType() string { return fmt.Sprintf("[%d x %s]", a.Len, a.Elem.LLVMType()) }
func (a *ArrayType) Size() int        { return a.Len * a.Elem.Size() }

// インターフェース型 (ファットポインタ: { i8* data, i64 type_id })
type InterfaceType struct {
	Name string
}

func (it *InterfaceType) TypeName() string {
	if it.Name != "" {
		return it.Name
	}
	return "interface{}"
}
func (it *InterfaceType) LLVMType() string { return "{ i8*, i64 }" }
func (it *InterfaceType) Size() int        { return 16 }

type Field struct {
	Name string
	Type Type
}

type StructType struct {
	Name   string
	Fields []Field
}

func (s *StructType) TypeName() string { return s.Name }
func (s *StructType) LLVMType() string { return "%struct." + s.Name }
func (s *StructType) Size() int {
	sz := 0
	for _, f := range s.Fields {
		sz += f.Type.Size()
	}
	return sz
}

type FuncType struct {
	Name        string
	ParamTypes  []Type
	ReturnTypes []Type
	IsVariadic  bool
	IsExtern    bool
}

func (f *FuncType) TypeName() string { return f.Name }
func (f *FuncType) LLVMType() string { return "void" }
func (f *FuncType) Size() int        { return 8 }

type TupleType struct {
	Types []Type
}

func (t *TupleType) TypeName() string {
	var names []string
	for _, sub := range t.Types {
		names = append(names, sub.TypeName())
	}
	return "(" + strings.Join(names, ", ") + ")"
}
func (t *TupleType) LLVMType() string {
	var types []string
	for _, sub := range t.Types {
		types = append(types, sub.LLVMType())
	}
	return "{ " + strings.Join(types, ", ") + " }"
}
func (t *TupleType) Size() int {
	sz := 0
	for _, sub := range t.Types {
		sz += sub.Size()
	}
	return sz
}

type Context struct {
	Functions map[string]*FuncType
	Structs   map[string]*StructType
	Aliases   map[string]Type
	Constants map[string]int64
	typeIDs   map[string]int64
	nextID    int64
}

func NewContext() *Context {
	ctx := &Context{
		Functions: make(map[string]*FuncType),
		Structs:   make(map[string]*StructType),
		Aliases:   make(map[string]Type),
		Constants: make(map[string]int64),
		typeIDs:   make(map[string]int64),
		nextID:    1,
	}

	ctx.GetTypeID(TypeInt)
	ctx.GetTypeID(TypeByte)
	ctx.GetTypeID(TypeBool)
	ctx.GetTypeID(TypeString)

	ctx.Aliases["any"] = &InterfaceType{Name: "any"}
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
	id := c.nextID
	c.nextID++
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
		case "string":
			return TypeString
		case "any":
			return &InterfaceType{Name: "any"}
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
	case *ast.InterfaceType:
		return &InterfaceType{Name: "interface{}"}
	}
	return TypeVoid
}

func evalConstExpr(expr ast.Expression, consts map[string]int64) int64 {
	if expr == nil {
		return 0
	}
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return e.Value
	case *ast.IotaExpr:
		return e.Value
	case *ast.Identifier:
		if val, ok := consts[e.Value]; ok {
			return val
		}
	case *ast.PrefixExpr:
		val := evalConstExpr(e.Right, consts)
		switch e.Operator {
		case "-":
			return -val
		case "^":
			return ^val
		case "!":
			if val == 0 {
				return 1
			}
			return 0
		}
	case *ast.BinaryExpr:
		left := evalConstExpr(e.Left, consts)
		right := evalConstExpr(e.Right, consts)
		switch e.Operator {
		case "+":
			return left + right
		case "-":
			return left - right
		case "*":
			return left * right
		case "/":
			if right != 0 {
				return left / right
			}
		case "<<":
			return left << uint(right)
		case ">>":
			return left >> uint(right)
		case "|":
			return left | right
		case "&":
			return left & right
		case "^":
			return left ^ right
		}
	}
	return 0
}

func Analyze(prog *ast.Program) (*Context, error) {
	ctx := NewContext()

	// 1. 型エイリアス・構造体登録
	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if stNode, ok := td.Type.(*ast.StructType); ok {
				st := &StructType{Name: td.Name.Value, Fields: []Field{}}
				ctx.Structs[td.Name.Value] = st
				for _, f := range stNode.Fields {
					st.Fields = append(st.Fields, Field{Name: f.Name.Value, Type: ctx.ResolveType(f.Type)})
				}
			} else if _, ok := td.Type.(*ast.InterfaceType); ok {
				ctx.Aliases[td.Name.Value] = &InterfaceType{Name: td.Name.Value}
			} else {
				ctx.Aliases[td.Name.Value] = ctx.ResolveType(td.Type)
			}
		}
	}

	// 2. 定数解決
	for _, decl := range prog.Decls {
		if cd, ok := decl.(*ast.ConstDecl); ok {
			val := evalConstExpr(cd.Value, ctx.Constants)
			ctx.Constants[cd.Name.Value] = val
		}
	}

	// 3. 関数シグネチャ登録
	for _, decl := range prog.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fnType := &FuncType{
				Name:        fd.Name.Value,
				ParamTypes:  []Type{},
				ReturnTypes: []Type{},
				IsVariadic:  fd.IsVariadic,
				IsExtern:    fd.Body == nil, // 追加: 本体なし関数を外部宣言と判定
			}
			for _, p := range fd.Params {
				fnType.ParamTypes = append(fnType.ParamTypes, ctx.ResolveType(p.Type))
			}
			for _, rt := range fd.ReturnTypes {
				fnType.ReturnTypes = append(fnType.ReturnTypes, ctx.ResolveType(rt))
			}
			ctx.Functions[fd.Name.Value] = fnType
		}
	}

	return ctx, nil
}
