package sema

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/token"
)

// -----------------------------------------------------------------------------
// 型システム基本定義
// -----------------------------------------------------------------------------

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
	PointerSize = 8

	TypeInt     = &BasicType{Name: "int", ByteSize: 8, LLVM: "i64"}
	TypeInt64   = &BasicType{Name: "int64", ByteSize: 8, LLVM: "i64"}
	TypeInt32   = &BasicType{Name: "int32", ByteSize: 4, LLVM: "i32"}
	TypeInt16   = &BasicType{Name: "int16", ByteSize: 2, LLVM: "i16"}
	TypeInt8    = &BasicType{Name: "int8", ByteSize: 1, LLVM: "i8"}
	TypeUint    = &BasicType{Name: "uint", ByteSize: 8, LLVM: "i64"}
	TypeUint64  = &BasicType{Name: "uint64", ByteSize: 8, LLVM: "i64"}
	TypeUint32  = &BasicType{Name: "uint32", ByteSize: 4, LLVM: "i32"}
	TypeUint16  = &BasicType{Name: "uint16", ByteSize: 2, LLVM: "i16"}
	TypeUint8   = &BasicType{Name: "uint8", ByteSize: 1, LLVM: "i8"}
	TypeUintptr = &BasicType{Name: "uintptr", ByteSize: 8, LLVM: "i64"}
	TypeByte    = &BasicType{Name: "byte", ByteSize: 1, LLVM: "i8"}
	TypeBool    = &BasicType{Name: "bool", ByteSize: 1, LLVM: "i1"}
	TypeFloat32 = &BasicType{Name: "float32", ByteSize: 4, LLVM: "float"}
	TypeFloat64 = &BasicType{Name: "float64", ByteSize: 8, LLVM: "double"}
	TypeString  = &BasicType{Name: "string", ByteSize: 8, LLVM: "i8*"}
	TypeCString = &BasicType{Name: "cstring", ByteSize: 8, LLVM: "i8*"}
	TypeVoid    = &BasicType{Name: "void", ByteSize: 0, LLVM: "void"}
)

var BuiltinTypes = map[string]Type{
	"int":     TypeInt,
	"int64":   TypeInt64,
	"int32":   TypeInt32,
	"int16":   TypeInt16,
	"int8":    TypeInt8,
	"uint":    TypeUint,
	"uint64":  TypeUint64,
	"uint32":  TypeUint32,
	"uint16":  TypeUint16,
	"uint8":   TypeUint8,
	"uintptr": TypeUintptr,
	"byte":    TypeByte,
	"float":   TypeFloat64,
	"float32": TypeFloat32,
	"float64": TypeFloat64,
	"bool":    TypeBool,
	"string":  TypeString,
	"cstring": TypeCString,
	"void":    TypeVoid,
}

// SetTargetArchitecture はターゲットアーキテクチャに応じて基本型の幅（32bit / 64bit）を設定する
func SetTargetArchitecture(arch string) {
	arch = strings.ToLower(arch)
	if arch == "wasm32" || arch == "386" || arch == "arm" || strings.HasPrefix(arch, "wasm32") {
		TypeInt.ByteSize = 4
		TypeInt.LLVM = "i32"
		TypeUint.ByteSize = 4
		TypeUint.LLVM = "i32"
		TypeUintptr.ByteSize = 4
		TypeUintptr.LLVM = "i32"
		TypeString.ByteSize = 4
		TypeCString.ByteSize = 4
		PointerSize = 4
	} else {
		TypeInt.ByteSize = 8
		TypeInt.LLVM = "i64"
		TypeUint.ByteSize = 8
		TypeUint.LLVM = "i64"
		TypeUintptr.ByteSize = 8
		TypeUintptr.LLVM = "i64"
		TypeString.ByteSize = 8
		TypeCString.ByteSize = 8
		PointerSize = 8
	}
}

func IsBuiltinType(name string) bool {
	if _, ok := BuiltinTypes[name]; ok {
		return true
	}
	return name == "any" || name == "error"
}

func LookupBuiltinType(name string) (Type, bool) {
	t, ok := BuiltinTypes[name]
	return t, ok
}

type TypeParamType struct {
	Name string
}

func (t *TypeParamType) TypeName() string { return t.Name }
func (t *TypeParamType) LLVMType() string { return "i8*" }
func (t *TypeParamType) Size() int        { return PointerSize }

type PointerType struct {
	Base Type
}

func (t *PointerType) TypeName() string { return "*" + t.Base.TypeName() }
func (t *PointerType) LLVMType() string { return t.Base.LLVMType() + "*" }
func (t *PointerType) Size() int        { return PointerSize }

type SliceType struct {
	Elem Type
}

func (t *SliceType) TypeName() string { return "[]" + t.Elem.TypeName() }
func (t *SliceType) LLVMType() string {
	return fmt.Sprintf("{ i8*, %s, %s }", TypeInt.LLVMType(), TypeInt.LLVMType())
}
func (t *SliceType) Size() int { return PointerSize + TypeInt.Size()*2 }

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
	InternalKey     string
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
			fsz = PointerSize
		}
		sz += fsz
	}
	if sz == 0 {
		return PointerSize
	}
	return sz
}

