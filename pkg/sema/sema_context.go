package sema

import (
	"fmt"
	"sort"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/diag"
	"hikec-go/pkg/logger"
	"hikec-go/pkg/token"
)

type Context struct {
	Structs    map[string]*StructType
	Interfaces map[string]*InterfaceType
	Functions  map[string]*FuncType
	// Methods is indexed by the complete receiver type and method name.  Method
	// lookup must not use a suffix scan of Functions: two packages can legally
	// define the same method for different receiver types.
	Methods            map[string]*FuncType
	Globals            map[string]Type
	Constants          map[string]int64
	StringConstants    map[string]string
	FloatConstants     map[string]float64
	Aliases            map[string]Type
	GenericTypes       map[string]*ast.TypeDecl
	GenericFuncs       map[string]*ast.FuncDecl
	TypeParams         map[string]*TypeParamType
	typeIDs            map[string]int64
	moduleTypeBases    map[string]int64
	moduleTypeSlots    map[string]int64
	nextModuleTypeBase int64
	nextTypeID         int64
	HasMapImport       bool
	GoHikeMode         bool
	RegionModeEnabled  bool
	diagnosticPackages map[string]bool

	// 呼び出し解決結果キャッシュ: 各 CallExpr がどの確定 FuncType を呼び出すかを 1 対 1 で保持
	ResolvedCalls map[*ast.CallExpr]*FuncType
}

func astIdentifierValue(id *ast.Identifier) string {
	if id == nil {
		return ""
	}
	return id.Value
}

func NewContext() *Context {
	ctx := &Context{
		Structs:            make(map[string]*StructType),
		Interfaces:         make(map[string]*InterfaceType),
		Functions:          make(map[string]*FuncType),
		Methods:            make(map[string]*FuncType),
		Globals:            make(map[string]Type),
		Constants:          make(map[string]int64),
		StringConstants:    make(map[string]string),
		FloatConstants:     make(map[string]float64),
		Aliases:            make(map[string]Type),
		GenericTypes:       make(map[string]*ast.TypeDecl),
		GenericFuncs:       make(map[string]*ast.FuncDecl),
		TypeParams:         make(map[string]*TypeParamType),
		typeIDs:            make(map[string]int64),
		moduleTypeBases:    make(map[string]int64),
		moduleTypeSlots:    make(map[string]int64),
		nextModuleTypeBase: 100,
		nextTypeID:         1,
		ResolvedCalls:      make(map[*ast.CallExpr]*FuncType),
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

func (c *Context) GetTypeID(t Type) int64 {
	if t == nil {
		return 0
	}
	return t.TypeID(c)
}

// typeIDFor is the sole allocator for semantic type IDs. Concrete Type
// implementations delegate here through TypeID so callers never need to
// manipulate the raw ID registry directly.
func (c *Context) typeIDFor(t Type) int64 {
	if t == nil {
		return 0
	}
	name := typeNameOf(t)
	if id, exists := c.typeIDs[name]; exists {
		return id
	}
	// Runtime type IDs are allocated in 100-entry module ranges.  Builtins
	// occupy the legacy 1..99 range; user and imported module types receive a
	// stable block base and a construction-order slot within that block.
	module := typeIDModule(name)
	base, exists := c.moduleTypeBases[module]
	if !exists {
		base = c.nextModuleTypeBase
		c.nextModuleTypeBase += 100
		c.moduleTypeBases[module] = base
	}
	slot := c.moduleTypeSlots[module]
	if slot >= 100 {
		panic(fmt.Sprintf("type ID range exhausted for module %q", module))
	}
	id := base + slot
	c.moduleTypeSlots[module] = slot + 1
	c.typeIDs[name] = id
	return id
}

func typeIDModule(name string) string {
	for len(name) > 0 && (name[0] == '*' || name[0] == '[') {
		name = name[1:]
	}
	if idx := strings.Index(name, "_"); idx > 0 {
		return name[:idx]
	}
	return "main"
}

// -----------------------------------------------------------------------------
// シンボル検索 (Lookup)
// -----------------------------------------------------------------------------

func (c *Context) LookupStruct(name string) (*StructType, string) {
	if st, ok := c.Structs[name]; ok {
		return st, name
	}
	if st, canonical := c.lookupSpecialASTStruct(name); st != nil {
		return st, canonical
	}
	keys := make([]string, 0, len(c.Structs))
	for k := range c.Structs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := c.Structs[k]
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) ||
			strings.HasSuffix(k, "."+name) || strings.HasSuffix(name, "."+k) {
			return v, k
		}
	}
	return nil, ""
}

func (c *Context) lookupSpecialASTStruct(name string) (*StructType, string) {
	// Go-Hike imports both ast.Program and hir.Program. Imported Go-shaped
	// signatures can lose the package qualifier during the reduced type pass;
	// prefer the AST program for those unqualified names.
	for _, astName := range []string{"Program", "ArrayType"} {
		if name != astName {
			continue
		}
		if st, ok := c.Structs["ast_"+astName]; ok {
			return st, "ast_" + astName
		}
	}
	return nil, ""
}

func (c *Context) LookupInterface(name string) (*InterfaceType, string) {
	if iface, ok := c.Interfaces[name]; ok {
		return iface, name
	}
	for k, v := range c.Interfaces {
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) ||
			strings.HasSuffix(k, "."+name) || strings.HasSuffix(name, "."+k) {
			return v, k
		}
	}
	return nil, ""
}

func methodLookupKey(recvTypeName, methodName string) string {
	return recvTypeName + "#" + methodName
}

// RegisterMethod はレシーバーの完全な型名を含むキーでメソッドを登録する。
func (c *Context) RegisterMethod(recvTypeName, methodName string, fn *FuncType) {
	if recvTypeName == "" || methodName == "" || fn == nil {
		return
	}
	c.Methods[methodLookupKey(recvTypeName, methodName)] = fn
}

// LookupMethod はレシーバーの完全な型名とメソッド名で対象メソッドを探索する。
// Functions の suffix 探索は同名メソッドを誤選択するため、ここでは行わない。
func (c *Context) LookupMethod(recvTypeName string, methodName string) (*FuncType, string) {
	// The loader qualifies package types (for example exec_Cmd), while a
	// receiver declaration inside that package may still refer to Cmd. Try the
	// resolved name first, then its package-local spelling, preserving pointer
	// qualification in every candidate.
	for _, candidate := range receiverTypeCandidates(recvTypeName) {
		if fn, ok := c.Methods[methodLookupKey(candidate, methodName)]; ok {
			return fn, fn.InternalKey
		}
	}

	// 旧形式で構築されたコンテキストとの互換性。こちらも完全一致のみ。
	if fn, name := c.lookupLegacyMethod(recvTypeName, methodName); fn != nil {
		return fn, name
	}

	return nil, ""
}

func (c *Context) lookupLegacyMethod(recvTypeName, methodName string) (*FuncType, string) {
	for _, candidate := range receiverTypeCandidates(recvTypeName) {
		isPtr := strings.HasPrefix(candidate, "*")
		rawRecv := strings.TrimPrefix(candidate, "*")
		legacyName := CanonicalMethodName(rawRecv, methodName)
		if fn, ok := c.Functions[legacyName]; ok {
			return fn, legacyName
		}
		if isPtr {
			ptrLegacy := CanonicalMethodName(rawRecv+"_ptr", methodName)
			if fn, ok := c.Functions[ptrLegacy]; ok {
				return fn, ptrLegacy
			}
		}
	}
	return nil, ""
}

