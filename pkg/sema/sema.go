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

func (t *BasicType) TypeName() string { return t.Name }
func (t *BasicType) Size() int        { return t.ByteSize }
func (t *BasicType) LLVMType() string { return t.LLVM }

var (
	TypeInt   = &BasicType{Name: "int", ByteSize: 8, LLVM: "i64"}
	TypeByte  = &BasicType{Name: "byte", ByteSize: 1, LLVM: "i8"}
	TypeBool  = &BasicType{Name: "bool", ByteSize: 1, LLVM: "i1"}
	TypeError = &BasicType{Name: "error", ByteSize: 8, LLVM: "i64"}
	TypeVoid  = &BasicType{Name: "void", ByteSize: 0, LLVM: "void"}
)

type PointerType struct {
	Base Type
}

func (p *PointerType) TypeName() string { return "*" + p.Base.TypeName() }
func (p *PointerType) Size() int        { return 8 }
func (p *PointerType) LLVMType() string { return p.Base.LLVMType() + "*" }

type StructField struct {
	Name   string
	Type   Type
	Offset int
	Index  int
}

type StructType struct {
	Name      string
	Fields    []StructField
	FieldMap  map[string]StructField
	TotalSize int
}

func (s *StructType) TypeName() string { return s.Name }
func (s *StructType) Size() int        { return s.TotalSize }
func (s *StructType) LLVMType() string { return "%struct." + s.Name }

type FuncType struct {
	Name        string
	ParamTypes  []Type
	ParamNames  []string
	ReturnTypes []Type
	IsExtern    bool
	IsVariadic  bool
}

type NamedType struct {
	Name         string
	ResolvedType Type
}

func (t *NamedType) TypeName() string {
	if t.ResolvedType != nil {
		return t.ResolvedType.TypeName()
	}
	return t.Name
}

func (t *NamedType) Size() int {
	if t.ResolvedType != nil {
		return t.ResolvedType.Size()
	}
	return 8
}

func (t *NamedType) LLVMType() string {
	if t.ResolvedType != nil {
		return t.ResolvedType.LLVMType()
	}
	return "%struct." + t.Name
}

type Context struct {
	Structs   map[string]*StructType
	Functions map[string]*FuncType
	Constants map[string]int64
	Errors    []string
}