type Method struct {
	Name         string
	InternalKey  string
	ParamTypes   []Type
	IsVariadic   bool
	VariadicElem Type
	ReturnTypes  []Type
}

type InterfaceType struct {
	Name            string
	InternalKey     string
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
		return fmt.Sprintf("{ i8*, %s }", TypeInt.LLVMType())
	}
	return "{ i8*, i8* }"
}

func (t *InterfaceType) Size() int       { return PointerSize * 2 }
func (t *InterfaceType) IsAny() bool     { return len(t.Methods) == 0 }
func (t *InterfaceType) IsGeneric() bool { return len(t.TypeParams) > 0 && !t.IsSpecialized }

// GetMethod はメソッド名から定義情報と itab スロット番号（0始まり）を返す
func (t *InterfaceType) GetMethod(name string) (*Method, int) {
	for i := range t.Methods {
		if t.Methods[i].Name == name {
			return &t.Methods[i], i
		}
	}
	return nil, -1
}

// HasMethod は指定された名称のメソッドがインターフェースに存在するかを返す
func (t *InterfaceType) HasMethod(name string) bool {
	_, idx := t.GetMethod(name)
	return idx != -1
}

type FuncType struct {
	Name            string
	InternalKey     string
	IRName          string
	TypeParams      []string
	TypeArgs        []Type
	IsMethod        bool
	ParamTypes      []Type
	ReturnTypes     []Type
	IsVariadic      bool
	VariadicElem    Type
	IsExtern        bool
	Template        *ast.FuncDecl
	IsSpecialized   bool
	SpecializedAst  *ast.FuncDecl
	Emitted         bool
	Specializations map[string]*FuncType

	IsCFunc     bool
	CFuncTarget string
	CFuncAst    *ast.CFuncDecl
}

func (t *FuncType) TypeName() string { return "func" }
func (t *FuncType) LLVMType() string { return "{ i8*, i8* }" }
func (t *FuncType) Size() int        { return PointerSize * 2 }
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
	return PointerSize
}

type ChanType struct {
	Elem Type
}

func (t *ChanType) TypeName() string { return "chan " + t.Elem.TypeName() }
func (t *ChanType) LLVMType() string { return "i8*" }
func (t *ChanType) Size() int        { return PointerSize }

type FutureType struct {
	ReturnTypes []Type
}

func (t *FutureType) TypeName() string {
	types := make([]string, len(t.ReturnTypes))
	for i, rt := range t.ReturnTypes {
		types[i] = rt.TypeName()
	}
	return fmt.Sprintf("future<(%s)>", strings.Join(types, ", "))
}
func (t *FutureType) LLVMType() string { return "i8*" }
func (t *FutureType) Size() int        { return PointerSize }

// -------------------------------------------------------------
// 内部シンボルキー生成 & マングリング変換ヘルパー
// -------------------------------------------------------------

func BuildInternalKey(pkg string, ident string, structName string) string {
	base := ident
	if pkg != "" {
		base = pkg + "." + ident
	}
	if structName != "" {
		return base + "@" + structName
	}
	return base
}

func MangleInternalKeyToIR(key string) string {
	if key == "" {
		return ""
	}
	if strings.Contains(key, "@") {
		parts := strings.SplitN(key, "@", 2)
		fnPart := parts[0]
		structPart := parts[1]

		isPtr := strings.HasPrefix(structPart, "*")
		rawStruct := strings.TrimPrefix(structPart, "*")

		var pkg, fn string
		if dot := strings.LastIndex(fnPart, "."); dot != -1 {
			pkg = fnPart[:dot]
			fn = fnPart[dot+1:]
		} else {
			fn = fnPart
		}

		ptrSuffix := ""
		if isPtr {
			ptrSuffix = "_ptr"
		}

		if pkg != "" && pkg != "main" {
			return fmt.Sprintf("%s_%s%s_%s", pkg, rawStruct, ptrSuffix, fn)
		}
		return fmt.Sprintf("%s%s_%s", rawStruct, ptrSuffix, fn)
	}

	clean := strings.ReplaceAll(key, ".", "_")
	clean = strings.ReplaceAll(clean, "*", "_ptr")
	clean = strings.ReplaceAll(clean, "(", "")
	clean = strings.ReplaceAll(clean, ")", "")
	return clean
}

func CanonicalMethodName(recvTypeName, fnName string) string {
	rawRecv := strings.TrimPrefix(recvTypeName, "*")
	cleanMethod := fnName

	if strings.HasPrefix(cleanMethod, rawRecv+"_") {
		cleanMethod = strings.TrimPrefix(cleanMethod, rawRecv+"_")
	}

	if strings.Contains(rawRecv, "_") {
		pkg := strings.Split(rawRecv, "_")[0]
		if strings.HasPrefix(cleanMethod, pkg+"_") {
			cleanMethod = strings.TrimPrefix(cleanMethod, pkg+"_")
		}
	} else if idx := strings.LastIndex(cleanMethod, "_"); idx != -1 {
		cleanMethod = cleanMethod[idx+1:]
	}

	return rawRecv + "_" + cleanMethod
}

