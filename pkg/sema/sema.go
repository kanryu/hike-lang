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

type ArrayType struct {
	Len  int
	Elem Type
}

func (a *ArrayType) TypeName() string { return fmt.Sprintf("[%d]%s", a.Len, a.Elem.TypeName()) }
func (a *ArrayType) LLVMType() string { return fmt.Sprintf("[%d x %s]", a.Len, a.Elem.LLVMType()) }
func (a *ArrayType) Size() int        { return a.Len * a.Elem.Size() }

type Field struct {
	Name       string
	Type       Type
	IsEmbedded bool
}

type StructType struct {
	Name   string
	Fields []Field
}

func (s *StructType) TypeName() string { return s.Name }
func (s *StructType) LLVMType() string { return "%struct." + s.Name }
func (s *StructType) Size() int {
	total := 0
	for _, f := range s.Fields {
		total += f.Type.Size()
	}
	return total
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

func (i *InterfaceType) IsAny() bool {
	return len(i.Methods) == 0
}

func (i *InterfaceType) TypeName() string {
	if i.Name != "" {
		return i.Name
	}
	return "interface{}"
}

func (i *InterfaceType) LLVMType() string {
	if i.IsAny() {
		return "{ i8*, i64 }"
	}
	return "{ i8*, i8* }"
}

func (i *InterfaceType) Size() int { return 16 }

type TupleType struct {
	Types []Type
}

func (t *TupleType) TypeName() string {
	names := []string{}
	for _, elem := range t.Types {
		names = append(names, elem.TypeName())
	}
	return "(" + strings.Join(names, ", ") + ")"
}

func (t *TupleType) LLVMType() string {
	types := []string{}
	for _, elem := range t.Types {
		types = append(types, elem.LLVMType())
	}
	return "{ " + strings.Join(types, ", ") + " }"
}

func (t *TupleType) Size() int {
	total := 0
	for _, elem := range t.Types {
		total += elem.Size()
	}
	return total
}

type FuncType struct {
	Name        string
	ParamTypes  []Type
	ReturnTypes []Type
	IsVariadic  bool
	IsExtern    bool
}

func (f *FuncType) TypeName() string {
	paramNames := []string{}
	for _, p := range f.ParamTypes {
		paramNames = append(paramNames, p.TypeName())
	}
	retNames := []string{}
	for _, r := range f.ReturnTypes {
		retNames = append(retNames, r.TypeName())
	}
	retStr := strings.Join(retNames, ", ")
	if len(retNames) > 1 {
		retStr = "(" + retStr + ")"
	}
	return fmt.Sprintf("func(%s) %s", strings.Join(paramNames, ", "), retStr)
}

func (f *FuncType) LLVMType() string {
	return "{ i8*, i8* }"
}

func (f *FuncType) Size() int { return 16 }

type Context struct {
	Functions  map[string]*FuncType
	Structs    map[string]*StructType
	Interfaces map[string]*InterfaceType
	Aliases    map[string]Type
	Constants  map[string]int64
	Globals    map[string]Type
	typeIDs    map[string]int64
	nextID     int64
}

func NewContext() *Context {
	ctx := &Context{
		Functions:  make(map[string]*FuncType),
		Structs:    make(map[string]*StructType),
		Interfaces: make(map[string]*InterfaceType),
		Aliases:    make(map[string]Type),
		Constants:  make(map[string]int64),
		Globals:    make(map[string]Type),
		typeIDs:    make(map[string]int64),
		nextID:     1,
	}

	ctx.GetTypeID(TypeInt)
	ctx.GetTypeID(TypeByte)
	ctx.GetTypeID(TypeBool)
	ctx.GetTypeID(TypeString)

	ctx.Aliases["any"] = &InterfaceType{Name: "any"}
	return ctx
}

func (c *Context) GetTypeID(t Type) int64 {
	key := t.TypeName()
	if id, exists := c.typeIDs[key]; exists {
		return id
	}
	id := c.nextID
	c.typeIDs[key] = id
	c.nextID++
	return id
}

func (c *Context) ResolveType(expr ast.TypeExpr) Type {
	if expr == nil {
		return TypeVoid
	}
	switch t := expr.(type) {
	case *ast.NamedType:
		name := t.Name.Value
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

func evalConstExpr(expr ast.Expression, consts map[string]int64) int64 {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return e.Value
	case *ast.IotaExpr:
		return e.Value
	case *ast.Identifier:
		if val, ok := consts[e.Value]; ok {
			return val
		}
		return 0
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
			return 0
		case "<<":
			return left << right
		case ">>":
			return left >> right
		case "&":
			return left & right
		case "|":
			return left | right
		case "^":
			return left ^ right
		}
	}
	return 0
}

func Analyze(prog *ast.Program) (*Context, error) {
	ctx := NewContext()

	for _, decl := range prog.Decls {
		if td, ok := decl.(*ast.TypeDecl); ok {
			if stNode, ok := td.Type.(*ast.StructType); ok {
				st := &StructType{Name: td.Name.Value, Fields: []Field{}}
				ctx.Structs[td.Name.Value] = st
				for _, f := range stNode.Fields {
					st.Fields = append(st.Fields, Field{Name: f.Name.Value, Type: ctx.ResolveType(f.Type), IsEmbedded: f.IsEmbedded})
				}
			} else if itNode, ok := td.Type.(*ast.InterfaceType); ok {
				methods := []Method{}
				for _, m := range itNode.Methods {
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
				iface := &InterfaceType{Name: td.Name.Value, Methods: methods}
				ctx.Interfaces[td.Name.Value] = iface
				ctx.Aliases[td.Name.Value] = iface
			} else {
				ctx.Aliases[td.Name.Value] = ctx.ResolveType(td.Type)
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
		if vd, ok := decl.(*ast.VarDecl); ok {
			var gType Type = TypeInt
			if vd.Type != nil {
				gType = ctx.ResolveType(vd.Type)
			}
			ctx.Globals[vd.Name.Value] = gType
		} else if as, ok := decl.(*ast.AssignStmt); ok {
			for _, lhs := range as.Left {
				if ident, ok := lhs.(*ast.Identifier); ok {
					var gType Type = TypeInt
					if as.Type != nil {
						gType = ctx.ResolveType(as.Type)
					}
					ctx.Globals[ident.Value] = gType
				}
			}
		}
	}

	for _, decl := range prog.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			funcMangledName := fd.Name.Value
			var recvType Type = nil
			if fd.Receiver != nil {
				recvType = ctx.ResolveType(fd.Receiver.Type)
				recvTypeName := ""
				if named, ok := fd.Receiver.Type.(*ast.NamedType); ok {
					recvTypeName = named.Name.Value
				} else if pt, ok := fd.Receiver.Type.(*ast.PointerType); ok {
					if named, ok := pt.Base.(*ast.NamedType); ok {
						recvTypeName = named.Name.Value
					}
				}
				if strings.Contains(funcMangledName, "_") {
					parts := strings.SplitN(funcMangledName, "_", 2)
					funcMangledName = parts[0] + "_" + recvTypeName + "_" + parts[1]
				} else {
					funcMangledName = recvTypeName + "_" + funcMangledName
				}
			}

			fnType := &FuncType{
				Name:        funcMangledName,
				ParamTypes:  []Type{},
				ReturnTypes: []Type{},
				IsVariadic:  fd.IsVariadic,
				IsExtern:    fd.Body == nil,
			}

			// メソッドの場合は第0引数にレシーバ型を登録
			if recvType != nil {
				fnType.ParamTypes = append(fnType.ParamTypes, recvType)
			}

			for _, p := range fd.Params {
				fnType.ParamTypes = append(fnType.ParamTypes, ctx.ResolveType(p.Type))
			}
			for _, rt := range fd.ReturnTypes {
				fnType.ReturnTypes = append(fnType.ReturnTypes, ctx.ResolveType(rt))
			}
			ctx.Functions[funcMangledName] = fnType
			if fd.Receiver == nil {
				ctx.Functions[fd.Name.Value] = fnType
			}
		}
	}

	return ctx, nil
}