func Analyze(prog *ast.Program) (*Context, error) {
	ctx := &Context{
		Structs:   make(map[string]*StructType),
		Functions: make(map[string]*FuncType),
		Constants: make(map[string]int64),
		Errors:    []string{},
	}

	// 0. 定数宣言の登録
	for _, decl := range prog.Decls {
		if cd, ok := decl.(*ast.ConstDecl); ok {
			if cd.Name != nil && cd.Value != nil {
				if intLit, isInt := cd.Value.(*ast.IntegerLiteral); isInt {
					ctx.Constants[cd.Name.Value] = intLit.Value
					if strings.Contains(cd.Name.Value, "_") {
						parts := strings.SplitN(cd.Name.Value, "_", 2)
						ctx.Constants[parts[1]] = intLit.Value
					}
				}
			}
		}
	}

	// ビルトイン Arena 構造体の登録
	arenaStruct := &StructType{
		Name: "Arena",
		Fields: []StructField{
			{Name: "buf", Type: &PointerType{Base: TypeByte}, Offset: 0, Index: 0},
			{Name: "cap", Type: TypeInt, Offset: 8, Index: 1},
			{Name: "offset", Type: TypeInt, Offset: 16, Index: 2},
		},
		TotalSize: 24,
	}
	arenaStruct.FieldMap = make(map[string]StructField)
	for _, f := range arenaStruct.Fields {
		arenaStruct.FieldMap[f.Name] = f
	}
	ctx.Structs["Arena"] = arenaStruct

	// 1-1. 全構造体名を先行登録 (プレフィックス付き・なし両方で登録)
	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if _, ok := td.Type.(*ast.StructType); ok {
				st := &StructType{
					Name:     td.Name.Value,
					Fields:   []StructField{},
					FieldMap: make(map[string]StructField),
				}
				ctx.Structs[td.Name.Value] = st
				if strings.Contains(td.Name.Value, "_") {
					parts := strings.SplitN(td.Name.Value, "_", 2)
					ctx.Structs[parts[1]] = st
				}
			}
		}
	}

	// 1-2. 各構造体のフィールド解決とレイアウト計算
	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if st, ok := td.Type.(*ast.StructType); ok {
				structType := ctx.Structs[td.Name.Value]
				totalOffset := 0
				for idx, field := range st.Fields {
					fType := ctx.resolveTypeExpr(field.Type)
					sf := StructField{
						Name:   field.Name.Value,
						Type:   fType,
						Offset: totalOffset,
						Index:  idx,
					}
					structType.Fields = append(structType.Fields, sf)
					structType.FieldMap[field.Name.Value] = sf
					totalOffset += fType.Size()
				}
				structType.TotalSize = totalOffset
			}
		}
	}

	// 2. 関数シグネチャの収集
	for _, decl := range prog.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fnType := &FuncType{
				Name:        fd.Name.Value,
				ParamTypes:  []Type{},
				ParamNames:  []string{},
				ReturnTypes: []Type{},
				IsExtern:    fd.Body == nil,
				IsVariadic:  fd.IsVariadic,
			}

			for _, p := range fd.Params {
				fnType.ParamTypes = append(fnType.ParamTypes, ctx.resolveTypeExpr(p.Type))
				fnType.ParamNames = append(fnType.ParamNames, p.Name.Value)
			}

			for _, r := range fd.ReturnTypes {
				fnType.ReturnTypes = append(fnType.ReturnTypes, ctx.resolveTypeExpr(r))
			}

			// 基本名で登録
			ctx.Functions[fd.Name.Value] = fnType

			// レシーバ付きメソッドの場合、多様な組み合わせでエイリアス登録
			if fd.Receiver != nil {
				recvTypeName := ""
				if pt, ok := fd.Receiver.Type.(*ast.PointerType); ok {
					if named, ok := pt.Base.(*ast.NamedType); ok {
						recvTypeName = named.Name.Value
					}
				} else if named, ok := fd.Receiver.Type.(*ast.NamedType); ok {
					recvTypeName = named.Name.Value
				}

				if recvTypeName != "" {
					ctx.Functions[recvTypeName+"_"+fd.Name.Value] = fnType
					if strings.Contains(recvTypeName, "_") {
						parts := strings.SplitN(recvTypeName, "_", 2)
						ctx.Functions[parts[1]+"_"+fd.Name.Value] = fnType
					}
					if strings.Contains(fd.Name.Value, "_") {
						parts := strings.SplitN(fd.Name.Value, "_", 2)
						ctx.Functions[recvTypeName+"_"+parts[1]] = fnType
						ctx.Functions[parts[0]+"_"+recvTypeName+"_"+parts[1]] = fnType
					}
				}
			}
		}
	}

	if len(ctx.Errors) > 0 {
		return nil, fmt.Errorf("semantic error: %s", ctx.Errors[0])
	}

	return ctx, nil
}

func (ctx *Context) resolveTypeExpr(expr ast.TypeExpr) Type {
	if expr == nil {
		return TypeVoid
	}
	switch t := expr.(type) {
	case *ast.NamedType:
		name := t.Name.Value

		// 1. パッケージ修飾付きの探索 (例: ast.Program -> ast_Program)
		if t.Package != nil {
			cand := t.Package.Value + "_" + name
			if st, ok := ctx.Structs[cand]; ok {
				return st
			}
			if st, ok := ctx.Structs[name]; ok {
				return st
			}
		}

		// 2. プリミティブ型
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

		// 3. 構造体完全一致
		if st, ok := ctx.Structs[name]; ok {
			return st
		}

		// 4. サフィックス一致による探索 (例: Program -> ast_Program)
		for sName, st := range ctx.Structs {
			if strings.HasSuffix(sName, "_"+name) || strings.HasSuffix(sName, name) {
				return st
			}
		}

		if t.Package != nil && t.Package.Value == "mem" && t.Name.Value == "Allocator" {
			return &BasicType{Name: "mem.Allocator", ByteSize: 16, LLVM: "%struct.Allocator"}
		}
		return &BasicType{Name: name, ByteSize: 8, LLVM: "%struct." + name}

	case *ast.PointerType:
		base := ctx.resolveTypeExpr(t.Base)
		return &PointerType{Base: base}
	}
	return TypeInt
}

func (ctx *Context) LookupField(st *StructType, fieldName string) (*StructField, error) {
	field, ok := st.FieldMap[fieldName]
	if !ok {
		return nil, fmt.Errorf("struct %s has no field named %s", st.Name, fieldName)
	}
	return &field, nil
}