// -------------------------------------------------------------
// 型ヘルパー関数
// -------------------------------------------------------------

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
	case *ast.EllipsisType:
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
	case *ast.EllipsisType:
		collectTypeParamsFromNode(node.Elem, out)
	case *ast.MapType:
		collectTypeParamsFromNode(node.Key, out)
		collectTypeParamsFromNode(node.Value, out)
	case *ast.NamedType:
		for _, ta := range node.TypeArgs {
			collectTypeParamsFromNode(ta, out)
		}
		name := node.Name.Value
		if IsBuiltinType(name) {
			return
		}
		if len(name) <= 2 && node.Package == nil {
			out[name] = true
		}
	}
}

func typeToTypeExpr(t Type) ast.TypeExpr {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case *PointerType:
		return &ast.PointerType{Base: typeToTypeExpr(v.Base)}
	case *SliceType:
		return &ast.SliceType{Elem: typeToTypeExpr(v.Elem)}
	case *ArrayType:
		return &ast.ArrayType{Len: int64(v.Len), Elem: typeToTypeExpr(v.Elem)}
	case *MapType:
		return &ast.MapType{Key: typeToTypeExpr(v.Key), Value: typeToTypeExpr(v.Value)}
	default:
		return &ast.NamedType{Name: &ast.Identifier{Value: v.TypeName()}}
	}
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

	intRank := func(llvm string) int {
		switch llvm {
		case "i64":
			return 64
		case "i32":
			return 32
		case "i16":
			return 16
		case "i8":
			return 8
		case "i1":
			return 1
		default:
			return 0
		}
	}

	rFrom := intRank(fromLLVM)
	rTo := intRank(toLLVM)

	// 浮動小数点数と整数の相互変換
	if (fromLLVM == "double" || fromLLVM == "float") && rTo > 0 {
		return ast.CastFloatToInt, true
	}
	if rFrom > 0 && (toLLVM == "double" || toLLVM == "float") {
		return ast.CastIntToFloat, true
	}

	// 整数間の拡縮 (Trunc / ZExt)
	if rFrom > 0 && rTo > 0 {
		if rFrom > rTo {
			return ast.CastTrunc, true
		}
		if rFrom < rTo {
			return ast.CastZExt, true
		}
	}

	// ポインタ同士の変換
	isFromPtr := strings.HasSuffix(fromLLVM, "*")
	isToPtr := strings.HasSuffix(toLLVM, "*")
	if isFromPtr && isToPtr {
		return ast.CastBitcast, true
	}

	// ポインタと整数の相互変換 (ptrtoint / inttoptr)
	if isFromPtr && rTo > 0 {
		return ast.CastPtrToInt, true
	}
	if rFrom > 0 && isToPtr {
		return ast.CastIntToPtr, true
	}

	return 0, false
}

