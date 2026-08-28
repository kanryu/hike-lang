package sema

import (
	"fmt"
	"hikec-go/pkg/ast"
)

// 型表現インターフェース
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
	TypeBool  = &BasicType{Name: "bool", ByteSize: 1, LLVM: "i1"} // 追加
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
	Name         string // ソース上の型名（例: "Node", "MyType"）
	ResolvedType Type   // 型解決フェーズでセットする実体（未解決時はnil）
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
	return 8 // 未解決時/ポインタサイズのデフォルト
}

func (t *NamedType) LLVMType() string {
	if t.ResolvedType != nil {
		return t.ResolvedType.LLVMType()
	}
	return "%struct." + t.Name
}

// セマンティクス解析結果コンテキスト
type Context struct {
	Structs   map[string]*StructType
	Functions map[string]*FuncType
	Constants map[string]int64 // 定数テーブル
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
					if prog.Package != "" && prog.Package != "main" {
						ctx.Constants[prog.Package+"_"+cd.Name.Value] = intLit.Value
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

	// 1-1. 全構造体名を先行登録（自己参照 *Node を解決可能にする）
	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if _, ok := td.Type.(*ast.StructType); ok {
				ctx.Structs[td.Name.Value] = &StructType{
					Name:     td.Name.Value,
					Fields:   []StructField{},
					FieldMap: make(map[string]StructField),
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

			ctx.Functions[fd.Name.Value] = fnType
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
		if t.Name.Value == "int" {
			return TypeInt
		}
		if t.Name.Value == "byte" {
			return TypeByte
		}
		if t.Name.Value == "bool" { // 追加
			return TypeBool
		}
		if t.Name.Value == "error" {
			return TypeError
		}
		if st, ok := ctx.Structs[t.Name.Value]; ok {
			return st
		}
		if t.Package != nil && t.Package.Value == "mem" && t.Name.Value == "Allocator" {
			return &BasicType{Name: "mem.Allocator", ByteSize: 16, LLVM: "%struct.Allocator"}
		}
	case *ast.PointerType:
		base := ctx.resolveTypeExpr(t.Base)
		return &PointerType{Base: base}
	}
	return TypeInt
}

// 構造体型から指定されたフィールドの情報を取得する
func (ctx *Context) LookupField(st *StructType, fieldName string) (*StructField, error) {
	field, ok := st.FieldMap[fieldName]
	if !ok {
		return nil, fmt.Errorf("struct %s has no field named %s", st.Name, fieldName)
	}
	return &field, nil
}
