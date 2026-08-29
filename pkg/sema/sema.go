package sema

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
)

type Type interface {
	TypeName() string
	Size() int
	LLVMType() string
}

type BasicType struct {
	Name     string
	ByteSize int
	LLVM     string
}

func (b *BasicType) TypeName() string { return b.Name }
func (b *BasicType) Size() int        { return b.ByteSize }
func (b *BasicType) LLVMType() string { return b.LLVM }

var (
	TypeInt    = &BasicType{Name: "int", ByteSize: 8, LLVM: "i64"}
	TypeByte   = &BasicType{Name: "byte", ByteSize: 1, LLVM: "i8"}
	TypeString = &BasicType{Name: "string", ByteSize: 8, LLVM: "i8*"} // 追加
	TypeBool   = &BasicType{Name: "bool", ByteSize: 1, LLVM: "i1"}
	TypeVoid   = &BasicType{Name: "void", ByteSize: 0, LLVM: "void"}
	TypeError  = &BasicType{Name: "error", ByteSize: 8, LLVM: "i8*"}
)

type PointerType struct {
	Base Type
}

func (p *PointerType) TypeName() string { return "*" + p.Base.TypeName() }
func (p *PointerType) Size() int        { return 8 }
func (p *PointerType) LLVMType() string {
	if p.Base == TypeByte {
		return "i8*"
	}
	if st, ok := p.Base.(*StructType); ok {
		return fmt.Sprintf("%%struct.%s*", st.Name)
	}
	return p.Base.LLVMType() + "*"
}

type SliceType struct {
	Elem Type
}

func (s *SliceType) TypeName() string { return "[]" + s.Elem.TypeName() }
func (s *SliceType) Size() int        { return 24 }
func (s *SliceType) LLVMType() string { return "{ i8*, i64, i64 }" }

type Field struct {
	Name string
	Type Type
}

type StructType struct {
	Name      string
	Fields    []Field
	TotalSize int
}

func (s *StructType) TypeName() string { return s.Name }
func (s *StructType) Size() int        { return s.TotalSize }
func (s *StructType) LLVMType() string { return fmt.Sprintf("%%struct.%s", s.Name) }

type FuncType struct {
	Name        string
	Receiver    Type
	ParamTypes  []Type
	ReturnTypes []Type
	IsVariadic  bool
	IsExtern    bool
}

func (f *FuncType) TypeName() string { return f.Name }
func (f *FuncType) Size() int        { return 8 }
func (f *FuncType) LLVMType() string { return "i8*" }

type Context struct {
	Structs   map[string]*StructType
	Functions map[string]*FuncType
	Constants map[string]int64
}

func NewContext() *Context {
	ctx := &Context{
		Structs:   make(map[string]*StructType),
		Functions: make(map[string]*FuncType),
		Constants: make(map[string]int64),
	}
	ctx.registerBuiltins()
	return ctx
}

func (ctx *Context) registerBuiltins() {
	ctx.Functions["malloc"] = &FuncType{
		Name:        "malloc",
		ParamTypes:  []Type{TypeInt},
		ReturnTypes: []Type{&PointerType{Base: TypeByte}},
		IsExtern:    true,
	}
	ctx.Functions["free"] = &FuncType{
		Name:        "free",
		ParamTypes:  []Type{&PointerType{Base: TypeByte}},
		ReturnTypes: []Type{},
		IsExtern:    true,
	}
	ctx.Functions["calloc"] = &FuncType{
		Name:        "calloc",
		ParamTypes:  []Type{TypeInt, TypeInt},
		ReturnTypes: []Type{&PointerType{Base: TypeByte}},
		IsExtern:    true,
	}
	ctx.Functions["printf"] = &FuncType{
		Name:        "printf",
		ParamTypes:  []Type{&PointerType{Base: TypeByte}},
		ReturnTypes: []Type{TypeInt},
		IsVariadic:  true,
		IsExtern:    true,
	}
	ctx.Functions["strlen"] = &FuncType{
		Name:        "strlen",
		ParamTypes:  []Type{&PointerType{Base: TypeByte}},
		ReturnTypes: []Type{TypeInt},
		IsExtern:    true,
	}
	ctx.Functions["strcmp"] = &FuncType{
		Name:        "strcmp",
		ParamTypes:  []Type{&PointerType{Base: TypeByte}, &PointerType{Base: TypeByte}},
		ReturnTypes: []Type{TypeInt},
		IsExtern:    true,
	}
	ctx.Functions["memcpy"] = &FuncType{
		Name:        "memcpy",
		ParamTypes:  []Type{&PointerType{Base: TypeByte}, &PointerType{Base: TypeByte}, TypeInt},
		ReturnTypes: []Type{&PointerType{Base: TypeByte}},
		IsExtern:    true,
	}
	ctx.Functions["memcmp"] = &FuncType{
		Name:        "memcmp",
		ParamTypes:  []Type{&PointerType{Base: TypeByte}, &PointerType{Base: TypeByte}, TypeInt},
		ReturnTypes: []Type{TypeInt},
		IsExtern:    true,
	}
}