func isTypeParamExpr(t ast.TypeExpr) bool {
	if t == nil {
		return false
	}
	switch node := t.(type) {
	case *ast.NamedType:
		if node.Package == nil && len(node.TypeArgs) == 0 {
			name := node.Name.Value
			if IsBuiltinType(name) {
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
	case *ast.EllipsisType:
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

// validateDefaultParams はデフォルト引数が末尾から連続しているかを検証する
func validateDefaultParams(params []*ast.ParamDecl) error {
	hasDefault := false
	for _, p := range params {
		if p.Default != nil {
			hasDefault = true
		} else if hasDefault {
			paramName := ""
			if p.Name != nil {
				paramName = p.Name.Value
			}
			return fmt.Errorf("line %d:%d: non-default parameter '%s' follows default parameter; default arguments must be trailing",
				p.Token.Line, p.Token.Col, paramName)
		}
	}
	return nil
}

// -------------------------------------------------------------
// 意味解析メインパイプライン (Analyze)
// -------------------------------------------------------------
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

			internalKey := BuildInternalKey(prog.Package, td.Name.Value, "")
			td.InternalKey = internalKey

			if _, ok := td.Type.(*ast.InterfaceType); ok {
				iface := &InterfaceType{
					Name:            td.Name.Value,
					InternalKey:     internalKey,
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
					InternalKey:     internalKey,
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
			} else {
				resolvedAlias := ctx.ResolveType(td.Type)
				ctx.Aliases[td.Name.Value] = resolvedAlias
			}
		} else if fd, ok := decl.(*ast.FuncDecl); ok {
			// デフォルト引数の末尾規則検証
			if err := validateDefaultParams(fd.Params); err != nil {
				return nil, err
			}

			fnName := fd.Name.Value
			isMethod := (fd.Receiver != nil)
			tpSet := make(map[string]bool)
			for _, tp := range fd.TypeParams {
				tpSet[tp.Name.Value] = true
			}

			var origRecvName string = ""
			var structNameWithPtr string = ""
			if fd.Receiver != nil {
				collectTypeParamsFromNode(fd.Receiver.Type, tpSet)
				origRecvName = getBaseTypeName(fd.Receiver.Type)
				recvTypeName := origRecvName
				if st, canonical := ctx.LookupStruct(recvTypeName); st != nil {
					recvTypeName = canonical
				} else if alias, _ := ctx.LookupAlias(recvTypeName); alias != nil {
					recvTypeName = alias.TypeName()
				}
				if recvTypeName != "" {
					fnName = CanonicalMethodName(recvTypeName, fnName)
				}
				structNameWithPtr = recvTypeName
				if _, isPtr := fd.Receiver.Type.(*ast.PointerType); isPtr {
					structNameWithPtr = "*" + recvTypeName
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

			internalKey := BuildInternalKey(prog.Package, fd.Name.Value, structNameWithPtr)
			fd.InternalKey = internalKey

			// IRName はコンパイルされる実体関数名 fnName と一致させる
			irName := fnName
			if !isMethod && (prog.Package == "" || prog.Package == "main") {
				irName = fd.Name.Value
			}

			fnType := &FuncType{
				Name:            fnName,
				InternalKey:     internalKey,
				IRName:          irName,
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
			if origRecvName != "" {
				aliasMethodName := CanonicalMethodName(origRecvName, fd.Name.Value)
				if aliasMethodName != fnName {
					ctx.Functions[aliasMethodName] = fnType
				}
			}
			if len(tParams) > 0 {
				ctx.GenericFuncs[fnName] = fd
				ctx.GenericFuncs[fd.Name.Value] = fd
			}
		} else if efd, ok := decl.(*ast.ExternFuncDecl); ok {
			if err := validateDefaultParams(efd.Params); err != nil {
				return nil, err
			}
			cName := efd.Name.Value
			if efd.TargetCName != nil {
				cName = efd.TargetCName.Value
			}
			internalKey := BuildInternalKey("", efd.Name.Value, "")
			efd.InternalKey = internalKey
			fnType := &FuncType{
				Name:            efd.Name.Value,
				InternalKey:     internalKey,
				IRName:          cName,
				ParamTypes:      []Type{},
				ReturnTypes:     []Type{},
				IsVariadic:      efd.IsVariadic,
				IsExtern:        true,
				Specializations: make(map[string]*FuncType),
			}
			ctx.Functions[efd.Name.Value] = fnType
		} else if jfd, ok := decl.(*ast.JFuncDecl); ok {
			if err := validateDefaultParams(jfd.Params); err != nil {
				return nil, err
			}
			jsBridgeName := "__hike_js_" + jfd.Name.Value
			internalKey := BuildInternalKey("", jfd.Name.Value, "")
			jfd.InternalKey = internalKey
			fnType := &FuncType{
				Name:            jfd.Name.Value,
				InternalKey:     internalKey,
				IRName:          jsBridgeName,
				ParamTypes:      []Type{},
				ReturnTypes:     []Type{},
				IsExtern:        true,
				Specializations: make(map[string]*FuncType),
			}
			ctx.Functions[jfd.Name.Value] = fnType
		} else if cfd, ok := decl.(*ast.CFuncDecl); ok {
			if err := validateDefaultParams(cfd.Params); err != nil {
				return nil, err
			}
			targetC := ""
			if cfd.TargetCName != nil {
				targetC = cfd.TargetCName.Value
			} else {
				targetC = cfd.Name.Value
			}
			internalKey := BuildInternalKey(prog.Package, cfd.Name.Value, "")
			cfd.InternalKey = internalKey

			irName := cfd.Name.Value
			if !cfd.IsAlias() {
				irName = "__hike_impl_" + cfd.Name.Value
			}

			fnType := &FuncType{
				Name:            cfd.Name.Value,
				InternalKey:     internalKey,
				IRName:          irName,
				ParamTypes:      []Type{},
				ReturnTypes:     []Type{},
				IsVariadic:      cfd.IsVariadic,
				IsCFunc:         true,
				CFuncTarget:     targetC,
				CFuncAst:        cfd,
				Specializations: make(map[string]*FuncType),
			}
			ctx.Functions[cfd.Name.Value] = fnType
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
					var varElem Type = nil
					for i, p := range m.ParamTypes {
						resolved := ctx.ResolveType(p)
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
						rts = append(rts, ctx.ResolveType(r))
					}
					methodKey := BuildInternalKey(prog.Package, m.Name.Value, td.Name.Value)
					methods = append(methods, Method{
						Name:         m.Name.Value,
						InternalKey:  methodKey,
						ParamTypes:   pts,
						IsVariadic:   m.IsVariadic,
						VariadicElem: varElem,
						ReturnTypes:  rts,
					})
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
	unresolvedConsts := []*ast.ConstDecl{}
	for _, decl := range prog.Decls {
		if cd, ok := decl.(*ast.ConstDecl); ok {
			unresolvedConsts = append(unresolvedConsts, cd)
		}
	}

	for len(unresolvedConsts) > 0 {
		progress := false
		remaining := []*ast.ConstDecl{}
		for _, cd := range unresolvedConsts {
			if iVal, ok := ctx.evalConstInt(cd.Value); ok {
				ctx.Constants[cd.Name.Value] = iVal
				progress = true
			} else if fVal, ok := ctx.evalConstFloat(cd.Value); ok {
				ctx.FloatConstants[cd.Name.Value] = fVal
				progress = true
			} else {
				remaining = append(remaining, cd)
			}
		}
		if !progress {
			break
		}
		unresolvedConsts = remaining
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.VarDecl:
			var gType Type = TypeInt
			if d.Type != nil {
				gType = ctx.ResolveType(d.Type)
			}
			ctx.Globals[d.Name.Value] = gType

		case *ast.FuncDecl:
			// 1. ジェネリック関数・メソッド宣言は Pass 2 での具象型解決をスキップ
			if IsGenericFuncDecl(d) {
				continue
			}

			fnName := d.Name.Value
			if d.Receiver != nil {
				// ResolveType を直接呼ばずに型名（"Map" など）を取得
				recvTypeName := getBaseTypeName(d.Receiver.Type)
				if st, canonical := ctx.LookupStruct(recvTypeName); st != nil {
					if st.IsGeneric() {
						continue
					}
					recvTypeName = canonical
				} else if alias, _ := ctx.LookupAlias(recvTypeName); alias != nil {
					recvTypeName = alias.TypeName()
				}
				if recvTypeName != "" {
					fnName = CanonicalMethodName(recvTypeName, fnName)
				}
			}

			fnType := ctx.Functions[fnName]
			if fnType == nil {
				fnType = ctx.Functions[d.Name.Value]
			}
			if fnType == nil || fnType.IsGeneric() {
				continue
			}

			isMethod := (d.Receiver != nil)
			paramTypes := []Type{}

			// ここに到達するのは非ジェネリックな具象メソッドのみなので安全に ResolveType できる
			if d.Receiver != nil {
				recvType := ctx.ResolveType(d.Receiver.Type)
				paramTypes = append(paramTypes, recvType)
			}

			var variadicElem Type = nil
			for _, p := range d.Params {
				pType := ctx.ResolveType(p.Type)
				if p.IsVariadic {
					if _, isSlice := pType.(*SliceType); !isSlice {
						pType = &SliceType{Elem: pType}
					}
					variadicElem = pType.(*SliceType).Elem
				}
				paramTypes = append(paramTypes, pType)
			}

			returnTypes := []Type{}
			for _, rt := range d.ReturnTypes {
				returnTypes = append(returnTypes, ctx.ResolveType(rt))
			}

			fnType.IsMethod = isMethod
			fnType.ParamTypes = paramTypes
			fnType.ReturnTypes = returnTypes
			fnType.IsVariadic = d.IsVariadic
			fnType.VariadicElem = variadicElem

		case *ast.ExternFuncDecl:
			fnType := ctx.Functions[d.Name.Value]
			if fnType == nil {
				continue
			}
			paramTypes := []Type{}
			var variadicElem Type = nil
			for _, p := range d.Params {
				pType := ctx.ResolveType(p.Type)
				if p.IsVariadic {
					if _, isSlice := pType.(*SliceType); !isSlice {
						pType = &SliceType{Elem: pType}
					}
					variadicElem = pType.(*SliceType).Elem
				}
				paramTypes = append(paramTypes, pType)
			}
			returnTypes := []Type{}
			for _, rt := range d.ReturnTypes {
				returnTypes = append(returnTypes, ctx.ResolveType(rt))
			}
			fnType.ParamTypes = paramTypes
			fnType.ReturnTypes = returnTypes
			fnType.IsVariadic = d.IsVariadic
			fnType.VariadicElem = variadicElem

		case *ast.JFuncDecl:
			fnType := ctx.Functions[d.Name.Value]
			if fnType == nil {
				continue
			}
			paramTypes := []Type{}
			for _, p := range d.Params {
				paramTypes = append(paramTypes, ctx.ResolveType(p.Type))
			}
			returnTypes := []Type{}
			for _, rt := range d.ReturnTypes {
				returnTypes = append(returnTypes, ctx.ResolveType(rt))
			}
			fnType.ParamTypes = paramTypes
			fnType.ReturnTypes = returnTypes

		case *ast.CFuncDecl:
			fnType := ctx.Functions[d.Name.Value]
			if fnType == nil {
				continue
			}
			paramTypes := []Type{}
			for _, p := range d.Params {
				paramTypes = append(paramTypes, ctx.ResolveType(p.Type))
			}
			returnTypes := []Type{}
			for _, rt := range d.ReturnTypes {
				returnTypes = append(returnTypes, ctx.ResolveType(rt))
			}
			fnType.ParamTypes = paramTypes
			fnType.ReturnTypes = returnTypes
			fnType.IsVariadic = d.IsVariadic
		}
	}

	// Pass 3: エスケープ解析
	runEscapeAnalysis(prog)

	// Pass 4: 暗黙キャスト挿入
	insertImplicitCasts(prog, ctx)

	return ctx, nil
}

// -------------------------------------------------------------
// エスケープ解析 & 暗黙キャスト挿入
// -------------------------------------------------------------

func runEscapeAnalysis(prog *ast.Program) {
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			varDecls := make(map[string]*ast.VarDecl)
			paramDecls := make(map[string]*ast.ParamDecl)

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
		} else if cfd, ok := decl.(*ast.CFuncDecl); ok && cfd.Body != nil {
			varDecls := make(map[string]*ast.VarDecl)
			paramDecls := make(map[string]*ast.ParamDecl)

			for _, p := range cfd.Params {
				paramDecls[p.Name.Value] = p
			}

			collectDeclsInBlock(cfd.Body, varDecls)
			capturedNames := CollectAllCapturesInBlock(cfd.Body)

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
			for _, p := range fl.Params {
				if p.Default != nil {
					walkExpr(p.Default)
				}
			}
			if fl.Body != nil {
				walkStmt(fl.Body)
			}
			return
		}
		switch node := e.(type) {
		case *ast.GenericInstExpr:
			walkExpr(node.Left)
		case *ast.BinaryExpr:
			walkExpr(node.Left)
			walkExpr(node.Right)
		case *ast.PrefixExpr:
			walkExpr(node.Right)
		case *ast.ReceiveExpr:
			walkExpr(node.Expr)
		case *ast.AsyncExpr:
			walkExpr(node.Fn)
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
		case *ast.CharLiteral:
			// 文字リテラルは識別子キャプチャなし
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
		case *ast.SendStmt:
			walkExpr(st.Chan)
			walkExpr(st.Value)
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
					"int", "int64", "int32", "int16", "int8", "uint", "uint64", "uint32", "uint16", "uint8", "uintptr", "byte", "string", "cstring", "bool", "float32", "float64", "void", "any", "error":
					return
				}
				seen[name] = true
				captured = append(captured, name)
			}
		case *ast.GenericInstExpr:
			walkExpr(node.Left)
		case *ast.BinaryExpr:
			walkExpr(node.Left)
			walkExpr(node.Right)
		case *ast.PrefixExpr:
			walkExpr(node.Right)
		case *ast.ReceiveExpr:
			walkExpr(node.Expr)
		case *ast.AsyncExpr:
			walkExpr(node.Fn)
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
		case *ast.CharLiteral:
			// 文字リテラルは識別子キャプチャなし
		case *ast.FuncLit:
			for _, p := range node.Params {
				if p.Default != nil {
					walkExpr(p.Default)
				}
			}
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
		case *ast.SendStmt:
			walkExpr(st.Chan)
			walkExpr(st.Value)
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

	for _, p := range fl.Params {
		if p.Default != nil {
			walkExpr(p.Default)
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
			if IsGenericFuncDecl(fn) {
				continue
			}
			if fn.Receiver != nil {
				recvName := getBaseTypeName(fn.Receiver.Type)
				if st, _ := ctx.LookupStruct(recvName); st != nil && st.IsGeneric() {
					continue
				}
			}

			locals := make(map[string]Type)
			if fn.Receiver != nil {
				locals[fn.Receiver.Name.Value] = ctx.ResolveType(fn.Receiver.Type)
			}
			for _, p := range fn.Params {
				pType := ctx.ResolveType(p.Type)
				locals[p.Name.Value] = pType
				if p.Default != nil {
					p.Default = ctx.CoerceExpr(p.Default, pType, locals)
					insertCastsInExpr(p.Default, locals, ctx)
				}
			}
			insertCastsInBlock(fn.Body, locals, ctx, fn.ReturnTypes)

		} else if cfd, ok := decl.(*ast.CFuncDecl); ok && cfd.Body != nil {
			locals := make(map[string]Type)
			for _, p := range cfd.Params {
				pType := ctx.ResolveType(p.Type)
				locals[p.Name.Value] = pType
				if p.Default != nil {
					p.Default = ctx.CoerceExpr(p.Default, pType, locals)
					insertCastsInExpr(p.Default, locals, ctx)
				}
			}
			insertCastsInBlock(cfd.Body, locals, ctx, cfd.ReturnTypes)
		}
	}
}

func insertCastsInBlock(b *ast.BlockStmt, locals map[string]Type, ctx *Context, retTypes []ast.TypeExpr) {
	if b == nil {
		return
	}

	blockLocals := make(map[string]Type)
	for k, v := range locals {
		blockLocals[k] = v
	}

	for _, stmt := range b.Statements {
		switch s := stmt.(type) {
		case *ast.VarDecl:
			var targetType Type = TypeInt
			if s.Type != nil {
				targetType = ctx.ResolveType(s.Type)
			} else if s.Value != nil {
				targetType = ctx.InferExprType(s.Value, blockLocals)
			}
			blockLocals[s.Name.Value] = targetType

			if s.Value != nil {
				s.Value = ctx.CoerceExpr(s.Value, targetType, blockLocals)
				insertCastsInExpr(s.Value, blockLocals, ctx)
			}

		case *ast.AssignStmt:
			isDefine := (s.Token.Type == token.DEFINE) || (s.Token.Literal == ":=") ||
				(s.Token.Type == token.VAR) || (s.Token.Literal == "var") || (s.Type != nil)

			if isDefine {
				if len(s.Left) > 1 && len(s.Right) == 1 {
					rhsType := ctx.InferExprType(s.Right[0], blockLocals)
					if tup, ok := rhsType.(*TupleType); ok {
						for i, left := range s.Left {
							if i < len(tup.Types) {
								if ident, okIdent := left.(*ast.Identifier); okIdent {
									blockLocals[ident.Value] = tup.Types[i]
								}
							}
						}
						insertCastsInExpr(s.Right[0], blockLocals, ctx)
						break
					}
				}

				for i, left := range s.Left {
					var actualType Type = TypeInt
					if s.Type != nil {
						actualType = ctx.ResolveType(s.Type)
					} else if i < len(s.Right) {
						actualType = ctx.InferExprType(s.Right[i], blockLocals)
					}

					if ident, ok := left.(*ast.Identifier); ok {
						blockLocals[ident.Value] = actualType
					}

					if i < len(s.Right) {
						if s.Type != nil {
							s.Right[i] = ctx.CoerceExpr(s.Right[i], actualType, blockLocals)
						}
						insertCastsInExpr(s.Right[i], blockLocals, ctx)
					}
				}
			} else {
				if len(s.Left) > 1 && len(s.Right) == 1 {
					rhsType := ctx.InferExprType(s.Right[0], blockLocals)
					if _, ok := rhsType.(*TupleType); ok {
						insertCastsInExpr(s.Right[0], blockLocals, ctx)
						for _, l := range s.Left {
							insertCastsInExpr(l, blockLocals, ctx)
						}
						break
					}
				}

				for i, r := range s.Right {
					if i < len(s.Left) {
						targetType := ctx.InferExprType(s.Left[i], blockLocals)
						s.Right[i] = ctx.CoerceExpr(r, targetType, blockLocals)
					}
					insertCastsInExpr(s.Right[i], blockLocals, ctx)
				}
				for _, l := range s.Left {
					insertCastsInExpr(l, blockLocals, ctx)
				}
			}

		case *ast.SendStmt:
			insertCastsInExpr(s.Chan, blockLocals, ctx)
			chanType := ctx.InferExprType(s.Chan, blockLocals)
			if ct, ok := chanType.(*ChanType); ok {
				s.Value = ctx.CoerceExpr(s.Value, ct.Elem, blockLocals)
			}
			insertCastsInExpr(s.Value, blockLocals, ctx)

		case *ast.ReturnStmt:
			// 多値関数の結果をそのまま 1 個の式としてパススルー返却する場合 (return enc.Decode(...))
			if len(s.Values) == 1 && len(retTypes) > 1 {
				insertCastsInExpr(s.Values[0], blockLocals, ctx)
				break
			}
			for i, val := range s.Values {
				if i < len(retTypes) {
					expected := ctx.ResolveType(retTypes[i])
					s.Values[i] = ctx.CoerceExpr(val, expected, blockLocals)
				}
				insertCastsInExpr(s.Values[i], blockLocals, ctx)
			}

		case *ast.ExprStmt:
			insertCastsInExpr(s.Expr, blockLocals, ctx)

		case *ast.IfStmt:
			if s.Init != nil {
				if initBlock, ok := s.Init.(*ast.BlockStmt); ok {
					insertCastsInBlock(initBlock, blockLocals, ctx, retTypes)
				} else {
					insertCastsInBlock(&ast.BlockStmt{Statements: []ast.Statement{s.Init}}, blockLocals, ctx, retTypes)
				}
			}
			insertCastsInExpr(s.Condition, blockLocals, ctx)
			if s.Consequence != nil {
				insertCastsInBlock(s.Consequence, blockLocals, ctx, retTypes)
			}
			if s.Alternative != nil {
				if altBlock, ok := s.Alternative.(*ast.BlockStmt); ok {
					insertCastsInBlock(altBlock, blockLocals, ctx, retTypes)
				} else if altIf, ok := s.Alternative.(*ast.IfStmt); ok {
					insertCastsInBlock(&ast.BlockStmt{Statements: []ast.Statement{altIf}}, blockLocals, ctx, retTypes)
				}
			}

		case *ast.ForStmt:
			if s.Init != nil {
				insertCastsInBlock(&ast.BlockStmt{Statements: []ast.Statement{s.Init}}, blockLocals, ctx, retTypes)
			}
			insertCastsInExpr(s.Cond, blockLocals, ctx)
			if s.Post != nil {
				insertCastsInBlock(&ast.BlockStmt{Statements: []ast.Statement{s.Post}}, blockLocals, ctx, retTypes)
			}
			if s.Body != nil {
				insertCastsInBlock(s.Body, blockLocals, ctx, retTypes)
			}

		case *ast.ForRangeStmt:
			insertCastsInExpr(s.X, blockLocals, ctx)
			if s.Body != nil {
				insertCastsInBlock(s.Body, blockLocals, ctx, retTypes)
			}
		}
	}
}

func insertCastsInExpr(e ast.Expression, locals map[string]Type, ctx *Context) {
	if e == nil {
		return
	}
	switch expr := e.(type) {
	case *ast.GenericInstExpr:
		insertCastsInExpr(expr.Left, locals, ctx)
	case *ast.CallExpr:
		fnType := ctx.InferExprType(expr.Function, locals)
		if ft, ok := fnType.(*FuncType); ok {
			if ft.IsVariadic && !ft.IsCFunc && len(ft.ParamTypes) > 0 {
				fixedCount := len(ft.ParamTypes) - 1
				for i := 0; i < fixedCount && i < len(expr.Args); i++ {
					expr.Args[i] = ctx.CoerceExpr(expr.Args[i], ft.ParamTypes[i], locals)
				}
				if expr.HasEllipsis {
					if len(expr.Args) > fixedCount {
						expr.Args[fixedCount] = ctx.CoerceExpr(expr.Args[fixedCount], ft.ParamTypes[fixedCount], locals)
					}
				} else {
					elemType := ft.VariadicElem
					if elemType == nil {
						if sl, isSl := ft.ParamTypes[fixedCount].(*SliceType); isSl {
							elemType = sl.Elem
						}
					}
					if elemType != nil {
						for i := fixedCount; i < len(expr.Args); i++ {
							expr.Args[i] = ctx.CoerceExpr(expr.Args[i], elemType, locals)
						}
					}
				}
			} else {
				for i, arg := range expr.Args {
					if i < len(ft.ParamTypes) {
						expr.Args[i] = ctx.CoerceExpr(arg, ft.ParamTypes[i], locals)
					}
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
	case *ast.ReceiveExpr:
		insertCastsInExpr(expr.Expr, locals, ctx)
	case *ast.AsyncExpr:
		insertCastsInExpr(expr.Fn, locals, ctx)
	}
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
		if ct, ok := t.(*ast.ChanType); ok {
			return checkType(ct.Elem)
		}
		if pt, ok := t.(*ast.PointerType); ok {
			return checkType(pt.Base)
		}
		if sl, ok := t.(*ast.SliceType); ok {
			return checkType(sl.Elem)
		}
		if el, ok := t.(*ast.EllipsisType); ok {
			return checkType(el.Elem)
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
		case *ast.GenericInstExpr:
			if err := checkExpr(n.Left); err != nil {
				return err
			}
			for _, ta := range n.TypeArgs {
				if err := checkType(ta); err != nil {
					return err
				}
			}
			return nil

		case *ast.FuncLit:
			if err := validateDefaultParams(n.Params); err != nil {
				return err
			}
			for _, p := range n.Params {
				if p.Default != nil {
					if err := checkExpr(p.Default); err != nil {
						return err
					}
				}
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
		case *ast.ReceiveExpr:
			return checkExpr(n.Expr)
		case *ast.AsyncExpr:
			return checkExpr(n.Fn)
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
		case *ast.CharLiteral:
			return nil
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
		case *ast.SendStmt:
			if err := checkExpr(st.Chan); err != nil {
				return err
			}
			return checkExpr(st.Value)
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
			recvName := getBaseTypeName(fd.Receiver.Type)
			if st, _ := ctx.LookupStruct(recvName); st != nil && st.IsGeneric() {
				return nil
			}
			if err := checkType(fd.Receiver.Type); err != nil {
				return err
			}
		}
		for _, p := range fd.Params {
			if p.Default != nil {
				if err := checkExpr(p.Default); err != nil {
					return err
				}
			}
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
	if efd, ok := node.(*ast.ExternFuncDecl); ok {
		for _, p := range efd.Params {
			if p.Default != nil {
				if err := checkExpr(p.Default); err != nil {
					return err
				}
			}
			if err := checkType(p.Type); err != nil {
				return err
			}
		}
		for _, rt := range efd.ReturnTypes {
			if err := checkType(rt); err != nil {
				return err
			}
		}
	}
	if jfd, ok := node.(*ast.JFuncDecl); ok {
		for _, p := range jfd.Params {
			if p.Default != nil {
				if err := checkExpr(p.Default); err != nil {
					return err
				}
			}
			if err := checkType(p.Type); err != nil {
				return err
			}
		}
		for _, rt := range jfd.ReturnTypes {
			if err := checkType(rt); err != nil {
				return err
			}
		}
	}
	if cfd, ok := node.(*ast.CFuncDecl); ok {
		for _, p := range cfd.Params {
			if p.Default != nil {
				if err := checkExpr(p.Default); err != nil {
					return err
				}
			}
			if err := checkType(p.Type); err != nil {
				return err
			}
		}
		for _, rt := range cfd.ReturnTypes {
			if err := checkType(rt); err != nil {
				return err
			}
		}
		if cfd.Body != nil {
			return checkStmt(cfd.Body)
		}
	}
	if vd, ok := node.(*ast.VarDecl); ok {
		return checkStmt(vd)
	}
	return nil
}