func receiverTypeCandidates(recvTypeName string) []string {
	candidates := []string{recvTypeName}
	isPtr := strings.HasPrefix(recvTypeName, "*")
	raw := strings.TrimPrefix(recvTypeName, "*")
	if idx := strings.LastIndex(raw, "_"); idx >= 0 && idx+1 < len(raw) {
		short := raw[idx+1:]
		if isPtr {
			short = "*" + short
		}
		candidates = append(candidates, short)
	}
	return candidates
}

func (c *Context) LookupFunction(name string) (*FuncType, string) {
	if fn, ok := c.Functions[name]; ok {
		return fn, name
	}
	keys := make([]string, 0, len(c.Functions))
	for k := range c.Functions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := c.Functions[k]
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
			return v, k
		}
		if strings.Contains(k, "@") {
			parts := strings.SplitN(k, "@", 2)
			fnPart := parts[0]
			if fnPart == name || strings.HasSuffix(fnPart, "/"+name) || strings.HasSuffix(fnPart, "."+name) {
				return v, k
			}
		}
	}
	return nil, ""
}

func (c *Context) LookupAlias(name string) (Type, string) {
	if a, ok := c.Aliases[name]; ok {
		return a, name
	}
	keys := make([]string, 0, len(c.Aliases))
	for k := range c.Aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := c.Aliases[k]
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

func (c *Context) LookupStringConstant(name string) (string, bool) {
	if val, ok := c.StringConstants[name]; ok {
		return val, true
	}
	for k, v := range c.StringConstants {
		if k == name || strings.HasSuffix(k, "_"+name) || strings.HasSuffix(name, "_"+k) {
			return v, true
		}
	}
	return "", false
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

// RegisterTypeDecl は AST の TypeDecl を走査し、型マップ（Structs, GenericTypes 等）へ先行登録する
func (c *Context) RegisterTypeDecl(td *ast.TypeDecl, pkgName string) {
	if td == nil || td.Name == nil {
		return
	}
	rawName := td.Name.Value
	qualifiedName := rawName
	if pkgName != "" && pkgName != "main" {
		qualifiedName = pkgName + "_" + rawName
	}

	if len(td.TypeParams) > 0 {
		c.GenericTypes[rawName] = td
		c.GenericTypes[qualifiedName] = td

		typeParamNames := make([]string, len(td.TypeParams))
		for i, tp := range td.TypeParams {
			typeParamNames[i] = tp.Name.Value
		}

		tmplStruct := &StructType{
			Name:                qualifiedName,
			TypeParams:          typeParamNames,
			ConstTypeParams:     constTypeParams(td.TypeParams),
			TypeArgs:            []Type{},
			Fields:              []Field{},
			Template:            td,
			IsSpecialized:       false,
			Specializations:     make(map[string]*StructType),
			BuiltinCapabilities: make(map[string]*FuncType),
		}
		c.Structs[qualifiedName] = tmplStruct
		c.Structs[rawName] = tmplStruct
	} else {
		if st := c.ResolveType(td.Type); st != nil && st != TypeVoid {
			if sType, okSt := st.(*StructType); okSt {
				c.Structs[qualifiedName] = sType
				c.Structs[rawName] = sType
			}
		}
	}
}

// -------------------------------------------------------------
// インターフェース充足性検査 (Interface Conformance)
// -------------------------------------------------------------

// Implements は concrete 型が iface インターフェースを満たしているかを判定する
func (c *Context) Implements(concrete Type, iface *InterfaceType) bool {
	if iface == nil {
		return false
	}
	if iface.IsAny() {
		return true
	}
	if concrete == nil {
		return false
	}

	// 検査元がインターフェースの場合（サブインターフェース判定）
	if srcIface, ok := concrete.(*InterfaceType); ok {
		for _, im := range iface.Methods {
			sm, idx := srcIface.GetMethod(im.Name)
			if idx == -1 || sm == nil {
				return false
			}
			if !c.signatureMatches(sm.ParamTypes, sm.ReturnTypes, sm.IsVariadic, im) {
				return false
			}
		}
		return true
	}

	// 具象型（ポインタ型含む）のメソッド探索
	recvName := typeNameOf(concrete)
	for _, im := range iface.Methods {
		fn, _ := c.LookupMethod(recvName, im.Name)
		if fn == nil {
			if !strings.HasPrefix(recvName, "*") {
				fn, _ = c.LookupMethod("*"+recvName, im.Name)
			}
		}
		if fn == nil {
			return false
		}

		fnParams := fn.ParamTypes
		if fn.IsMethod && len(fnParams) > 0 {
			fnParams = fnParams[1:] // レシーバ自身をスキップして比較
		}

		if !c.signatureMatches(fnParams, fn.ReturnTypes, fn.IsVariadic, im) {
			return false
		}
	}

	return true
}

func (c *Context) signatureMatches(paramTypes []Type, returnTypes []Type, isVariadic bool, im Method) bool {
	if len(paramTypes) != len(im.ParamTypes) {
		return false
	}
	for i := range paramTypes {
		if !c.typesCompatible(paramTypes[i], im.ParamTypes[i]) {
			return false
		}
	}
	if len(returnTypes) != len(im.ReturnTypes) {
		return false
	}
	for i := range returnTypes {
		if !c.typesCompatible(returnTypes[i], im.ReturnTypes[i]) {
			return false
		}
	}
	return true
}

func (c *Context) typesCompatible(t1, t2 Type) bool {
	if t1 == nil || t2 == nil {
		return t1 == t2
	}
	if t1 == t2 || typeNameOf(t1) == typeNameOf(t2) {
		return true
	}
	if isIntType(t1) && isIntType(t2) && SizeOf(t1) == SizeOf(t2) {
		return true
	}
	if iface2, ok := t2.(*InterfaceType); ok {
		// A nil interface value is represented as void by the Hike type
		// checker.  It is a valid result for interface-typed helper functions
		// such as parser conversion routines.
		if t1 == TypeVoid {
			return true
		}
		if c.GoHikeMode && goHikeInterfaceCompatible(t1, iface2) {
			return true
		}
		return c.Implements(t1, iface2)
	}
	return false
}

func goHikeInterfaceCompatible(concrete Type, iface *InterfaceType) bool {
	concreteName := typeNameOf(concrete)
	interfaceName := typeNameOf(iface)
	if strings.HasPrefix(interfaceName, "ast_") {
		return true
	}
	if interfaceName == "hir_Terminator" {
		// Go-Hike cannot currently carry the unexported Instruction marker
		// methods through imported HIR package types.  Lowering still constructs
		// concrete terminators, so retain this relationship in compatibility mode.
		return true
	}
	if interfaceName == "hir_Instruction" {
		// Imported HIR instruction implementations carry an unexported marker
		// method, so Go-Hike cannot prove this interface relationship structurally.
		return true
	}
	if interfaceName == "hir_Value" {
		return true
	}
	if interfaceName == "error" && concreteName != "void" {
		// Error values returned by Go-shaped package stubs are opaque to the
		// Hike checker; their concrete representation is not used by lowering.
		return true
	}
	return interfaceName == "sema_Type" && concreteName != "void"
}

// -------------------------------------------------------------
// 型解決 (Type Resolution)
// -------------------------------------------------------------

func (c *Context) resolveGenericStructType(t *ast.NamedType, name, canonicalName string, st *StructType) Type {
	if len(t.TypeArgs) == 0 && len(c.TypeParams) > 0 {
		return st
	}
	if len(t.TypeArgs) == 0 {
		panic(fmt.Sprintf("[Sema Error] line %d:%d: generic struct '%s' requires type arguments (e.g. %s[...])", t.Token.Line, t.Token.Col, name, name))
	}
	if len(t.TypeArgs) != len(st.TypeParams) {
		panic(fmt.Sprintf("[Sema Error] line %d:%d: generic struct '%s' expects %d type arguments, got %d", t.Token.Line, t.Token.Col, name, len(st.TypeParams), len(t.TypeArgs)))
	}

	resolvedArgs := make([]Type, len(t.TypeArgs))
	argNames := []string{}
	typeMap := make(map[string]Type)
	seenConst := false
	for i, arg := range t.TypeArgs {
		if seenConst {
			if named, ok := arg.(*ast.NamedType); ok && named.Package == nil {
				if _, exists := c.Constants[named.Name.Value]; !exists {
					panic(fmt.Sprintf("[Sema Error] line %d:%d: const generic arguments must be trailing and use a const variable", t.Token.Line, t.Token.Col))
				}
			}
		}
		resolvedArg, isConst := c.resolveGenericArg(arg)
		paramName := st.TypeParams[i]
		if st.ConstTypeParams[paramName] && !isConst {
			panic(fmt.Sprintf("[Sema Error] line %d:%d: generic parameter '%s' requires an integer constant argument", t.Token.Line, t.Token.Col, paramName))
		}
		if !st.ConstTypeParams[paramName] && isConst {
			panic(fmt.Sprintf("[Sema Error] line %d:%d: generic parameter '%s' requires a type argument", t.Token.Line, t.Token.Col, paramName))
		}
		if st.ConstTypeParams[paramName] {
			if cv, ok := resolvedArg.(*ConstValueType); ok && cv.Value < 0 {
				panic(fmt.Sprintf("[Sema Error] line %d:%d: generic parameter '%s' requires a non-negative uint constant", t.Token.Line, t.Token.Col, paramName))
			}
		}
		if isConst {
			if i == 0 {
				panic(fmt.Sprintf("[Sema Error] line %d:%d: a generic instantiation requires at least one type argument before const arguments", t.Token.Line, t.Token.Col))
			}
			seenConst = true
		} else if seenConst {
			panic(fmt.Sprintf("[Sema Error] line %d:%d: const generic arguments must be trailing", t.Token.Line, t.Token.Col))
		}
		if resolvedArg == TypeVoid && isConst {
			panic(fmt.Sprintf("[Sema Error] line %d:%d: const generic arguments must be integer literals or const variables", t.Token.Line, t.Token.Col))
		}
		resolvedArgs[i] = resolvedArg
		argNames = append(argNames, specializationArgName(resolvedArg))
		if !isConst {
			typeMap[st.TypeParams[i]] = resolvedArg
		}
	}

	specKey := strings.Join(argNames, "_")
	if existingSt, ok := st.Specializations[specKey]; ok {
		if existingSt.InternalKey == "" {
			existingSt.InternalKey = specializedInternalKeyFor(st, canonicalName, resolvedArgs)
		}
		c.Structs[existingSt.Name] = existingSt
		logGenericTypeResolution(name, existingSt)
		return existingSt
	}

	specializedName := fmt.Sprintf("%s__%s", canonicalName, specKey)
	specializedInternalKey := specializedInternalKeyFor(st, canonicalName, resolvedArgs)
	if existingSt, ok := c.Structs[specializedName]; ok {
		st.Specializations[specKey] = existingSt
		if existingSt.InternalKey == "" {
			existingSt.InternalKey = specializedInternalKey
		}
		logGenericTypeResolution(name, existingSt)
		return existingSt
	}

	newSt := &StructType{
		Name:                specializedName,
		InternalKey:         specializedInternalKey,
		TypeParams:          st.TypeParams,
		ConstTypeParams:     st.ConstTypeParams,
		TypeArgs:            resolvedArgs,
		Fields:              []Field{},
		Template:            st.Template,
		IsSpecialized:       true,
		Specializations:     make(map[string]*StructType),
		BuiltinCapabilities: make(map[string]*FuncType),
	}
	c.Structs[specializedName] = newSt
	st.Specializations[specKey] = newSt
	logGenericTypeResolution(name, newSt)

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

func (c *Context) ResolveType(expr ast.TypeExpr) Type {
	if expr == nil {
		return TypeVoid
	}

	switch t := expr.(type) {
	case *ast.ConstArg:
		return c.resolveConstArg(t)
	case *ast.NamedType:
		name := astIdentifierValue(t.Name)
		if t.Package != nil {
			name = astIdentifierValue(t.Package) + "_" + astIdentifierValue(t.Name)
		}

		if strings.HasPrefix(name, "*") {
			baseName := strings.TrimPrefix(name, "*")
			return &PointerType{Base: c.ResolveType(&ast.NamedType{Token: t.Token, Name: &ast.Identifier{Value: baseName}})}
		}
		if strings.HasPrefix(name, "[]") {
			elemName := strings.TrimPrefix(name, "[]")
			return &SliceType{Elem: c.ResolveType(&ast.NamedType{Token: t.Token, Name: &ast.Identifier{Value: elemName}})}
		}
		if strings.HasPrefix(name, "map[") {
			if end := strings.Index(name, "]"); end > len("map[") && end+1 < len(name) {
				keyName := name[len("map["):end]
				valueName := name[end+1:]
				return &MapType{
					Key:   c.ResolveType(&ast.NamedType{Token: t.Token, Name: &ast.Identifier{Value: keyName}}),
					Value: c.ResolveType(&ast.NamedType{Token: t.Token, Name: &ast.Identifier{Value: valueName}}),
				}
			}
		}

		if tp, ok := c.TypeParams[name]; ok {
			return tp
		}
		if tp, ok := c.TypeParams[astIdentifierValue(t.Name)]; ok && t.Package == nil {
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
				return c.resolveGenericStructType(t, name, canonicalName, st)
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
					argNames = append(argNames, specializationArgName(resolvedArg))
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
						for _, embedded := range itAst.Embedded {
							if embeddedIface, ok := c.ResolveTypeWithSubst(embedded, typeMap).(*InterfaceType); ok {
								newIface.Methods = appendInterfaceMethods(newIface.Methods, embeddedIface.Methods)
							}
						}
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

		// ★追加: 関数呼び出し判定等から参照された場合に FuncType を返す
		if fn, _ := c.LookupFunction(name); fn != nil {
			return fn
		}

		panic(fmt.Sprintf("[Sema Error] line %d:%d: undefined type '%s'",
			t.Token.Line, t.Token.Col, name))

	// ★追加: *ast.StructType の解決ハンドラ
	case *ast.StructType:
		st := &StructType{
			Fields:              []Field{},
			Specializations:     make(map[string]*StructType),
			BuiltinCapabilities: make(map[string]*FuncType),
		}
		for _, f := range t.Fields {
			fType := c.ResolveType(f.Type)
			name := ""
			if f.Name != nil {
				name = f.Name.Value
			}
			st.Fields = append(st.Fields, Field{
				Name:       name,
				Type:       fType,
				IsEmbedded: f.IsEmbedded,
			})
		}
		return st

	case *ast.PointerType:
		return &PointerType{Base: c.ResolveType(t.Base)}
	case *ast.SliceType:
		return &SliceType{Elem: c.ResolveType(t.Elem)}
	case *ast.EllipsisType:
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
		for _, embedded := range t.Embedded {
			if embeddedIface, ok := c.ResolveType(embedded).(*InterfaceType); ok {
				methods = appendInterfaceMethods(methods, embeddedIface.Methods)
			}
		}
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
				setFuncVariadicElem(fnType, resolved.(*SliceType).Elem)
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

func (c *Context) resolveGenericArg(arg ast.TypeExpr) (Type, bool) {
	if constArg, ok := arg.(*ast.ConstArg); ok {
		return c.resolveConstArg(constArg), true
	}
	if named, ok := arg.(*ast.NamedType); ok && named.Package == nil {
		if value, exists := c.Constants[named.Name.Value]; exists {
			return &ConstValueType{Value: value}, true
		}
	}
	return c.ResolveType(arg), false
}

func specializationArgName(t Type) string {
	if value, ok := t.(*ConstValueType); ok {
		return fmt.Sprintf("const_%d", value.Value)
	}
	return strings.ReplaceAll(typeNameOf(t), "*", "Ptr")
}

func specializedInternalKeyFor(template *StructType, canonicalName string, args []Type) string {
	base := template.InternalKey
	if base == "" {
		base = canonicalName
		if parts := strings.SplitN(base, "_", 2); len(parts) == 2 {
			base = parts[0] + "/" + parts[1]
		}
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		if cv, ok := arg.(*ConstValueType); ok {
			parts[i] = fmt.Sprintf("%d", cv.Value)
		} else if arg != nil {
			parts[i] = typeNameOf(arg)
		}
	}
	return base + "@" + strings.Join(parts, "@")
}

func logGenericTypeResolution(source string, resolved *StructType) {
	if resolved == nil {
		return
	}
	argNames := make([]string, len(resolved.TypeArgs))
	for i, arg := range resolved.TypeArgs {
		if arg != nil {
			argNames[i] = typeNameOf(arg)
		}
	}
	logger.LogVerbose2("[Verbose2] Sema generic type: %s[%s] -> name=%s internal=%s ir=%s\n",
		source, strings.Join(argNames, ","), resolved.Name, resolved.InternalKey, MangleInternalKeyToIR(resolved.InternalKey))
}

func (c *Context) resolveConstArg(arg *ast.ConstArg) Type {
	if arg == nil || arg.Expr == nil {
		return TypeVoid
	}
	switch expr := arg.Expr.(type) {
	case *ast.IntegerLiteral:
		return &ConstValueType{Value: expr.Value}
	case *ast.Identifier:
		if value, ok := c.Constants[astIdentifierValue(expr)]; ok {
			return &ConstValueType{Value: value}
		}
	}
	return TypeVoid
}

func (c *Context) ResolveTypeWithSubst(t ast.TypeExpr, subst map[string]Type) Type {
	if t == nil {
		return TypeVoid
	}
	switch node := t.(type) {
	case *ast.ConstArg:
		return c.resolveConstArg(node)
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
					argNames = append(argNames, specializationArgName(resolvedArg))
					if i < len(st.TypeParams) {
						typeMap[st.TypeParams[i]] = resolvedArg
					}
				}
			} else if len(subst) > 0 {
				for _, tp := range st.TypeParams {
					if resolvedArg, ok := subst[tp]; ok {
						resolvedArgs = append(resolvedArgs, resolvedArg)
						argNames = append(argNames, specializationArgName(resolvedArg))
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
					Name:                specializedName,
					TypeParams:          st.TypeParams,
					TypeArgs:            resolvedArgs,
					Fields:              []Field{},
					Template:            st.Template,
					IsSpecialized:       true,
					Specializations:     make(map[string]*StructType),
					BuiltinCapabilities: make(map[string]*FuncType),
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
	case *ast.StructType:
		st := &StructType{
			Fields:              []Field{},
			Specializations:     make(map[string]*StructType),
			BuiltinCapabilities: make(map[string]*FuncType),
		}
		for _, f := range node.Fields {
			fType := c.ResolveTypeWithSubst(f.Type, subst)
			name := ""
			if f.Name != nil {
				name = f.Name.Value
			}
			st.Fields = append(st.Fields, Field{
				Name:       name,
				Type:       fType,
				IsEmbedded: f.IsEmbedded,
			})
		}
		return st
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
				setFuncVariadicElem(fnType, resolved.(*SliceType).Elem)
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
		switch astIdentifierValue(id) {
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
		if st, _ := c.LookupStruct(astIdentifierValue(id)); st != nil {
			return st
		}
		if iface, _ := c.LookupInterface(astIdentifierValue(id)); iface != nil {
			return iface
		}
		if alias, _ := c.LookupAlias(astIdentifierValue(id)); alias != nil {
			return alias
		}
		return nil
	}
	// 1. パッケージ修飾型名 (例: list.List)
	if mem, ok := e.(*ast.MemberExpr); ok {
		if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
			qualified := pkgId.Value + "_" + mem.Field.Value
			if st, _ := c.LookupStruct(qualified); st != nil {
				return st
			}
			if iface, _ := c.LookupInterface(qualified); iface != nil {
				return iface
			}
			if alias, _ := c.LookupAlias(qualified); alias != nil {
				return alias
			}
			// 型として解決できない場合（メソッドや通常関数）は nil を返して通常呼び出しとして処理させる
			return nil
		}
	}
	// 2. 添字構文によるジェネリクス型指定 (例: List[int] や list.List[int])
	if idxExpr, ok := e.(*ast.IndexExpr); ok {
		var pkgId *ast.Identifier
		var typeId *ast.Identifier
		if id, okId := idxExpr.Left.(*ast.Identifier); okId {
			typeId = id
		} else if mem, okMem := idxExpr.Left.(*ast.MemberExpr); okMem {
			if p, okP := mem.Object.(*ast.Identifier); okP {
				pkgId = p
				typeId = mem.Field
			}
		}

		if typeId != nil {
			var typeArgs []ast.TypeExpr
			if te, okTe := idxExpr.Index.(ast.TypeExpr); okTe {
				typeArgs = append(typeArgs, te)
			} else if id, okId := idxExpr.Index.(*ast.Identifier); okId {
				typeArgs = append(typeArgs, &ast.NamedType{Token: id.Token, Name: id})
			} else if mem, okMem := idxExpr.Index.(*ast.MemberExpr); okMem {
				if p, okP := mem.Object.(*ast.Identifier); okP {
					typeArgs = append(typeArgs, &ast.NamedType{Token: mem.Token, Package: p, Name: mem.Field})
				}
			}

			return c.ResolveType(&ast.NamedType{
				Token:    idxExpr.Token,
				Package:  pkgId,
				Name:     typeId,
				TypeArgs: typeArgs,
			})
		}
	}
	// 3. GenericInstExpr によるジェネリクス型指定
	if gen, ok := e.(*ast.GenericInstExpr); ok {
		var pkgId *ast.Identifier
		var typeId *ast.Identifier
		if id, okId := gen.Left.(*ast.Identifier); okId {
			typeId = id
		} else if mem, okMem := gen.Left.(*ast.MemberExpr); okMem {
			if p, okP := mem.Object.(*ast.Identifier); okP {
				pkgId = p
				typeId = mem.Field
			}
		}
		if typeId != nil {
			return c.ResolveType(&ast.NamedType{
				Token:    gen.Token,
				Package:  pkgId,
				Name:     typeId,
				TypeArgs: gen.TypeArgs,
			})
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

func isIntType(t Type) bool {
	if t == nil {
		return false
	}
	switch t {
	case TypeInt, TypeInt64, TypeInt32, TypeInt16, TypeInt8,
		TypeUint, TypeUint64, TypeUint32, TypeUint16, TypeUint8, TypeUintptr, TypeByte:
		return true
	}
	return false
}

// -------------------------------------------------------------
// 型推論 (Type Inference) & 暗黙キャスト
// -------------------------------------------------------------

func (c *Context) inferGenericInstType(e *ast.GenericInstExpr) Type {
	var baseName string
	if id, ok := e.Left.(*ast.Identifier); ok {
		baseName = astIdentifierValue(id)
	} else if mem, ok := e.Left.(*ast.MemberExpr); ok {
		if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
			baseName = pkgId.Value + "_" + mem.Field.Value
		} else {
			baseName = mem.Field.Value
		}
	}
	if baseName == "" {
		return TypeInt
	}

	tmpl := c.GenericFuncs[baseName]
	if tmpl == nil {
		if fn, _ := c.LookupFunction(baseName); fn != nil && fn.Template != nil {
			tmpl = fn.Template
		}
	}
	if tmpl != nil {
		typeArgs := make([]Type, len(e.TypeArgs))
		for i, ta := range e.TypeArgs {
			typeArgs[i] = c.ResolveType(ta)
		}
		subst := make(map[string]Type)
		for i, tp := range tmpl.TypeParams {
			if i < len(typeArgs) {
				subst[tp.Name.Value] = typeArgs[i]
			}
		}
		rts := make([]Type, len(tmpl.ReturnTypes))
		for i, rt := range tmpl.ReturnTypes {
			rts[i] = c.ResolveTypeWithSubst(rt, subst)
		}
		pts := make([]Type, len(tmpl.Params))
		for i, p := range tmpl.Params {
			pts[i] = c.ResolveTypeWithSubst(p.Type, subst)
		}
		return &FuncType{
			Name:          baseName,
			ParamTypes:    pts,
			ReturnTypes:   rts,
			IsSpecialized: true,
		}
	}
	if st, _ := c.LookupStruct(baseName); st != nil && st.IsGeneric() {
		return c.ResolveType(&ast.NamedType{
			Token:    e.Token,
			Name:     &ast.Identifier{Token: e.Token, Value: baseName},
			TypeArgs: e.TypeArgs,
		})
	}
	return TypeInt
}

func (c *Context) inferMemberExprType(e *ast.MemberExpr, locals map[string]Type) Type {
	if pkgId, okPkg := e.Object.(*ast.Identifier); okPkg {
		qualified := pkgId.Value + "_" + e.Field.Value
		if t, ok := c.Globals[qualified]; ok {
			return t
		}
		if _, ok := c.LookupConstant(qualified); ok {
			return TypeInt
		}
		if _, ok := c.LookupStringConstant(qualified); ok {
			return TypeString
		}
		if _, ok := c.LookupFloatConstant(qualified); ok {
			return TypeFloat64
		}
		if fn, _ := c.LookupFunction(qualified); fn != nil {
			return fn
		}
	}

	objType := c.InferExprType(e.Object, locals)
	rawObjType := objType
	if pt, ok := objType.(*PointerType); ok {
		rawObjType = pt.Base
	}

	// インターフェース型レシーバのメソッド解決
	if iface, ok := rawObjType.(*InterfaceType); ok {
		if m, _ := iface.GetMethod(e.Field.Value); m != nil {
			return &FuncType{
				Name:         m.Name,
				InternalKey:  m.InternalKey,
				ParamTypes:   m.ParamTypes,
				ReturnTypes:  m.ReturnTypes,
				IsVariadic:   m.IsVariadic,
				VariadicElem: m.VariadicElem,
				IsMethod:     true,
			}
		}
	}

	if fn, _ := c.LookupMethod(typeNameOf(objType), e.Field.Value); fn != nil {
		return fn
	}

	if st, ok := rawObjType.(*StructType); ok {
		for _, f := range st.Fields {
			if f.Name == e.Field.Value {
				return f.Type
			}
		}
	}
	return TypeInt
}

func (c *Context) inferBinaryExprType(e *ast.BinaryExpr, locals map[string]Type) Type {
	if e.WithCarry && (e.Operator == "<<" || e.Operator == ">>") {
		valueType := c.InferExprType(e.Left, locals)
		return &TupleType{Types: []Type{valueType, valueType}}
	}
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
}

func (c *Context) inferCallExprType(e *ast.CallExpr, locals map[string]Type) Type {
	if len(e.Args) == 1 {
		if castT := c.resolveTypeFromExpr(e.Function); castT != nil && castT != TypeVoid {
			if _, isFn := castT.(*FuncType); !isFn {
				return castT
			}
		}
	}
	if id, ok := e.Function.(*ast.Identifier); ok {
		switch astIdentifierValue(id) {
		case "len", "cap", "sizeof":
			return TypeInt
		case "recover", "recover_cause":
			return &InterfaceType{Name: "any", Specializations: make(map[string]*InterfaceType)}
		case "recover_site":
			return TypeInt
		case "panic":
			return TypeVoid
		case "string":
			return TypeString
		case "cstring":
			return TypeCString
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

	if mem, ok := e.Function.(*ast.MemberExpr); ok {
		if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
			targetName := pkgId.Value + "_" + mem.Field.Value
			if fn, _ := c.LookupFunction(targetName); fn != nil {
				c.ResolvedCalls[e] = fn
			}
		} else {
			objType := c.InferExprType(mem.Object, locals)
			rawObj := objType
			if pt, okPt := objType.(*PointerType); okPt {
				rawObj = pt.Base
			}

			if iface, okIface := rawObj.(*InterfaceType); okIface {
				if m, _ := iface.GetMethod(mem.Field.Value); m != nil {
					c.ResolvedCalls[e] = &FuncType{
						Name:         m.Name,
						InternalKey:  m.InternalKey,
						ParamTypes:   m.ParamTypes,
						ReturnTypes:  m.ReturnTypes,
						IsVariadic:   m.IsVariadic,
						VariadicElem: m.VariadicElem,
						IsMethod:     true,
					}
				}
			} else if fn, _ := c.LookupMethod(typeNameOf(objType), mem.Field.Value); fn != nil {
				c.ResolvedCalls[e] = fn
			}
		}
	}

	fnType := c.InferExprType(e.Function, locals)
	if ft, ok := fnType.(*FuncType); ok {
		c.ResolvedCalls[e] = ft
		if len(ft.ReturnTypes) == 1 {
			return ft.ReturnTypes[0]
		} else if len(ft.ReturnTypes) > 1 {
			return &TupleType{Types: ft.ReturnTypes}
		}
		return TypeVoid
	}
	return TypeVoid
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
	case *ast.CharLiteral:
		return TypeString
	case *ast.StringLiteral:
		return TypeString
	case *ast.NilLiteral:
		return &PointerType{Base: TypeByte}
	case *ast.Identifier:
		if astIdentifierValue(e) == "..." {
			return TypeVoid
		}
		switch astIdentifierValue(e) {
		case "true", "false":
			return TypeBool
		}
		if t, ok := locals[astIdentifierValue(e)]; ok {
			return t
		}
		if t, ok := c.Globals[astIdentifierValue(e)]; ok {
			return t
		}
		if _, ok := c.LookupConstant(astIdentifierValue(e)); ok {
			return TypeInt
		}
		if _, ok := c.LookupStringConstant(astIdentifierValue(e)); ok {
			return TypeString
		}
		if _, ok := c.LookupFloatConstant(astIdentifierValue(e)); ok {
			return TypeFloat64
		}
		if fn, ok := c.Functions[astIdentifierValue(e)]; ok {
			return fn
		}
		return TypeInt

	case *ast.GenericInstExpr:
		return c.inferGenericInstType(e)

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
		return c.inferBinaryExprType(e, locals)

	case *ast.MemberExpr:
		return c.inferMemberExprType(e, locals)

	case *ast.IndexExpr:
		lt := c.InferExprType(e.Left, locals)
		if t, err := c.ResolveIndexExprType(lt, e.Index); err == nil {
			return t
		}

	case *ast.SliceExpr:
		lt := c.InferExprType(e.Left, locals)
		if st, err := c.ResolveSliceExprType(lt, e.Low, e.High); err == nil {
			return st
		}
		return lt

	case *ast.TypeAssertExpr:
		if e.Target != nil {
			return c.ResolveType(e.Target)
		}
		return &InterfaceType{Name: "any", Specializations: make(map[string]*InterfaceType)}

	case *ast.CallExpr:
		return c.inferCallExprType(e, locals)
	case *ast.InlineAsmExpr:
		return TypeVoid

	case *ast.StructLiteral:
		return c.ResolveType(e.Type)

	case *ast.MapLiteral:
		return c.ResolveType(e.Type)

	case *ast.ArrayLiteral:
		resolved := c.ResolveType(e.Type)
		if array, ok := resolved.(*ArrayType); ok && array.Len < 0 {
			array.Len = len(e.Elements)
			e.Type.Len = int64(array.Len)
		}
		return resolved

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
		setFuncVariadicElem(ft, variadicElem)
		return ft
	}

	return TypeInt
}

func (c *Context) CoerceExpr(expr ast.Expression, targetType Type, locals map[string]Type) ast.Expression {
	if expr == nil || targetType == nil {
		return expr
	}

	if cl, ok := expr.(*ast.CharLiteral); ok && isIntType(targetType) {
		return &ast.IntegerLiteral{
			Token: cl.Token,
			Value: int64(cl.CodePoint),
		}
	}

	actualType := c.InferExprType(expr, locals)

	// インターフェース代入時の充足性検査
	if iface, ok := targetType.(*InterfaceType); ok {
		if _, isNil := expr.(*ast.NilLiteral); !isNil && actualType != TypeVoid && !iface.IsAny() && !(c.GoHikeMode && goHikeInterfaceCompatible(actualType, iface)) {
			if !c.Implements(actualType, iface) {
				line, col := expressionPosition(expr)
				panic(fmt.Sprintf("[Sema Error] line %d:%d: type '%s' does not implement interface '%s'",
					line, col, typeNameOf(actualType), typeNameOf(iface)))
			}
		}
	}

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

func expressionPosition(expr ast.Expression) (int, int) {
	switch n := expr.(type) {
	case *ast.Identifier:
		return n.Token.Line, n.Token.Col
	case *ast.IntegerLiteral:
		return n.Token.Line, n.Token.Col
	case *ast.StringLiteral:
		return n.Token.Line, n.Token.Col
	case *ast.CharLiteral:
		return n.Token.Line, n.Token.Col
	case *ast.MemberExpr:
		return n.Token.Line, n.Token.Col
	case *ast.CallExpr:
		return n.Token.Line, n.Token.Col
	default:
		return 0, 0
	}
}

// -------------------------------------------------------------
// 定数評価 (Constant Folding)
// -------------------------------------------------------------

func (c *Context) evalConstString(expr ast.Expression) (string, bool) {
	if expr == nil {
		return "", false
	}
	switch e := expr.(type) {
	case *ast.StringLiteral:
		return e.Value, true
	case *ast.Identifier:
		return c.LookupStringConstant(astIdentifierValue(e))
	case *ast.MemberExpr:
		if pkgID, ok := e.Object.(*ast.Identifier); ok {
			if value, found := c.LookupStringConstant(pkgID.Value + "_" + e.Field.Value); found {
				return value, true
			}
		}
	}
	return "", false
}

func (c *Context) evalConstInt(expr ast.Expression) (int64, bool) {
	if expr == nil {
		return 0, false
	}
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return e.Value, true
	case *ast.CharLiteral:
		return int64(e.CodePoint), true
	case *ast.Identifier:
		if val, ok := c.LookupConstant(astIdentifierValue(e)); ok {
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
		// sizeof(Type) または sizeof(Expr) のコンパイル時定数評価
		if id, ok := e.Function.(*ast.Identifier); ok && astIdentifierValue(id) == "sizeof" {
			if len(e.Args) == 1 {
				arg := e.Args[0]
				logger.LogVerbose2("[Verbose2] Sema evalConstInt CallExpr sizeof: arg=%T (%+v)\n", arg, arg)
				if _, ok := arg.(*ast.GenericInstExpr); ok {
					logger.LogVerbose2("[Verbose2] Sema CallExpr sizeof: arg is GenericInstExpr, postponing\n")
					return 0, false
				}
				if _, ok := arg.(*ast.IndexExpr); ok {
					logger.LogVerbose2("[Verbose2] Sema CallExpr sizeof: arg is IndexExpr, postponing\n")
					return 0, false
				}
				t := c.resolveTypeFromExpr(arg)
				if t == nil || t == TypeVoid {
					t = c.InferExprType(arg, nil)
				}
				if t != nil && t != TypeVoid {
					if pt, isPtr := t.(*PointerType); isPtr {
						t = pt.Base
					}
					if st, _ := c.LookupStruct(typeNameOf(t)); st != nil {
						if st.IsGeneric() || len(st.Fields) == 0 {
							logger.LogVerbose2("[Verbose2] Sema CallExpr sizeof: struct '%s' unexpanded, postponing\n", st.Name)
							return 0, false
						}
						sz := int64(st.Size())
						logger.LogVerbose2("[Verbose2] Sema CallExpr sizeof: folded struct size = %d\n", sz)
						if sz > 0 {
							return sz, true
						}
						return 0, false
					}
					sz := int64(SizeOf(t))
					logger.LogVerbose2("[Verbose2] Sema CallExpr sizeof: folded type size = %d\n", sz)
					if sz > 0 {
						return sz, true
					}
				}
			}
			return 0, false
		}
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
	case *ast.CharLiteral:
		return float64(e.CodePoint), true
	case *ast.Identifier:
		if val, ok := c.LookupFloatConstant(astIdentifierValue(e)); ok {
			return val, true
		}
		if val, ok := c.LookupConstant(astIdentifierValue(e)); ok {
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

// -------------------------------------------------------------
// 組み込みインターフェース能力判定 (Builtin Capabilities)
// -------------------------------------------------------------

// CheckIndexable は指定された型が Indexable (Get(key) V または Get(key) (V, bool)) を満たすか検査する
func (c *Context) CheckIndexable(t Type) (Type, Type, bool, *FuncType) {
	if t == nil {
		return nil, nil, false, nil
	}
	typeName := typeNameOf(t)
	rawName := strings.TrimPrefix(typeName, "*")

	st, _ := c.LookupStruct(rawName)
	if st == nil && strings.Contains(rawName, "__") {
		st, _ = c.LookupStruct(strings.Split(rawName, "__")[0])
	}

	if st != nil && st.BuiltinCapabilities != nil {
		if capFn, ok := st.BuiltinCapabilities["Indexable"]; ok && capFn != nil {
			k, v, okFlag := extractIndexableSignature(capFn)
			return k, v, okFlag, capFn
		}
	}

	fn, _ := c.LookupMethod(typeName, "Get")
	if fn == nil && !strings.HasPrefix(typeName, "*") {
		fn, _ = c.LookupMethod("*"+typeName, "Get")
	}
	if fn == nil && strings.HasPrefix(typeName, "*") {
		fn, _ = c.LookupMethod(rawName, "Get")
	}

	if fn != nil {
		k, v, okFlag := extractIndexableSignature(fn)
		if k != nil && v != nil {
			if st != nil {
				if st.BuiltinCapabilities == nil {
					st.BuiltinCapabilities = make(map[string]*FuncType)
				}
				st.BuiltinCapabilities["Indexable"] = fn
			}
			return k, v, okFlag, fn
		}
	}

	return nil, nil, false, nil
}

func extractIndexableSignature(fn *FuncType) (keyType Type, valType Type, hasOk bool) {
	params := fn.ParamTypes
	if fn.IsMethod && len(params) > 0 {
		params = params[1:]
	}
	if len(params) != 1 {
		return nil, nil, false
	}
	if len(fn.ReturnTypes) == 1 {
		return params[0], fn.ReturnTypes[0], false
	}
	if len(fn.ReturnTypes) == 2 {
		return params[0], fn.ReturnTypes[0], true
	}
	return nil, nil, false
}

// CheckIndexAssignable は指定された型が IndexAssignable (Set(key, val)) を満たすか検査する
func (c *Context) CheckIndexAssignable(t Type) (Type, Type, *FuncType) {
	if t == nil {
		return nil, nil, nil
	}
	typeName := typeNameOf(t)
	rawName := strings.TrimPrefix(typeName, "*")

	st, _ := c.LookupStruct(rawName)
	if st == nil && strings.Contains(rawName, "__") {
		st, _ = c.LookupStruct(strings.Split(rawName, "__")[0])
	}

	if st != nil && st.BuiltinCapabilities != nil {
		if capFn, ok := st.BuiltinCapabilities["IndexAssignable"]; ok && capFn != nil {
			k, v := extractIndexAssignableSignature(capFn)
			return k, v, capFn
		}
	}

	fn, _ := c.LookupMethod(typeName, "Set")
	if fn == nil && !strings.HasPrefix(typeName, "*") {
		fn, _ = c.LookupMethod("*"+typeName, "Set")
	}
	if fn == nil && strings.HasPrefix(typeName, "*") {
		fn, _ = c.LookupMethod(rawName, "Set")
	}

	if fn != nil {
		k, v := extractIndexAssignableSignature(fn)
		if k != nil && v != nil {
			if st != nil {
				if st.BuiltinCapabilities == nil {
					st.BuiltinCapabilities = make(map[string]*FuncType)
				}
				st.BuiltinCapabilities["IndexAssignable"] = fn
			}
			return k, v, fn
		}
	}

	return nil, nil, nil
}

func extractIndexAssignableSignature(fn *FuncType) (keyType Type, valType Type) {
	params := fn.ParamTypes
	if fn.IsMethod && len(params) > 0 {
		params = params[1:]
	}
	if len(params) == 2 && len(fn.ReturnTypes) == 0 {
		return params[0], params[1]
	}
	return nil, nil
}

// CheckSliceable は指定された型が Sliceable (Slice(low, high int) (T, bool) または Slice(low, high int) T) を満たすか検査する
func (c *Context) CheckSliceable(t Type) (Type, bool, *FuncType) {
	if t == nil {
		return nil, false, nil
	}
	typeName := typeNameOf(t)
	rawName := strings.TrimPrefix(typeName, "*")

	st, _ := c.LookupStruct(rawName)
	if st == nil && strings.Contains(rawName, "__") {
		st, _ = c.LookupStruct(strings.Split(rawName, "__")[0])
	}

	if st != nil && st.BuiltinCapabilities != nil {
		if capFn, ok := st.BuiltinCapabilities["Sliceable"]; ok && capFn != nil {
			r, okFlag := extractSliceableSignature(capFn)
			return r, okFlag, capFn
		}
	}

	fn, _ := c.LookupMethod(typeName, "Slice")
	if fn == nil && !strings.HasPrefix(typeName, "*") {
		fn, _ = c.LookupMethod("*"+typeName, "Slice")
	}
	if fn == nil && strings.HasPrefix(typeName, "*") {
		fn, _ = c.LookupMethod(rawName, "Slice")
	}

	if fn != nil {
		r, okFlag := extractSliceableSignature(fn)
		if r != nil {
			if st != nil {
				if st.BuiltinCapabilities == nil {
					st.BuiltinCapabilities = make(map[string]*FuncType)
				}
				st.BuiltinCapabilities["Sliceable"] = fn
			}
			return r, okFlag, fn
		}
	}

	return nil, false, nil
}

func extractSliceableSignature(fn *FuncType) (resType Type, hasOk bool) {
	params := fn.ParamTypes
	if fn.IsMethod && len(params) > 0 {
		params = params[1:]
	}
	if len(params) == 2 && isIntType(params[0]) && isIntType(params[1]) {
		if len(fn.ReturnTypes) == 1 {
			return fn.ReturnTypes[0], false
		}
		if len(fn.ReturnTypes) == 2 {
			return fn.ReturnTypes[0], true
		}
	}
	return nil, false
}

// CheckIterable は指定された型が Iterable (InitIterator(buf *byte) int && Next(buf *byte) (*T, bool)) を満たすか検査する
func (c *Context) CheckIterable(t Type) (Type, *FuncType, *FuncType) {
	if t == nil {
		return nil, nil, nil
	}
	typeName := typeNameOf(t)
	rawName := strings.TrimPrefix(typeName, "*")

	fnInit, _ := c.LookupMethod(typeName, "InitIterator")
	if fnInit == nil {
		fnInit, _ = c.LookupMethod("*"+rawName, "InitIterator")
	}

	fnNext, _ := c.LookupMethod(typeName, "Next")
	if fnNext == nil {
		fnNext, _ = c.LookupMethod("*"+rawName, "Next")
	}

	if fnInit != nil && fnNext != nil {
		if len(fnNext.ReturnTypes) == 2 {
			ret0 := fnNext.ReturnTypes[0]
			if pt, ok := ret0.(*PointerType); ok {
				return pt.Base, fnInit, fnNext
			}
			return ret0, fnInit, fnNext
		}
	}
	return nil, nil, nil
}

// CheckAsyncIterable は指定された型が AsyncIterable (InitIterator + NextChannel(buf *byte) (chan T, bool)) を満たすか検査する
func (c *Context) CheckAsyncIterable(t Type) (Type, *FuncType, *FuncType) {
	if t == nil {
		return nil, nil, nil
	}
	typeName := typeNameOf(t)
	rawName := strings.TrimPrefix(typeName, "*")

	fnInit, _ := c.LookupMethod(typeName, "InitIterator")
	if fnInit == nil {
		fnInit, _ = c.LookupMethod("*"+rawName, "InitIterator")
	}

	fnNextChan, _ := c.LookupMethod(typeName, "NextChannel")
	if fnNextChan == nil {
		fnNextChan, _ = c.LookupMethod("*"+rawName, "NextChannel")
	}

	if fnNextChan != nil {
		if len(fnNextChan.ReturnTypes) >= 1 {
			if ch, ok := fnNextChan.ReturnTypes[0].(*ChanType); ok {
				return ch.Elem, fnInit, fnNextChan
			}
		}
	}
	return nil, nil, nil
}

// -------------------------------------------------------------
// マップビヘイビア・インデックス & スライス解決
// -------------------------------------------------------------

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

	typeName := typeNameOf(t)
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

	// ユーザー定義構造体の Indexable 能力判定 (Get メソッド)
	if _, valType, _, fn := c.CheckIndexable(leftType); fn != nil {
		return valType, nil
	}

	// 従来の MapBehavior (後方互換性)
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
	if leftType == TypeString || leftType == TypeCString {
		return TypeByte, nil
	}

	return TypeVoid, fmt.Errorf("type '%s' does not support indexing (implement 'Indexable' with Get method to enable)", typeNameOf(leftType))
}

func (c *Context) ResolveSliceExprType(leftType Type, low, high ast.Expression) (Type, error) {
	if leftType == nil {
		return TypeVoid, fmt.Errorf("cannot slice nil type")
	}

	if leftType == TypeString || leftType == TypeCString {
		return TypeString, nil
	}
	if sl, ok := leftType.(*SliceType); ok {
		return sl, nil
	}
	if ar, ok := leftType.(*ArrayType); ok {
		return &SliceType{Elem: ar.Elem}, nil
	}
	if pt, ok := leftType.(*PointerType); ok {
		return &SliceType{Elem: pt.Base}, nil
	}

	// ユーザー定義構造体の Sliceable 能力判定 (Slice メソッド)
	if resType, _, fn := c.CheckSliceable(leftType); fn != nil {
		return resType, nil
	}

	return TypeVoid, fmt.Errorf("type '%s' does not support slicing (implement 'Sliceable' with Slice(low, high int) to enable)", typeNameOf(leftType))
}

func (c *Context) InferExprTypeWithDiag(expr ast.Expression, locals map[string]Type, reporter *diag.Reporter, filename string) Type {
	if expr == nil {
		return TypeBad
	}
	switch e := expr.(type) {
	case *ast.InlineAsmExpr:
		for _, operand := range e.Operands {
			if IsBad(c.InferExprTypeWithDiag(operand, locals, reporter, filename)) {
				return TypeBad
			}
		}
		return TypeVoid
	case *ast.IntegerLiteral, *ast.CharLiteral:
		return TypeInt
	case *ast.FloatLiteral:
		return TypeFloat64
	case *ast.StringLiteral:
		return TypeString
	case *ast.NilLiteral:
		return &PointerType{Base: TypeByte}
	case *ast.Identifier:
		if t, ok := locals[astIdentifierValue(e)]; ok {
			return t
		}
		if t, ok := c.Globals[astIdentifierValue(e)]; ok {
			return t
		}
		if _, ok := c.LookupConstant(astIdentifierValue(e)); ok {
			return TypeInt
		}
		if _, ok := c.LookupStringConstant(astIdentifierValue(e)); ok {
			return TypeString
		}
		if _, ok := c.LookupFloatConstant(astIdentifierValue(e)); ok {
			return TypeFloat64
		}
		if t, ok := c.Functions[astIdentifierValue(e)]; ok {
			return t
		}
		switch astIdentifierValue(e) {
		case "true", "false":
			return TypeBool
		case "len", "cap", "append", "delete", "make", "sizeof":
			return &FuncType{Name: astIdentifierValue(e), ReturnTypes: []Type{TypeInt}}
		case "int", "int64", "int32", "int16", "int8", "uint", "uint64", "uint32", "uint16", "uint8", "uintptr", "byte":
			return &FuncType{Name: astIdentifierValue(e), ReturnTypes: []Type{TypeInt}}
		case "string", "cstring":
			return &FuncType{Name: astIdentifierValue(e), ReturnTypes: []Type{TypeString}}
		case "bool":
			return &FuncType{Name: astIdentifierValue(e), ReturnTypes: []Type{TypeBool}}
		case "float32", "float64":
			return &FuncType{Name: astIdentifierValue(e), ReturnTypes: []Type{TypeFloat64}}
		}

		// 未定義識別子: エラーを記録して TypeBad を返却
		reporter.Errorf(filename, e.Token.Line, e.Token.Col, "undefined: %s", astIdentifierValue(e))
		return TypeBad

	case *ast.PrefixExpr:
		right := c.InferExprTypeWithDiag(e.Right, locals, reporter, filename)
		if IsBad(right) {
			return TypeBad
		}
		if e.Operator == "!" {
			if right != TypeBool {
				reporter.Errorf(filename, e.Token.Line, e.Token.Col, "cannot use %s as bool", typeNameOf(right))
				return TypeBad
			}
			return TypeBool
		}
		if e.Operator == "*" {
			if _, ok := right.(*PointerType); !ok {
				reporter.Errorf(filename, e.Token.Line, e.Token.Col, "cannot dereference non-pointer type %s", typeNameOf(right))
				return TypeBad
			}
		}
		return right

	case *ast.BinaryExpr:
		lt := c.InferExprTypeWithDiag(e.Left, locals, reporter, filename)
		rt := c.InferExprTypeWithDiag(e.Right, locals, reporter, filename)

		// どちらかがすでに不正型なら、これ以上の重複エラーを出さずに TypeBad を返す
		if IsBad(lt) || IsBad(rt) {
			return TypeBad
		}

		if !c.typesCompatible(lt, rt) && isDiagnosticLiteral(e.Left) && isDiagnosticLiteral(e.Right) {
			reporter.Errorf(filename, e.Token.Line, e.Token.Col, "invalid operation: %s %s %s (mismatched types %s and %s)",
				e.Left.TokenLiteral(), e.Operator, e.Right.TokenLiteral(), typeNameOf(lt), typeNameOf(rt))
			return TypeBad
		}
		return lt

	case *ast.CallExpr:
		fnType := c.InferExprTypeWithDiag(e.Function, locals, reporter, filename)
		for _, arg := range e.Args {
			c.InferExprTypeWithDiag(arg, locals, reporter, filename)
		}
		if IsBad(fnType) {
			return TypeBad
		}
		if ft, ok := fnType.(*FuncType); ok {
			if len(ft.ReturnTypes) == 0 {
				return TypeVoid
			}
			if len(ft.ReturnTypes) == 1 {
				return ft.ReturnTypes[0]
			}
			return &TupleType{Types: ft.ReturnTypes}
		}
		return TypeBad

	case *ast.MemberExpr:
		// Imported package members are resolved by the loader/transformer and do
		// not appear as local identifiers in this context.
		if object, isIdentifier := e.Object.(*ast.Identifier); isIdentifier {
			if !c.diagnosticPackages[astIdentifierValue(object)] {
				return c.InferExprTypeWithDiag(e.Object, locals, reporter, filename)
			}
		} else {
			objType := c.InferExprTypeWithDiag(e.Object, locals, reporter, filename)
			if IsBad(objType) {
				return TypeBad
			}
		}
		return TypeInt

	case *ast.IndexExpr:
		left := c.InferExprTypeWithDiag(e.Left, locals, reporter, filename)
		c.InferExprTypeWithDiag(e.Index, locals, reporter, filename)
		if IsBad(left) {
			return TypeBad
		}
		if left == TypeString || left == TypeCString {
			return TypeInt
		}
		_, directInteger := e.Left.(*ast.IntegerLiteral)
		directBool := false
		if ident, ok := e.Left.(*ast.Identifier); ok {
			directBool = astIdentifierValue(ident) == "true" || astIdentifierValue(ident) == "false"
		}
		if !directInteger && !directBool {
			return TypeInt
		}
		if _, ok := left.(*SliceType); !ok {
			if _, ok := left.(*ArrayType); !ok {
				reporter.Errorf(filename, e.Token.Line, e.Token.Col, "type '%s' does not support indexing", typeNameOf(left))
				return TypeBad
			}
		}
		return TypeInt

	case *ast.GenericInstExpr:
		return TypeInt

	case *ast.StructLiteral:
		for _, field := range e.Fields {
			c.InferExprTypeWithDiag(field.Value, locals, reporter, filename)
		}
		return TypeInt

	case *ast.MapLiteral:
		for _, entry := range e.Entries {
			c.InferExprTypeWithDiag(entry.Key, locals, reporter, filename)
			c.InferExprTypeWithDiag(entry.Value, locals, reporter, filename)
		}
		return c.ResolveType(e.Type)

	case *ast.ArrayLiteral:
		for _, element := range e.Elements {
			c.InferExprTypeWithDiag(element, locals, reporter, filename)
		}
		return TypeInt

	case *ast.SliceLiteral:
		for _, element := range e.Elements {
			c.InferExprTypeWithDiag(element, locals, reporter, filename)
		}
		return TypeInt

	case *ast.FuncLit:
		inner := cloneTypes(locals)
		for _, p := range e.Params {
			inner[p.Name.Value] = TypeInt
			if p.Default != nil {
				c.InferExprTypeWithDiag(p.Default, inner, reporter, filename)
			}
		}
		if e.Body != nil {
			c.checkDiagnosticBlock(e.Body, inner, e.ReturnTypes, c.diagnosticPackages, reporter, filename)
		}
		return c.InferExprType(e, locals)
	}
	return TypeInt
}