func (ctx *Context) ResolveType(expr ast.TypeExpr) Type {
	if expr == nil {
		return TypeVoid
	}
	switch t := expr.(type) {
	case *ast.SliceType:
		elem := ctx.ResolveType(t.Elem)
		return &SliceType{Elem: elem}
	case *ast.PointerType:
		base := ctx.ResolveType(t.Base)
		return &PointerType{Base: base}
	case *ast.NamedType:
		name := t.Name.Value
		if t.Package != nil {
			cand := t.Package.Value + "_" + name
			if st, ok := ctx.Structs[cand]; ok {
				return st
			}
			if st, ok := ctx.Structs[name]; ok {
				return st
			}
		}
		if name == "int" {
			return TypeInt
		}
		if name == "byte" {
			return TypeByte
		}
		if name == "string" {
			return TypeString
		}
		if name == "bool" {
			return TypeBool
		}
		if name == "error" {
			return TypeError
		}
		if st, ok := ctx.Structs[name]; ok {
			return st
		}
		for sName, st := range ctx.Structs {
			if strings.HasSuffix(sName, "_"+name) || strings.HasSuffix(sName, name) {
				return st
			}
		}
		return &BasicType{Name: name, ByteSize: 8, LLVM: "%struct." + name}
	}
	return TypeInt
}

func Analyze(prog *ast.Program) (*Context, error) {
	ctx := NewContext()

	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if st, ok := td.Type.(*ast.StructType); ok {
				structType := &StructType{
					Name:   td.Name.Value,
					Fields: []Field{},
				}
				totalSize := 0
				for _, f := range st.Fields {
					fType := ctx.ResolveType(f.Type)
					structType.Fields = append(structType.Fields, Field{
						Name: f.Name.Value,
						Type: fType,
					})
					totalSize += fType.Size()
				}
				structType.TotalSize = totalSize
				ctx.Structs[td.Name.Value] = structType
			}
		}
	}

	for _, decl := range prog.Decls {
		if cd, ok := decl.(*ast.ConstDecl); ok {
			val := evalConstExpr(cd.Value, ctx.Constants)
			ctx.Constants[cd.Name.Value] = val
		}
	}

	for _, decl := range prog.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			var recvType Type = nil
			var mangledName = fd.Name.Value

			if fd.Receiver != nil {
				recvType = ctx.ResolveType(fd.Receiver.Type)
				var recvTypeName = ""
				if named, ok := fd.Receiver.Type.(*ast.NamedType); ok {
					recvTypeName = named.Name.Value
				} else if pt, ok := fd.Receiver.Type.(*ast.PointerType); ok {
					if named, ok := pt.Base.(*ast.NamedType); ok {
						recvTypeName = named.Name.Value
					}
				}
				if strings.Contains(mangledName, "_") {
					parts := strings.SplitN(mangledName, "_", 2)
					mangledName = parts[0] + "_" + recvTypeName + "_" + parts[1]
				} else {
					mangledName = recvTypeName + "_" + mangledName
				}
			}

			paramTypes := []Type{}
			for _, p := range fd.Params {
				paramTypes = append(paramTypes, ctx.ResolveType(p.Type))
			}

			retTypes := []Type{}
			for _, rt := range fd.ReturnTypes {
				retTypes = append(retTypes, ctx.ResolveType(rt))
			}

			fnType := &FuncType{
				Name:        mangledName,
				Receiver:    recvType,
				ParamTypes:  paramTypes,
				ReturnTypes: retTypes,
				IsVariadic:  fd.IsVariadic,
				IsExtern:    fd.Body == nil,
			}

			ctx.Functions[fd.Name.Value] = fnType
			if fd.Receiver != nil {
				ctx.Functions[mangledName] = fnType
			}
		}
	}

	return ctx, nil
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
		}
	}
	return 0
}
