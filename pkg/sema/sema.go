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
	TypeInt   = &BasicType{Name: "int", ByteSize: 8, LLVM: "i64"}
	TypeByte  = &BasicType{Name: "byte", ByteSize: 1, LLVM: "i8"}
	TypeBool  = &BasicType{Name: "bool", ByteSize: 1, LLVM: "i1"}
	TypeVoid  = &BasicType{Name: "void", ByteSize: 0, LLVM: "void"}
	TypeError = &BasicType{Name: "error", ByteSize: 8, LLVM: "i8*"}
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

// スライス型: { i8* data, i64 len, i64 cap }
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
	// 標準Cライブラリ組み込み関数
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

// Analyze はAST全体を解析し、型情報・関数シグネチャ・定数テーブルを構築します
func Analyze(prog *ast.Program) (*Context, error) {
	ctx := NewContext()

	// パス1: 構造体型宣言（TypeDecl）の収集
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

	// パス2: 定数宣言（ConstDecl）の収集
	for _, decl := range prog.Decls {
		if cd, ok := decl.(*ast.ConstDecl); ok {
			if il, ok := cd.Value.(*ast.IntegerLiteral); ok {
				ctx.Constants[cd.Name.Value] = il.Value
			}
		}
	}

	// パス3: 関数宣言（FuncDecl）のシグネチャ収集
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
