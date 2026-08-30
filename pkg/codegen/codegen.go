package codegen

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/sema"
)

type StringConst struct {
	Label  string
	Raw    string
	Value  string
	Length int
}

type Symbol struct {
	Name     string
	LLVMName string
	Type     sema.Type
}

type loopContext struct {
	breakLabel    string
	continueLabel string
}

type anonFuncMeta struct {
	Name       string
	Decl       *ast.FuncDecl
	EnvStruct  string
	Captures   []string
	OuterTypes map[string]sema.Type
	FuncType   *sema.FuncType
}

type CodeGenerator struct {
	prog           *ast.Program
	semaCtx        *sema.Context
	output         strings.Builder
	regCount       int
	labelCount     int
	anonFuncCount  int
	anonFuncs      []anonFuncMeta
	envStructDefs  []string
	itabDefs       []string
	generatedItabs map[string]string
	thunkList      []string
	emittedThunks  map[string]bool
	stringLiterals []StringConst
	symbols        map[string]Symbol
	deferStack     []*ast.CallExpr
	loopStack      []loopContext
	entryAllocas   *strings.Builder
	emittedFuncs   map[string]bool
	verbose        bool
}

func New(prog *ast.Program, semaCtx *sema.Context) *CodeGenerator {
	return &CodeGenerator{
		prog:           prog,
		semaCtx:        semaCtx,
		symbols:        make(map[string]Symbol),
		stringLiterals: []StringConst{},
		loopStack:      []loopContext{},
		deferStack:     []*ast.CallExpr{},
		anonFuncs:      []anonFuncMeta{},
		envStructDefs:  []string{},
		itabDefs:       []string{},
		generatedItabs: make(map[string]string),
		thunkList:      []string{},
		emittedThunks:  make(map[string]bool),
		emittedFuncs:   make(map[string]bool),
		verbose:        false,
	}
}

func (g *CodeGenerator) SetVerbose(v bool) {
	g.verbose = v
}

func (g *CodeGenerator) log(msg string) {
	if g.verbose {
		fmt.Printf("[CODEGEN] %s\n", msg)
	}
}

func (g *CodeGenerator) nextReg() string {
	g.regCount++
	return fmt.Sprintf("%%t%d", g.regCount)
}

func (g *CodeGenerator) nextLabel(prefix string) string {
	g.labelCount++
	return fmt.Sprintf("%s%d", prefix, g.labelCount)
}

func (g *CodeGenerator) getStringLiteral(str string) (string, int) {
	for _, sc := range g.stringLiterals {
		if sc.Raw == str {
			return sc.Label, sc.Length
		}
	}

	var encoded strings.Builder
	byteCount := 0
	runes := []rune(str)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]
		if r == '\\' && i+1 < n {
			nextR := runes[i+1]
			switch nextR {
			case 'n':
				encoded.WriteString("\\0A")
				byteCount++
				i++
				continue
			case 't':
				encoded.WriteString("\\09")
				byteCount++
				i++
				continue
			case 'r':
				encoded.WriteString("\\0D")
				byteCount++
				i++
				continue
			case '"':
				encoded.WriteString("\\22")
				byteCount++
				i++
				continue
			case '\\':
				encoded.WriteString("\\5C")
				byteCount++
				i++
				continue
			case '0':
				encoded.WriteString("\\00")
				byteCount++
				i++
				continue
			}
		}

		if r == '\n' {
			encoded.WriteString("\\0A")
			byteCount++
		} else if r == '\t' {
			encoded.WriteString("\\09")
			byteCount++
		} else if r == '\r' {
			encoded.WriteString("\\0D")
			byteCount++
		} else if r == '"' {
			encoded.WriteString("\\22")
			byteCount++
		} else {
			encoded.WriteRune(r)
			byteCount += len(string(r))
		}
	}

	encoded.WriteString("\\00")
	totalLen := byteCount + 1

	label := fmt.Sprintf("@.str.%d", len(g.stringLiterals)+1)
	g.stringLiterals = append(g.stringLiterals, StringConst{
		Label:  label,
		Raw:    str,
		Value:  encoded.String(),
		Length: totalLen,
	})
	return label, totalLen
}

func (g *CodeGenerator) lookupFunction(name string) *sema.FuncType {
	if fn, ok := g.semaCtx.Functions[name]; ok {
		return fn
	}
	for fnName, fn := range g.semaCtx.Functions {
		if strings.HasSuffix(fnName, "_"+name) {
			return fn
		}
	}
	return nil
}

func (g *CodeGenerator) findStructByName(name string) (*sema.StructType, string) {
	if st, ok := g.semaCtx.Structs[name]; ok {
		return st, name
	}
	for stName, st := range g.semaCtx.Structs {
		if strings.HasSuffix(stName, "_"+name) {
			return st, stName
		}
	}
	return nil, ""
}

func (g *CodeGenerator) findStruct(t sema.Type, fieldName string) (*sema.StructType, string) {
	if t == nil {
		return nil, ""
	}
	typeName := t.TypeName()
	typeName = strings.TrimPrefix(typeName, "*")

	if st, name := g.findStructByName(typeName); st != nil {
		return st, name
	}

	for stName, st := range g.semaCtx.Structs {
		for _, f := range st.Fields {
			if f.Name == fieldName {
				return st, stName
			}
		}
	}
	return nil, ""
}

func (g *CodeGenerator) resolveStructPtr(b *strings.Builder, expr ast.Expression) (string, sema.Type, *sema.StructType, string) {
	if ident, ok := expr.(*ast.Identifier); ok {
		if sym, exists := g.symbols[ident.Value]; exists {
			if strings.HasSuffix(sym.Type.LLVMType(), "*") {
				loadReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, sym.Type.LLVMType(), sym.Type.LLVMType(), sym.LLVMName))
				st, sName := g.findStruct(sym.Type, "")
				return loadReg, sym.Type, st, sName
			} else {
				st, sName := g.findStruct(sym.Type, "")
				return sym.LLVMName, &sema.PointerType{Base: sym.Type}, st, sName
			}
		}
	}

	if mem, ok := expr.(*ast.MemberExpr); ok {
		parentPtr, _, parentSt, parentName := g.resolveStructPtr(b, mem.Object)
		if parentSt != nil {
			gepReg, fieldType, _, found := g.resolveFieldPath(parentSt, parentName, parentPtr, mem.Field.Value, b)
			if found {
				st, sName := g.findStruct(fieldType, "")
				if _, isPtr := fieldType.(*sema.PointerType); isPtr {
					loadReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, fieldType.LLVMType(), fieldType.LLVMType(), gepReg))
					return loadReg, fieldType, st, sName
				}
				return gepReg, &sema.PointerType{Base: fieldType}, st, sName
			}
		}
	}

	valReg, valType := g.resolveValue(b, expr)
	if strings.HasSuffix(valType.LLVMType(), "*") {
		st, sName := g.findStruct(valType, "")
		return valReg, valType, st, sName
	}

	allocaReg := g.nextReg()
	g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", allocaReg, valType.LLVMType()))
	b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", valType.LLVMType(), valReg, valType.LLVMType(), allocaReg))
	st, sName := g.findStruct(valType, "")
	return allocaReg, &sema.PointerType{Base: valType}, st, sName
}

func (g *CodeGenerator) resolveFieldPath(st *sema.StructType, structName string, curPtrReg string, fieldName string, b *strings.Builder) (string, sema.Type, string, bool) {
	if st == nil {
		return "", nil, "", false
	}

	for i, f := range st.Fields {
		if f.Name == fieldName {
			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
				gepReg, structName, structName, curPtrReg, i))
			return gepReg, f.Type, structName, true
		}
	}

	for i, f := range st.Fields {
		if f.IsEmbedded {
			embTypeName := strings.TrimPrefix(f.Type.TypeName(), "*")
			embSt, embStructName := g.findStructByName(embTypeName)
			if embSt != nil {
				gepReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
					gepReg, structName, structName, curPtrReg, i))

				nextPtrReg := gepReg
				if _, isPtr := f.Type.(*sema.PointerType); isPtr {
					loadReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = load %%struct.%s*, %%struct.%s** %s\n",
						loadReg, embStructName, embStructName, gepReg))
					nextPtrReg = loadReg
				}

				if finalGep, finalType, sName, found := g.resolveFieldPath(embSt, embStructName, nextPtrReg, fieldName, b); found {
					return finalGep, finalType, sName, true
				}
			}
		}
	}

	return "", nil, "", false
}

func (g *CodeGenerator) resolveMethodPath(st *sema.StructType, structName string, curPtrReg string, methodName string, b *strings.Builder) (string, *sema.FuncType, string, bool) {
	if st == nil {
		return "", nil, "", false
	}

	directName := structName + "_" + methodName
	if fn := g.lookupFunction(directName); fn != nil {
		return directName, fn, curPtrReg, true
	}

	for i, f := range st.Fields {
		if f.IsEmbedded {
			embTypeName := strings.TrimPrefix(f.Type.TypeName(), "*")
			embSt, embStructName := g.findStructByName(embTypeName)
			if embSt != nil {
				gepReg := g.nextReg()
				if b != nil {
					b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
						gepReg, structName, structName, curPtrReg, i))
				}

				nextPtrReg := gepReg
				if _, isPtr := f.Type.(*sema.PointerType); isPtr {
					loadReg := g.nextReg()
					if b != nil {
						b.WriteString(fmt.Sprintf("  %s = load %%struct.%s*, %%struct.%s** %s\n",
							loadReg, embStructName, embStructName, gepReg))
					}
					nextPtrReg = loadReg
				}

				if targetName, fnMeta, finalPtr, found := g.resolveMethodPath(embSt, embStructName, nextPtrReg, methodName, b); found {
					return targetName, fnMeta, finalPtr, true
				}
			}
		}
	}

	return "", nil, "", false
}

func (g *CodeGenerator) getOrCreateThunk(fnName string) string {
	thunkName := fmt.Sprintf("__thunk_%s", fnName)
	if g.emittedThunks[thunkName] {
		return thunkName
	}
	g.emittedThunks[thunkName] = true
	g.thunkList = append(g.thunkList, fnName)
	return thunkName
}

func (g *CodeGenerator) emitPromotionThunk(structName string, methodName string, targetFnName string, method sema.Method) {
	thunkName := fmt.Sprintf("__promo_%s_%s", structName, methodName)
	if g.emittedThunks[thunkName] {
		return
	}
	g.emittedThunks[thunkName] = true

	var b strings.Builder
	st, _ := g.findStructByName(structName)

	retTypeStr := "void"
	if len(method.ReturnTypes) == 1 {
		retTypeStr = method.ReturnTypes[0].LLVMType()
	} else if len(method.ReturnTypes) > 1 {
		types := []string{}
		for _, rt := range method.ReturnTypes {
			types = append(types, rt.LLVMType())
		}
		retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
	}

	params := []string{fmt.Sprintf("%%struct.%s* %%self", structName)}
	callArgs := []string{}
	for i, pt := range method.ParamTypes {
		argName := fmt.Sprintf("%%a%d", i)
		params = append(params, fmt.Sprintf("%s %s", pt.LLVMType(), argName))
		callArgs = append(callArgs, fmt.Sprintf("%s %s", pt.LLVMType(), argName))
	}

	b.WriteString(fmt.Sprintf("define %s @%s(%s) {\nentry:\n", retTypeStr, thunkName, strings.Join(params, ", ")))
	_, targetFn, finalRecvPtr, _ := g.resolveMethodPath(st, structName, "%self", methodName, &b)

	var expectedRecvType string = "i8*"
	if targetFn != nil && len(targetFn.ParamTypes) > 0 {
		expectedRecvType = targetFn.ParamTypes[0].LLVMType()
	}

	allCallArgs := append([]string{fmt.Sprintf("%s %s", expectedRecvType, finalRecvPtr)}, callArgs...)

	if retTypeStr == "void" {
		b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", targetFnName, strings.Join(allCallArgs, ", ")))
		b.WriteString("  ret void\n")
	} else {
		resReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", resReg, retTypeStr, targetFnName, strings.Join(allCallArgs, ", ")))
		b.WriteString(fmt.Sprintf("  ret %s %s\n", retTypeStr, resReg))
	}
	b.WriteString("}\n\n")

	g.itabDefs = append(g.itabDefs, b.String())
}

func (g *CodeGenerator) getOrCreateItab(concreteType sema.Type, iface *sema.InterfaceType) string {
	if iface.IsAny() {
		return ""
	}
	structTypeName := strings.TrimPrefix(concreteType.TypeName(), "*")
	ifaceName := iface.Name
	if ifaceName == "" {
		ifaceName = "anon_iface"
	}
	key := fmt.Sprintf("%s_%s", concreteType.TypeName(), ifaceName)
	if globalName, ok := g.generatedItabs[key]; ok {
		return globalName
	}

	typeID := g.semaCtx.GetTypeID(concreteType)
	itabStructName := fmt.Sprintf("__itab_%s", ifaceName)
	methodPtrTypes := []string{"i64"}
	methodPtrValues := []string{fmt.Sprintf("i64 %d", typeID)}

	st, _ := g.findStructByName(structTypeName)

	for _, m := range iface.Methods {
		retTypeStr := "void"
		if len(m.ReturnTypes) == 1 {
			retTypeStr = m.ReturnTypes[0].LLVMType()
		} else if len(m.ReturnTypes) > 1 {
			types := []string{}
			for _, rt := range m.ReturnTypes {
				types = append(types, rt.LLVMType())
			}
			retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
		}

		sigParams := []string{"i8*"}
		for _, pt := range m.ParamTypes {
			sigParams = append(sigParams, pt.LLVMType())
		}
		rawSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(sigParams, ", "))
		methodPtrTypes = append(methodPtrTypes, rawSig)

		concreteFnName := fmt.Sprintf("%s_%s", structTypeName, m.Name)
		actualFn := g.lookupFunction(concreteFnName)
		if actualFn == nil && st != nil {
			if promoTarget, _, _, found := g.resolveMethodPath(st, structTypeName, "%self", m.Name, nil); found {
				thunkName := fmt.Sprintf("__promo_%s_%s", structTypeName, m.Name)
				g.emitPromotionThunk(structTypeName, m.Name, promoTarget, m)
				concreteFnName = thunkName
			}
		}

		actualSigParams := []string{fmt.Sprintf("%%struct.%s*", structTypeName)}
		for _, pt := range m.ParamTypes {
			actualSigParams = append(actualSigParams, pt.LLVMType())
		}
		concreteSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(actualSigParams, ", "))
		bitcastVal := fmt.Sprintf("%s bitcast (%s @%s to %s)", rawSig, concreteSig, concreteFnName, rawSig)
		methodPtrValues = append(methodPtrValues, bitcastVal)
	}

	itabTypeDecl := fmt.Sprintf("%%struct.%s = type { %s }\n", itabStructName, strings.Join(methodPtrTypes, ", "))
	hasDecl := false
	for _, def := range g.itabDefs {
		if strings.HasPrefix(def, fmt.Sprintf("%%struct.%s =", itabStructName)) {
			hasDecl = true
			break
		}
	}
	if !hasDecl {
		g.itabDefs = append(g.itabDefs, itabTypeDecl)
	}

	globalItabName := fmt.Sprintf("@__itab_%s_%s", structTypeName, ifaceName)
	itabValDef := fmt.Sprintf("%s = constant %%struct.%s { %s }\n", globalItabName, itabStructName, strings.Join(methodPtrValues, ", "))
	g.itabDefs = append(g.itabDefs, itabValDef)

	g.generatedItabs[key] = globalItabName
	return globalItabName
}

func (g *CodeGenerator) boxToInterface(b *strings.Builder, valReg string, valType sema.Type, iface *sema.InterfaceType) string {
	if _, srcIsIface := valType.(*sema.InterfaceType); srcIsIface {
		return valReg
	}

	if iface.IsAny() {
		typeID := g.semaCtx.GetTypeID(valType)
		dataPtr := g.nextReg()
		if strings.HasSuffix(valType.LLVMType(), "*") {
			b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", dataPtr, valType.LLVMType(), valReg))
		} else if valType.LLVMType() == "i64" {
			b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", dataPtr, valReg))
		} else if valType.LLVMType() == "i1" {
			zextReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", zextReg, valReg))
			b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to i8*\n", dataPtr, zextReg))
		} else {
			tempAlloca := g.nextReg()
			g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", tempAlloca, valType.LLVMType()))
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", valType.LLVMType(), valReg, valType.LLVMType(), tempAlloca))
			b.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", dataPtr, valType.LLVMType(), tempAlloca))
		}

		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i64 } undef, i8* %s, 0\n", t1, dataPtr))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i64 } %s, i64 %d, 1\n", t2, t1, typeID))
		return t2
	}

	globalItab := g.getOrCreateItab(valType, iface)
	if globalItab == "" {
		return "zeroinitializer"
	}

	dataPtr := g.nextReg()
	if strings.HasSuffix(valType.LLVMType(), "*") {
		b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", dataPtr, valType.LLVMType(), valReg))
	} else {
		tempAlloca := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", tempAlloca, valType.LLVMType()))
		b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", valType.LLVMType(), valReg, valType.LLVMType(), tempAlloca))
		b.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", dataPtr, valType.LLVMType(), tempAlloca))
	}

	itabPtr := g.nextReg()
	ifaceStructName := iface.Name
	if ifaceStructName == "" {
		ifaceStructName = "anon_iface"
	}
	b.WriteString(fmt.Sprintf("  %s = bitcast %%struct.__itab_%s* %s to i8*\n", itabPtr, ifaceStructName, globalItab))

	t1 := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } undef, i8* %s, 0\n", t1, dataPtr))
	t2 := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } %s, i8* %s, 1\n", t2, t1, itabPtr))
	return t2
}

func (g *CodeGenerator) emitArgConversion(b *strings.Builder, argReg string, argType sema.Type, targetType sema.Type) string {
	if argType == nil || targetType == nil {
		return argReg
	}

	if iface, ok := targetType.(*sema.InterfaceType); ok {
		if _, srcIsIface := argType.(*sema.InterfaceType); !srcIsIface {
			if argReg == "null" || argType == nil {
				return "zeroinitializer"
			}
			return g.boxToInterface(b, argReg, argType, iface)
		}
	}

	if argType.LLVMType() == targetType.LLVMType() {
		return argReg
	}

	if strings.HasSuffix(argType.LLVMType(), "*") && strings.HasSuffix(targetType.LLVMType(), "*") {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, argType.LLVMType(), argReg, targetType.LLVMType()))
		return convReg
	}

	if strings.HasSuffix(argType.LLVMType(), "*") && targetType.LLVMType() == "i64" {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", convReg, argType.LLVMType(), argReg))
		return convReg
	}

	if argType.LLVMType() == "i64" && strings.HasSuffix(targetType.LLVMType(), "*") {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", convReg, argReg, targetType.LLVMType()))
		return convReg
	}

	if argType.LLVMType() == "i1" && targetType.LLVMType() == "i64" {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", convReg, argReg))
		return convReg
	}

	return argReg
}

func (g *CodeGenerator) Generate() string {
	var bodyBuilder strings.Builder

	for _, decl := range g.prog.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Body != nil {
			g.emitFuncDecl(&bodyBuilder, fnDecl)
		}
	}

	for i := 0; i < len(g.anonFuncs); i++ {
		g.emitAnonFunc(&bodyBuilder, g.anonFuncs[i])
	}

	for _, fnName := range g.thunkList {
		g.emitThunk(&bodyBuilder, fnName)
	}

	g.output.Reset()
	g.emitPrologue()

	for _, sc := range g.stringLiterals {
		g.output.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\", align 1\n", sc.Label, sc.Length, sc.Value))
	}
	g.output.WriteString("\n")

	g.output.WriteString(bodyBuilder.String())
	return g.output.String()
}

func (g *CodeGenerator) emitPrologue() {
	g.output.WriteString("; ModuleID = 'hike'\n")
	g.output.WriteString("source_filename = \"hike\"\n")
	g.output.WriteString("target triple = \"x86_64-w64-windows-gnu\"\n\n")

	g.output.WriteString("declare noalias i8* @malloc(i64)\n")
	g.output.WriteString("declare noalias i8* @calloc(i64, i64)\n")
	g.output.WriteString("declare void @free(i8*)\n")
	g.output.WriteString("declare i32 @strcmp(i8*, i8*)\n")
	g.output.WriteString("declare i64 @strlen(i8*)\n")
	g.output.WriteString("declare i8* @memcpy(i8*, i8*, i64)\n")
	g.output.WriteString("declare i32 @memcmp(i8*, i8*, i64)\n")
	g.output.WriteString("declare i64 @printf(i8*, ...)\n")

	for _, fn := range g.semaCtx.Functions {
		if fn.IsExtern {
			if fn.Name == "malloc" || fn.Name == "free" || fn.Name == "calloc" || fn.Name == "strcmp" || fn.Name == "strlen" || fn.Name == "memcpy" || fn.Name == "memcmp" || fn.Name == "printf" {
				continue
			}
			retType := "void"
			if len(fn.ReturnTypes) == 1 {
				retType = fn.ReturnTypes[0].LLVMType()
			}
			paramTypes := []string{}
			for _, p := range fn.ParamTypes {
				paramTypes = append(paramTypes, p.LLVMType())
			}
			if fn.IsVariadic {
				paramTypes = append(paramTypes, "...")
			}
			g.output.WriteString(fmt.Sprintf("declare %s @%s(%s)\n", retType, fn.Name, strings.Join(paramTypes, ", ")))
		}
	}
	g.output.WriteString("\n")

	for _, st := range g.semaCtx.Structs {
		fields := make([]string, len(st.Fields))
		for i, f := range st.Fields {
			fields[i] = f.Type.LLVMType()
		}
		g.output.WriteString(fmt.Sprintf("%%struct.%s = type { %s }\n", st.Name, strings.Join(fields, ", ")))
	}

	if _, ok := g.semaCtx.Structs["Arena"]; !ok {
		g.output.WriteString("%struct.Arena = type { i8*, i64, i64 }\n")
	}
	if _, ok := g.semaCtx.Structs["Allocator"]; !ok {
		g.output.WriteString("%struct.Allocator = type { i8*, i8* }\n")
	}

	for _, envDef := range g.envStructDefs {
		g.output.WriteString(envDef)
	}

	for _, itabDef := range g.itabDefs {
		g.output.WriteString(itabDef)
	}
	g.output.WriteString("\n")

	for name, gType := range g.semaCtx.Globals {
		g.output.WriteString(fmt.Sprintf("@%s = global %s 0, align 8\n", name, gType.LLVMType()))
	}
	g.output.WriteString("\n")

	g.output.WriteString("define i1 @hike_streq(i8* %a, i8* %b) {\n")
	g.output.WriteString("entry:\n")
	g.output.WriteString("  %eq_ptr = icmp eq i8* %a, %b\n")
	g.output.WriteString("  br i1 %eq_ptr, label %ret_true, label %check_null\n")
	g.output.WriteString("check_null:\n")
	g.output.WriteString("  %a_null = icmp eq i8* %a, null\n")
	g.output.WriteString("  %b_null = icmp eq i8* %b, null\n")
	g.output.WriteString("  %either_null = or i1 %a_null, %b_null\n")
	g.output.WriteString("  br i1 %either_null, label %ret_false, label %do_strcmp\n")
	g.output.WriteString("do_strcmp:\n")
	g.output.WriteString("  %res = call i32 @strcmp(i8* %a, i8* %b)\n")
	g.output.WriteString("  %is_zero = icmp eq i32 %res, 0\n")
	g.output.WriteString("  ret i1 %is_zero\n")
	g.output.WriteString("ret_true:\n")
	g.output.WriteString("  ret i1 true\n")
	g.output.WriteString("ret_false:\n")
	g.output.WriteString("  ret i1 false\n")
	g.output.WriteString("}\n\n")

	g.output.WriteString("define i8* @hike_substr(i8* %s, i64 %low, i64 %high) {\n")
	g.output.WriteString("entry:\n")
	g.output.WriteString("  %s_null = icmp eq i8* %s, null\n")
	g.output.WriteString("  br i1 %s_null, label %ret_null, label %do_sub\n")
	g.output.WriteString("do_sub:\n")
	g.output.WriteString("  %len = sub i64 %high, %low\n")
	g.output.WriteString("  %alloc_size = add i64 %len, 1\n")
	g.output.WriteString("  %buf = call i8* @malloc(i64 %alloc_size)\n")
	g.output.WriteString("  %src_ptr = getelementptr inbounds i8, i8* %s, i64 %low\n")
	g.output.WriteString("  call i8* @memcpy(i8* %buf, i8* %src_ptr, i64 %len)\n")
	g.output.WriteString("  %null_pos = getelementptr inbounds i8, i8* %buf, i64 %len\n")
	g.output.WriteString("  store i8 0, i8* %null_pos\n")
	g.output.WriteString("  ret i8* %buf\n")
	g.output.WriteString("ret_null:\n")
	g.output.WriteString("  ret i8* null\n")
	g.output.WriteString("}\n\n")

	g.output.WriteString("define i8* @hike_strcat(i8* %a, i8* %b) {\n")
	g.output.WriteString("entry:\n")
	g.output.WriteString("  %len_a = call i64 @strlen(i8* %a)\n")
	g.output.WriteString("  %len_b = call i64 @strlen(i8* %b)\n")
	g.output.WriteString("  %total_len = add i64 %len_a, %len_b\n")
	g.output.WriteString("  %alloc_size = add i64 %total_len, 1\n")
	g.output.WriteString("  %buf = call i8* @malloc(i64 %alloc_size)\n")
	g.output.WriteString("  call i8* @memcpy(i8* %buf, i8* %a, i64 %len_a)\n")
	g.output.WriteString("  %dst_b = getelementptr inbounds i8, i8* %buf, i64 %len_a\n")
	g.output.WriteString("  call i8* @memcpy(i8* %dst_b, i8* %b, i64 %len_b)\n")
	g.output.WriteString("  %null_ptr = getelementptr inbounds i8, i8* %buf, i64 %total_len\n")
	g.output.WriteString("  store i8 0, i8* %null_ptr\n")
	g.output.WriteString("  ret i8* %buf\n")
	g.output.WriteString("}\n\n")
}

func (g *CodeGenerator) scanCaptures(fl *ast.FuncLit) []string {
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
				if _, isConst := g.semaCtx.Constants[name]; !isConst {
					if _, isGlobal := g.semaCtx.Globals[name]; !isGlobal {
						if _, isFn := g.semaCtx.Functions[name]; !isFn {
							if name != "true" && name != "false" && name != "nil" &&
								name != "len" && name != "cap" && name != "append" &&
								name != "int" && name != "byte" && name != "string" && name != "bool" {
								if _, isOuter := g.symbols[name]; isOuter {
									seen[name] = true
									captured = append(captured, name)
								}
							}
						}
					}
				}
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
			if st.Variable != nil {
				locals[st.Variable.Value] = true
			}
			for _, cc := range st.Cases {
				for _, bs := range cc.Body {
					walkStmt(bs)
				}
			}
		}
	}

	if fl.Body != nil {
		for _, s := range fl.Body.Statements {
			walkStmt(s)
		}
	}

	return captured
}

func (g *CodeGenerator) emitThunk(b *strings.Builder, fnName string) {
	fnMeta := g.lookupFunction(fnName)
	if fnMeta == nil {
		return
	}

	thunkName := fmt.Sprintf("__thunk_%s", fnName)
	retType := "void"
	if len(fnMeta.ReturnTypes) == 1 {
		retType = fnMeta.ReturnTypes[0].LLVMType()
	} else if len(fnMeta.ReturnTypes) > 1 {
		types := []string{}
		for _, rt := range fnMeta.ReturnTypes {
			types = append(types, rt.LLVMType())
		}
		retType = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
	}

	params := []string{"i8* %__env"}
	callArgs := []string{}
	for i, pt := range fnMeta.ParamTypes {
		argName := fmt.Sprintf("%%a%d", i)
		params = append(params, fmt.Sprintf("%s %s", pt.LLVMType(), argName))
		callArgs = append(callArgs, fmt.Sprintf("%s %s", pt.LLVMType(), argName))
	}

	b.WriteString(fmt.Sprintf("define %s @%s(%s) {\nentry:\n", retType, thunkName, strings.Join(params, ", ")))
	if retType == "void" {
		b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", fnName, strings.Join(callArgs, ", ")))
		b.WriteString("  ret void\n")
	} else {
		resReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", resReg, retType, fnName, strings.Join(callArgs, ", ")))
		b.WriteString(fmt.Sprintf("  ret %s %s\n", retType, resReg))
	}
	b.WriteString("}\n\n")
}

func (g *CodeGenerator) emitAnonFunc(b *strings.Builder, meta anonFuncMeta) {
	g.symbols = make(map[string]Symbol)
	oldDeferStack := g.deferStack
	g.deferStack = []*ast.CallExpr{}
	defer func() {
		g.deferStack = oldDeferStack
	}()

	retType := "void"
	if len(meta.FuncType.ReturnTypes) == 1 {
		retType = meta.FuncType.ReturnTypes[0].LLVMType()
	} else if len(meta.FuncType.ReturnTypes) > 1 {
		types := []string{}
		for _, rt := range meta.FuncType.ReturnTypes {
			types = append(types, rt.LLVMType())
		}
		retType = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
	}

	params := []string{"i8* %__env_arg"}
	for _, p := range meta.Decl.Params {
		pType := g.semaCtx.ResolveType(p.Type)
		params = append(params, fmt.Sprintf("%s %%%s_arg", pType.LLVMType(), p.Name.Value))
	}

	var entryAllocas strings.Builder
	var bodyBuilder strings.Builder
	g.entryAllocas = &entryAllocas

	if len(meta.Captures) > 0 {
		envPtr := g.nextReg()
		bodyBuilder.WriteString(fmt.Sprintf("  %s = bitcast i8* %%__env_arg to %%struct.%s*\n", envPtr, meta.EnvStruct))
		for idx, capName := range meta.Captures {
			cType := meta.OuterTypes[capName]
			gepReg := g.nextReg()
			bodyBuilder.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
				gepReg, meta.EnvStruct, meta.EnvStruct, envPtr, idx))
			loadPtrReg := g.nextReg()
			bodyBuilder.WriteString(fmt.Sprintf("  %s = load %s*, %s** %s\n", loadPtrReg, cType.LLVMType(), cType.LLVMType(), gepReg))
			g.symbols[capName] = Symbol{
				Name:     capName,
				LLVMName: loadPtrReg,
				Type:     cType,
			}
		}
	}

	for _, p := range meta.Decl.Params {
		pType := g.semaCtx.ResolveType(p.Type)
		entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", p.Name.Value, pType.LLVMType()))
		bodyBuilder.WriteString(fmt.Sprintf("  store %s %%%s_arg, %s* %%%s\n", pType.LLVMType(), p.Name.Value, pType.LLVMType(), p.Name.Value))
		g.symbols[p.Name.Value] = Symbol{
			Name:     p.Name.Value,
			LLVMName: "%" + p.Name.Value,
			Type:     pType,
		}
	}

	for _, s := range meta.Decl.Body.Statements {
		g.emitStatement(&bodyBuilder, s, meta.Name)
	}

	for i := len(g.deferStack) - 1; i >= 0; i-- {
		g.emitCallExpr(&bodyBuilder, g.deferStack[i])
	}

	if retType == "void" {
		bodyBuilder.WriteString("  ret void\n")
	} else if retType == "i32" {
		bodyBuilder.WriteString("  ret i32 0\n")
	} else if retType == "i64" {
		bodyBuilder.WriteString("  ret i64 0\n")
	} else if retType == "i1" {
		bodyBuilder.WriteString("  ret i1 false\n")
	} else if strings.HasSuffix(retType, "*") {
		bodyBuilder.WriteString(fmt.Sprintf("  ret %s null\n", retType))
	} else {
		bodyBuilder.WriteString(fmt.Sprintf("  ret %s zeroinitializer\n", retType))
	}

	b.WriteString(fmt.Sprintf("define %s @%s(%s) {\nentry:\n", retType, meta.Name, strings.Join(params, ", ")))
	b.WriteString(entryAllocas.String())
	b.WriteString(bodyBuilder.String())
	b.WriteString("}\n\n")
}

func (g *CodeGenerator) emitFuncDecl(b *strings.Builder, fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
	}

	g.symbols = make(map[string]Symbol)
	oldDeferStack := g.deferStack
	g.deferStack = []*ast.CallExpr{}
	defer func() {
		g.deferStack = oldDeferStack
	}()

	funcMangledName := fn.Name.Value
	var recvType sema.Type = nil
	var recvTypeName string = ""

	if fn.Receiver != nil {
		recvType = g.semaCtx.ResolveType(fn.Receiver.Type)
		if named, ok := fn.Receiver.Type.(*ast.NamedType); ok {
			recvTypeName = named.Name.Value
		} else if pt, ok := fn.Receiver.Type.(*ast.PointerType); ok {
			if named, ok := pt.Base.(*ast.NamedType); ok {
				recvTypeName = named.Name.Value
			}
		}
		if recvType == nil {
			recvType = &sema.PointerType{Base: sema.TypeByte}
		}
		if strings.Contains(funcMangledName, "_") {
			parts := strings.SplitN(funcMangledName, "_", 2)
			funcMangledName = parts[0] + "_" + recvTypeName + "_" + parts[1]
		} else {
			funcMangledName = recvTypeName + "_" + funcMangledName
		}
	}

	if g.emittedFuncs[funcMangledName] {
		return
	}
	g.emittedFuncs[funcMangledName] = true
	g.log(fmt.Sprintf("Emitting function: %s", funcMangledName))

	fnMeta, exists := g.semaCtx.Functions[fn.Name.Value]
	if !exists && fn.Receiver != nil {
		fnMeta, exists = g.semaCtx.Functions[funcMangledName]
	}

	retType := "void"
	if exists && len(fnMeta.ReturnTypes) == 1 {
		retType = fnMeta.ReturnTypes[0].LLVMType()
	} else if exists && len(fnMeta.ReturnTypes) > 1 {
		types := []string{}
		for _, rt := range fnMeta.ReturnTypes {
			types = append(types, rt.LLVMType())
		}
		retType = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
	} else if len(fn.ReturnTypes) == 1 {
		rT := g.semaCtx.ResolveType(fn.ReturnTypes[0])
		retType = rT.LLVMType()
	}

	if fn.Name.Value == "main" {
		retType = "i32"
	}

	params := []string{}
	if fn.Receiver != nil {
		params = append(params, fmt.Sprintf("%s %%%s_arg", recvType.LLVMType(), fn.Receiver.Name.Value))
	}

	for i, p := range fn.Params {
		var pType sema.Type = sema.TypeInt
		paramIdx := i
		if fn.Receiver != nil {
			paramIdx = i + 1
		}
		if exists && paramIdx < len(fnMeta.ParamTypes) {
			pType = fnMeta.ParamTypes[paramIdx]
		} else {
			pType = g.semaCtx.ResolveType(p.Type)
		}
		params = append(params, fmt.Sprintf("%s %%%s_arg", pType.LLVMType(), p.Name.Value))
	}

	var entryAllocas strings.Builder
	var bodyBuilder strings.Builder
	g.entryAllocas = &entryAllocas

	if fn.Receiver != nil {
		recvName := fn.Receiver.Name.Value
		entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", recvName, recvType.LLVMType()))
		bodyBuilder.WriteString(fmt.Sprintf("  store %s %%%s_arg, %s* %%%s\n", recvType.LLVMType(), recvName, recvType.LLVMType(), recvName))
		g.symbols[recvName] = Symbol{
			Name:     recvName,
			LLVMName: "%" + recvName,
			Type:     recvType,
		}
	}

	for i, p := range fn.Params {
		var pType sema.Type = sema.TypeInt
		paramIdx := i
		if fn.Receiver != nil {
			paramIdx = i + 1
		}
		if exists && paramIdx < len(fnMeta.ParamTypes) {
			pType = fnMeta.ParamTypes[paramIdx]
		} else {
			pType = g.semaCtx.ResolveType(p.Type)
		}

		entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", p.Name.Value, pType.LLVMType()))
		bodyBuilder.WriteString(fmt.Sprintf("  store %s %%%s_arg, %s* %%%s\n", pType.LLVMType(), p.Name.Value, pType.LLVMType(), p.Name.Value))
		g.symbols[p.Name.Value] = Symbol{
			Name:     p.Name.Value,
			LLVMName: "%" + p.Name.Value,
			Type:     pType,
		}
	}

	for _, s := range fn.Body.Statements {
		g.emitStatement(&bodyBuilder, s, funcMangledName)
	}

	for i := len(g.deferStack) - 1; i >= 0; i-- {
		g.emitCallExpr(&bodyBuilder, g.deferStack[i])
	}

	if retType == "void" {
		bodyBuilder.WriteString("  ret void\n")
	} else if retType == "i32" {
		bodyBuilder.WriteString("  ret i32 0\n")
	} else if retType == "i64" {
		bodyBuilder.WriteString("  ret i64 0\n")
	} else if retType == "i1" {
		bodyBuilder.WriteString("  ret i1 false\n")
	} else if strings.HasSuffix(retType, "*") {
		bodyBuilder.WriteString(fmt.Sprintf("  ret %s null\n", retType))
	} else {
		bodyBuilder.WriteString(fmt.Sprintf("  ret %s zeroinitializer\n", retType))
	}

	b.WriteString(fmt.Sprintf("define %s @%s(%s) {\nentry:\n", retType, funcMangledName, strings.Join(params, ", ")))
	b.WriteString(entryAllocas.String())
	b.WriteString(bodyBuilder.String())
	b.WriteString("}\n\n")
}

func (g *CodeGenerator) emitStatement(b *strings.Builder, s ast.Statement, currentFn string) {
	if s == nil {
		return
	}

	switch s := s.(type) {
	case *ast.VarDecl:
		g.emitVarDecl(b, s)
	case *ast.ExprStmt:
		g.resolveValue(b, s.Expr)
	case *ast.AssignStmt:
		g.emitAssignStmt(b, s)
	case *ast.BlockStmt:
		for _, stmt := range s.Statements {
			g.emitStatement(b, stmt, currentFn)
		}
	case *ast.IfStmt:
		g.emitIfStmt(b, s, currentFn)
	case *ast.ForStmt:
		g.emitForStmt(b, s, currentFn)
	case *ast.ForRangeStmt:
		g.emitForRangeStmt(b, s, currentFn)
	case *ast.SwitchStmt:
		g.emitSwitchStmt(b, s, currentFn)
	case *ast.TypeSwitchStmt:
		g.emitTypeSwitchStmt(b, s, currentFn)
	case *ast.ReturnStmt:
		g.emitReturnStmt(b, s, currentFn)
	case *ast.DeferStmt:
		if s.Call != nil {
			g.deferStack = append(g.deferStack, s.Call)
		}
	case *ast.BreakStmt:
		if len(g.loopStack) > 0 {
			ctx := g.loopStack[len(g.loopStack)-1]
			b.WriteString(fmt.Sprintf("  br label %%%s\n\n", ctx.breakLabel))
		}
	case *ast.ContinueStmt:
		if len(g.loopStack) > 0 {
			ctx := g.loopStack[len(g.loopStack)-1]
			b.WriteString(fmt.Sprintf("  br label %%%s\n\n", ctx.continueLabel))
		}
	}
}

func (g *CodeGenerator) emitVarDecl(b *strings.Builder, s *ast.VarDecl) {
	name := s.Name.Value
	var valReg string
	var valType sema.Type

	if s.Value != nil {
		valReg, valType = g.resolveValue(b, s.Value)
	}

	targetType := valType
	if s.Type != nil {
		resolvedTarget := g.semaCtx.ResolveType(s.Type)
		if resolvedTarget != nil && resolvedTarget != sema.TypeVoid {
			targetType = resolvedTarget
		}
	}

	if targetType == nil {
		targetType = sema.TypeInt
	}

	g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", name, targetType.LLVMType()))
	g.symbols[name] = Symbol{Name: name, LLVMName: "%" + name, Type: targetType}

	if s.Value != nil {
		finalValReg := g.emitArgConversion(b, valReg, valType, targetType)
		b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", targetType.LLVMType(), finalValReg, targetType.LLVMType(), name))
	} else {
		b.WriteString(fmt.Sprintf("  store %s zeroinitializer, %s* %%%s\n", targetType.LLVMType(), targetType.LLVMType(), name))
	}
}

func (g *CodeGenerator) emitAssignStmt(b *strings.Builder, s *ast.AssignStmt) {
	if len(s.Left) > 1 && len(s.Right) == 1 {
		rhsReg, rhsType := g.resolveValue(b, s.Right[0])

		var tupleElemTypes []sema.Type
		if tup, ok := rhsType.(*sema.TupleType); ok {
			tupleElemTypes = tup.Types
		}

		for i, leftExpr := range s.Left {
			if lhsIdent, ok := leftExpr.(*ast.Identifier); ok {
				if lhsIdent.Value == "_" {
					continue
				}

				var elemType sema.Type = sema.TypeInt
				if i < len(tupleElemTypes) {
					elemType = tupleElemTypes[i]
				}

				if _, exists := g.symbols[lhsIdent.Value]; !exists {
					g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", lhsIdent.Value, elemType.LLVMType()))
					g.symbols[lhsIdent.Value] = Symbol{Name: lhsIdent.Value, LLVMName: "%" + lhsIdent.Value, Type: elemType}
				}
				sym := g.symbols[lhsIdent.Value]

				elemReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, %d\n", elemReg, rhsType.LLVMType(), rhsReg, i))
				b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", sym.Type.LLVMType(), elemReg, sym.Type.LLVMType(), lhsIdent.Value))
			}
		}
		return
	}

	if len(s.Left) == 1 && len(s.Right) == 1 {
		if lhsIdent, ok := s.Left[0].(*ast.Identifier); ok && lhsIdent.Value == "_" {
			g.resolveValue(b, s.Right[0])
			return
		}

		op := s.Token.Literal
		if op == "++" {
			op = "+="
		} else if op == "--" {
			op = "-="
		}

		isCompound := (op == "+=" || op == "-=" || op == "*=" || op == "/=" ||
			op == "&=" || op == "|=" || op == "^=" || op == "<<=" || op == ">>=")

		if lhsIndex, isLhsIndex := s.Left[0].(*ast.IndexExpr); isLhsIndex {
			baseReg, baseType := g.resolveValue(b, lhsIndex.Left)
			idxReg, _ := g.resolveValue(b, lhsIndex.Index)
			valReg, valType := g.resolveValue(b, s.Right[0])

			if arr, isArr := baseType.(*sema.ArrayType); isArr {
				arrPtrReg := baseReg
				if objIdent, isIdent := lhsIndex.Left.(*ast.Identifier); isIdent {
					if sym, ok := g.symbols[objIdent.Value]; ok {
						arrPtrReg = sym.LLVMName
					}
				}

				gepReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
					gepReg, arr.LLVMType(), arr.LLVMType(), arrPtrReg, idxReg))

				finalValReg := valReg
				if isCompound {
					oldValReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", oldValReg, arr.Elem.LLVMType(), arr.Elem.LLVMType(), gepReg))
					calcReg := g.nextReg()
					opInst := "add"
					switch op {
					case "+=":
						opInst = "add"
					case "-=":
						opInst = "sub"
					case "*=":
						opInst = "mul"
					case "/=":
						opInst = "sdiv"
					case "&=":
						opInst = "and"
					case "|=":
						opInst = "or"
					case "^=":
						opInst = "xor"
					case "<<=":
						opInst = "shl"
					case ">>=":
						opInst = "ashr"
					}
					b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", calcReg, opInst, arr.Elem.LLVMType(), oldValReg, valReg))
					finalValReg = calcReg
				} else {
					if valType != nil && valType.LLVMType() != arr.Elem.LLVMType() {
						convReg := g.nextReg()
						if arr.Elem.LLVMType() == "i8" && valType.LLVMType() == "i64" {
							b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", convReg, valReg))
							finalValReg = convReg
						} else if arr.Elem.LLVMType() == "i64" && valType.LLVMType() == "i8" {
							b.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", convReg, valReg))
							finalValReg = convReg
						}
					}
				}
				b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", arr.Elem.LLVMType(), finalValReg, arr.Elem.LLVMType(), gepReg))
				return
			}

			var dataPtrReg string
			var elemType sema.Type = sema.TypeByte

			if sl, isSlice := baseType.(*sema.SliceType); isSlice {
				elemType = sl.Elem
				dataPtrReg = g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtrReg, sl.LLVMType(), baseReg))
			} else {
				dataPtrReg = baseReg
			}

			typedPtrReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedPtrReg, dataPtrReg, elemType.LLVMType()))

			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", gepReg, elemType.LLVMType(), elemType.LLVMType(), typedPtrReg, idxReg))

			finalValReg := valReg
			if isCompound {
				oldValReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", oldValReg, elemType.LLVMType(), elemType.LLVMType(), gepReg))
				calcReg := g.nextReg()
				opInst := "add"
				switch op {
				case "+=":
					opInst = "add"
				case "-=":
					opInst = "sub"
				case "*=":
					opInst = "mul"
				case "/=":
					opInst = "sdiv"
				case "&=":
					opInst = "and"
				case "|=":
					opInst = "or"
				case "^=":
					opInst = "xor"
				case "<<=":
					opInst = "shl"
				case ">>=":
					opInst = "ashr"
				}
				b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", calcReg, opInst, elemType.LLVMType(), oldValReg, valReg))
				finalValReg = calcReg
			} else {
				if valType != nil && valType.LLVMType() != elemType.LLVMType() {
					convReg := g.nextReg()
					if elemType.LLVMType() == "i8" && valType.LLVMType() == "i64" {
						b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", convReg, valReg))
						finalValReg = convReg
					} else if elemType.LLVMType() == "i64" && valType.LLVMType() == "i8" {
						b.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", convReg, valReg))
						finalValReg = convReg
					} else if strings.HasSuffix(elemType.LLVMType(), "*") && strings.HasSuffix(valType.LLVMType(), "*") {
						b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, elemType.LLVMType()))
						finalValReg = convReg
					}
				}
			}

			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemType.LLVMType(), finalValReg, elemType.LLVMType(), gepReg))
			return
		}

		if lhsIdent, isLhsIdent := s.Left[0].(*ast.Identifier); isLhsIdent {
			if gType, isGlobal := g.semaCtx.Globals[lhsIdent.Value]; isGlobal {
				valReg, valType := g.resolveValue(b, s.Right[0])
				finalValReg := valReg
				if isCompound {
					oldValReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = load %s, %s* @%s\n", oldValReg, gType.LLVMType(), gType.LLVMType(), lhsIdent.Value))
					calcReg := g.nextReg()
					opInst := "add"
					switch op {
					case "+=":
						opInst = "add"
					case "-=":
						opInst = "sub"
					case "*=":
						opInst = "mul"
					case "/=":
						opInst = "sdiv"
					case "&=":
						opInst = "and"
					case "|=":
						opInst = "or"
					case "^=":
						opInst = "xor"
					case "<<=":
						opInst = "shl"
					case ">>=":
						opInst = "ashr"
					}
					b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", calcReg, opInst, gType.LLVMType(), oldValReg, valReg))
					finalValReg = calcReg
				} else {
					finalValReg = g.emitArgConversion(b, valReg, valType, gType)
				}
				b.WriteString(fmt.Sprintf("  store %s %s, %s* @%s\n", gType.LLVMType(), finalValReg, gType.LLVMType(), lhsIdent.Value))
				return
			}

			if call, ok := s.Right[0].(*ast.CallExpr); ok {
				if memExpr, ok := call.Function.(*ast.MemberExpr); ok && memExpr.Field.Value == "NewArena" {
					g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %%struct.Arena\n", lhsIdent.Value))
					b.WriteString(fmt.Sprintf("  call void @hike_arena_init(%%struct.Arena* %%%s, i64 65536)\n", lhsIdent.Value))
					g.symbols[lhsIdent.Value] = Symbol{Name: lhsIdent.Value, LLVMName: "%" + lhsIdent.Value, Type: &sema.BasicType{Name: "Arena", LLVM: "%struct.Arena"}}
					return
				}
			}

			valReg, valType := g.resolveValue(b, s.Right[0])

			targetType := valType
			if s.Type != nil {
				resolvedTarget := g.semaCtx.ResolveType(s.Type)
				if resolvedTarget != nil && resolvedTarget != sema.TypeVoid {
					targetType = resolvedTarget
				}
			}

			if _, exists := g.symbols[lhsIdent.Value]; !exists {
				g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", lhsIdent.Value, targetType.LLVMType()))
				g.symbols[lhsIdent.Value] = Symbol{Name: lhsIdent.Value, LLVMName: "%" + lhsIdent.Value, Type: targetType}
			}
			sym := g.symbols[lhsIdent.Value]
			targetType = sym.Type

			finalValReg := g.emitArgConversion(b, valReg, valType, targetType)

			if isCompound {
				oldValReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = load %s, %s* %%%s\n", oldValReg, sym.Type.LLVMType(), sym.Type.LLVMType(), lhsIdent.Value))
				calcReg := g.nextReg()
				opInst := "add"
				switch op {
				case "+=":
					opInst = "add"
				case "-=":
					opInst = "sub"
				case "*=":
					opInst = "mul"
				case "/=":
					opInst = "sdiv"
				case "&=":
					opInst = "and"
				case "|=":
					opInst = "or"
				case "^=":
					opInst = "xor"
				case "<<=":
					opInst = "shl"
				case ">>=":
					opInst = "ashr"
				}
				b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", calcReg, opInst, sym.Type.LLVMType(), oldValReg, valReg))
				finalValReg = calcReg
			}

			b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", sym.Type.LLVMType(), finalValReg, sym.Type.LLVMType(), lhsIdent.Value))
			return
		}

		if lhsMember, isLhsMember := s.Left[0].(*ast.MemberExpr); isLhsMember {
			objPtrReg, _, st, structName := g.resolveStructPtr(b, lhsMember.Object)
			if st == nil {
				panic(fmt.Sprintf("[Codegen Error] cannot assign to field %s on non-struct", lhsMember.Field.Value))
			}

			gepReg, fieldType, _, found := g.resolveFieldPath(st, structName, objPtrReg, lhsMember.Field.Value, b)
			if !found {
				panic(fmt.Sprintf("[Codegen Error] unknown field %s in struct %s", lhsMember.Field.Value, structName))
			}

			valReg, valType := g.resolveValue(b, s.Right[0])

			finalValReg := valReg
			if isCompound {
				oldValReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", oldValReg, fieldType.LLVMType(), fieldType.LLVMType(), gepReg))
				calcReg := g.nextReg()
				opInst := "add"
				switch op {
				case "+=":
					opInst = "add"
				case "-=":
					opInst = "sub"
				case "*=":
					opInst = "mul"
				case "/=":
					opInst = "sdiv"
				case "&=":
					opInst = "and"
				case "|=":
					opInst = "or"
				case "^=":
					opInst = "xor"
				case "<<=":
					opInst = "shl"
				case ">>=":
					opInst = "ashr"
				}
				b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", calcReg, opInst, fieldType.LLVMType(), oldValReg, valReg))
				finalValReg = calcReg
			} else {
				finalValReg = g.emitArgConversion(b, valReg, valType, fieldType)
			}

			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n",
				fieldType.LLVMType(), finalValReg, fieldType.LLVMType(), gepReg))
			return
		}
	}
}

func (g *CodeGenerator) emitIfStmt(b *strings.Builder, s *ast.IfStmt, currentFn string) {
	if s.Init != nil {
		g.emitStatement(b, s.Init, currentFn)
	}

	condReg, condType := g.resolveValue(b, s.Condition)

	finalCondReg := condReg
	if condType != nil && condType.LLVMType() != "i1" {
		cmpReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0\n", cmpReg, condType.LLVMType(), condReg))
		finalCondReg = cmpReg
	}

	thenLabel := g.nextLabel("if.then")
	elseLabel := g.nextLabel("if.else")
	endLabel := g.nextLabel("if.end")

	if s.Alternative != nil {
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", finalCondReg, thenLabel, elseLabel))
	} else {
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", finalCondReg, thenLabel, endLabel))
	}

	b.WriteString(fmt.Sprintf("%s:\n", thenLabel))
	g.emitStatement(b, s.Consequence, currentFn)
	if !strings.HasSuffix(strings.TrimSpace(b.String()), "ret void") &&
		!strings.HasSuffix(strings.TrimSpace(b.String()), "ret i32") &&
		!strings.HasSuffix(strings.TrimSpace(b.String()), "ret i64") &&
		!strings.HasSuffix(strings.TrimSpace(b.String()), "ret i1") &&
		!strings.Contains(strings.TrimSpace(b.String()), "br label") {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))
	} else {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))
	}

	if s.Alternative != nil {
		b.WriteString(fmt.Sprintf("%s:\n", elseLabel))
		g.emitStatement(b, s.Alternative, currentFn)
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))
	}

	b.WriteString(fmt.Sprintf("%s:\n", endLabel))
}

func (g *CodeGenerator) emitForStmt(b *strings.Builder, s *ast.ForStmt, currentFn string) {
	if s.Init != nil {
		g.emitStatement(b, s.Init, currentFn)
	}

	condLabel := g.nextLabel("for.cond")
	bodyLabel := g.nextLabel("for.body")
	postLabel := g.nextLabel("for.post")
	endLabel := g.nextLabel("for.end")

	g.loopStack = append(g.loopStack, loopContext{breakLabel: endLabel, continueLabel: postLabel})
	defer func() {
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
	}()

	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", condLabel))

	b.WriteString(fmt.Sprintf("%s:\n", condLabel))
	if s.Cond != nil {
		condReg, condType := g.resolveValue(b, s.Cond)
		finalCondReg := condReg
		if condType != nil && condType.LLVMType() != "i1" {
			cmpReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0\n", cmpReg, condType.LLVMType(), condReg))
			finalCondReg = cmpReg
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", finalCondReg, bodyLabel, endLabel))
	} else {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", bodyLabel))
	}

	b.WriteString(fmt.Sprintf("%s:\n", bodyLabel))
	g.emitStatement(b, s.Body, currentFn)
	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", postLabel))

	b.WriteString(fmt.Sprintf("%s:\n", postLabel))
	if s.Post != nil {
		g.emitStatement(b, s.Post, currentFn)
	}
	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", condLabel))

	b.WriteString(fmt.Sprintf("%s:\n", endLabel))
}

func (g *CodeGenerator) emitForRangeStmt(b *strings.Builder, s *ast.ForRangeStmt, currentFn string) {
	xReg, xType := g.resolveValue(b, s.X)
	var lenReg string
	var dataPtrReg string
	var elemType sema.Type = sema.TypeByte

	if sl, isSlice := xType.(*sema.SliceType); isSlice {
		elemType = sl.Elem
		dataPtrReg = g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtrReg, sl.LLVMType(), xReg))
		lenReg = g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", lenReg, sl.LLVMType(), xReg))
	} else if arr, isArr := xType.(*sema.ArrayType); isArr {
		elemType = arr.Elem
		lenReg = fmt.Sprintf("%d", arr.Len)
		tempAlloca := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", tempAlloca, arr.LLVMType()))
		b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", arr.LLVMType(), xReg, arr.LLVMType(), tempAlloca))
		dataPtrReg = g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", dataPtrReg, arr.LLVMType(), tempAlloca))
	} else {
		lenReg = g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", lenReg, xReg))
		dataPtrReg = xReg
		elemType = sema.TypeByte
	}

	typedDataPtrReg := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedDataPtrReg, dataPtrReg, elemType.LLVMType()))

	idxAlloca := g.nextReg()
	g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i64\n", idxAlloca))
	b.WriteString(fmt.Sprintf("  store i64 0, i64* %s\n", idxAlloca))

	var keyIdent *ast.Identifier = nil
	if s.Key != nil {
		if kId, ok := s.Key.(*ast.Identifier); ok {
			keyIdent = kId
		}
	}

	var valIdent *ast.Identifier = nil
	if s.Value != nil {
		if vId, ok := s.Value.(*ast.Identifier); ok {
			valIdent = vId
		}
	}

	if keyIdent != nil {
		if _, exists := g.symbols[keyIdent.Value]; !exists {
			g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca i64\n", keyIdent.Value))
			g.symbols[keyIdent.Value] = Symbol{Name: keyIdent.Value, LLVMName: "%" + keyIdent.Value, Type: sema.TypeInt}
		}
	}
	if valIdent != nil {
		if _, exists := g.symbols[valIdent.Value]; !exists {
			g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", valIdent.Value, elemType.LLVMType()))
			g.symbols[valIdent.Value] = Symbol{Name: valIdent.Value, LLVMName: "%" + valIdent.Value, Type: elemType}
		}
	}

	condLabel := g.nextLabel("forrange.cond")
	bodyLabel := g.nextLabel("forrange.body")
	postLabel := g.nextLabel("forrange.post")
	endLabel := g.nextLabel("forrange.end")

	g.loopStack = append(g.loopStack, loopContext{breakLabel: endLabel, continueLabel: postLabel})
	defer func() {
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
	}()

	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", condLabel))

	b.WriteString(fmt.Sprintf("%s:\n", condLabel))
	curIdx := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", curIdx, idxAlloca))
	cmpReg := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, %s\n", cmpReg, curIdx, lenReg))
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", cmpReg, bodyLabel, endLabel))

	b.WriteString(fmt.Sprintf("%s:\n", bodyLabel))
	if keyIdent != nil {
		b.WriteString(fmt.Sprintf("  store i64 %s, i64* %%%s\n", curIdx, keyIdent.Value))
	}
	if valIdent != nil {
		elemPtr := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", elemPtr, elemType.LLVMType(), elemType.LLVMType(), typedDataPtrReg, curIdx))
		elemVal := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", elemVal, elemType.LLVMType(), elemType.LLVMType(), elemPtr))
		b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", elemType.LLVMType(), elemVal, elemType.LLVMType(), valIdent.Value))
	}

	g.emitStatement(b, s.Body, currentFn)
	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", postLabel))

	b.WriteString(fmt.Sprintf("%s:\n", postLabel))
	nextIdx := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", nextIdx, curIdx))
	b.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", nextIdx, idxAlloca))
	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", condLabel))

	b.WriteString(fmt.Sprintf("%s:\n", endLabel))
}

func (g *CodeGenerator) emitSwitchStmt(b *strings.Builder, s *ast.SwitchStmt, currentFn string) {
	if s.Init != nil {
		g.emitStatement(b, s.Init, currentFn)
	}

	switchValReg, switchValType := g.resolveValue(b, s.Value)
	endLabel := g.nextLabel("switch.end")

	g.loopStack = append(g.loopStack, loopContext{breakLabel: endLabel, continueLabel: endLabel})
	defer func() {
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
	}()

	var defaultCase *ast.CaseClause = nil

	for i, c := range s.Cases {
		if len(c.Values) == 0 {
			defaultCase = c
			continue
		}

		caseBodyLabel := g.nextLabel(fmt.Sprintf("switch.case%d.body", i))
		nextCaseLabel := g.nextLabel(fmt.Sprintf("switch.case%d.next", i))

		var matchedCondReg string = ""
		for _, valExpr := range c.Values {
			vReg, vType := g.resolveValue(b, valExpr)
			cmpReg := g.nextReg()
			if vType == sema.TypeString || (vType != nil && vType.TypeName() == "string") {
				b.WriteString(fmt.Sprintf("  %s = call i1 @hike_streq(i8* %s, i8* %s)\n", cmpReg, switchValReg, vReg))
			} else {
				b.WriteString(fmt.Sprintf("  %s = icmp eq %s %s, %s\n", cmpReg, switchValType.LLVMType(), switchValReg, vReg))
			}

			if matchedCondReg == "" {
				matchedCondReg = cmpReg
			} else {
				orReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", orReg, matchedCondReg, cmpReg))
				matchedCondReg = orReg
			}
		}

		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", matchedCondReg, caseBodyLabel, nextCaseLabel))

		b.WriteString(fmt.Sprintf("%s:\n", caseBodyLabel))
		for _, stmt := range c.Body {
			g.emitStatement(b, stmt, currentFn)
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))

		b.WriteString(fmt.Sprintf("%s:\n", nextCaseLabel))
	}

	if defaultCase != nil {
		for _, stmt := range defaultCase.Body {
			g.emitStatement(b, stmt, currentFn)
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))
	} else {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))
	}

	b.WriteString(fmt.Sprintf("%s:\n", endLabel))
}

func (g *CodeGenerator) emitTypeSwitchStmt(b *strings.Builder, s *ast.TypeSwitchStmt, currentFn string) {
	if s.Init != nil {
		g.emitStatement(b, s.Init, currentFn)
	}

	exprReg, exprType := g.resolveValue(b, s.Expr)
	endLabel := g.nextLabel("typeswitch.end")

	g.loopStack = append(g.loopStack, loopContext{breakLabel: endLabel, continueLabel: endLabel})
	defer func() {
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
	}()

	dataPtrReg := g.nextReg()
	actualTypeIDReg := g.nextReg()

	if it, ok := exprType.(*sema.InterfaceType); ok && !it.IsAny() {
		itabRawReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 0\n", dataPtrReg, exprReg))
		b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 1\n", itabRawReg, exprReg))
		typeIDPtr := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i64*\n", typeIDPtr, itabRawReg))
		b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", actualTypeIDReg, typeIDPtr))
	} else {
		b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i64 } %s, 0\n", dataPtrReg, exprReg))
		b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i64 } %s, 1\n", actualTypeIDReg, exprReg))
	}

	var defaultCase *ast.TypeCaseClause = nil

	for i, c := range s.Cases {
		if len(c.Types) == 0 {
			defaultCase = c
			continue
		}

		caseBodyLabel := g.nextLabel(fmt.Sprintf("typeswitch.case%d.body", i))
		nextCaseLabel := g.nextLabel(fmt.Sprintf("typeswitch.case%d.next", i))

		var matchedCondReg string = ""
		for _, tExpr := range c.Types {
			targetType := g.semaCtx.ResolveType(tExpr)
			targetTypeID := g.semaCtx.GetTypeID(targetType)

			cmpReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, %d\n", cmpReg, actualTypeIDReg, targetTypeID))

			if matchedCondReg == "" {
				matchedCondReg = cmpReg
			} else {
				orReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", orReg, matchedCondReg, cmpReg))
				matchedCondReg = orReg
			}
		}

		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", matchedCondReg, caseBodyLabel, nextCaseLabel))

		b.WriteString(fmt.Sprintf("%s:\n", caseBodyLabel))

		var oldSym Symbol
		var hadOld bool = false
		if s.Variable != nil {
			oldSym, hadOld = g.symbols[s.Variable.Value]
			if len(c.Types) == 1 {
				targetType := g.semaCtx.ResolveType(c.Types[0])
				var valToStore string
				if strings.HasSuffix(targetType.LLVMType(), "*") {
					castReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", castReg, dataPtrReg, targetType.LLVMType()))
					valToStore = castReg
				} else if targetType.LLVMType() == "i64" {
					castReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", castReg, dataPtrReg))
					valToStore = castReg
				} else if targetType.LLVMType() == "i1" {
					pInt := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", pInt, dataPtrReg))
					castReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i1\n", castReg, pInt))
					valToStore = castReg
				} else {
					castPtr := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", castPtr, dataPtrReg, targetType.LLVMType()))
					loadReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, targetType.LLVMType(), targetType.LLVMType(), castPtr))
					valToStore = loadReg
				}

				vAlloca := g.nextReg()
				g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", vAlloca, targetType.LLVMType()))
				b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", targetType.LLVMType(), valToStore, targetType.LLVMType(), vAlloca))
				g.symbols[s.Variable.Value] = Symbol{Name: s.Variable.Value, LLVMName: vAlloca, Type: targetType}
			} else {
				vAlloca := g.nextReg()
				g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", vAlloca, exprType.LLVMType()))
				b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", exprType.LLVMType(), exprReg, exprType.LLVMType(), vAlloca))
				g.symbols[s.Variable.Value] = Symbol{Name: s.Variable.Value, LLVMName: vAlloca, Type: exprType}
			}
		}

		for _, stmt := range c.Body {
			g.emitStatement(b, stmt, currentFn)
		}

		if s.Variable != nil {
			if hadOld {
				g.symbols[s.Variable.Value] = oldSym
			} else {
				delete(g.symbols, s.Variable.Value)
			}
		}

		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))

		b.WriteString(fmt.Sprintf("%s:\n", nextCaseLabel))
	}

	if defaultCase != nil {
		var oldSym Symbol
		var hadOld bool = false
		if s.Variable != nil {
			oldSym, hadOld = g.symbols[s.Variable.Value]
			vAlloca := g.nextReg()
			g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", vAlloca, exprType.LLVMType()))
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", exprType.LLVMType(), exprReg, exprType.LLVMType(), vAlloca))
			g.symbols[s.Variable.Value] = Symbol{Name: s.Variable.Value, LLVMName: vAlloca, Type: exprType}
		}

		for _, stmt := range defaultCase.Body {
			g.emitStatement(b, stmt, currentFn)
		}

		if s.Variable != nil {
			if hadOld {
				g.symbols[s.Variable.Value] = oldSym
			} else {
				delete(g.symbols, s.Variable.Value)
			}
		}

		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))
	} else {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", endLabel))
	}

	b.WriteString(fmt.Sprintf("%s:\n", endLabel))
}

func (g *CodeGenerator) emitReturnStmt(b *strings.Builder, s *ast.ReturnStmt, currentFn string) {
	if currentFn == "main" {
		var retReg string = "0"
		if len(s.Values) == 1 {
			valReg, valType := g.resolveValue(b, s.Values[0])
			truncReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = trunc %s %s to i32\n", truncReg, valType.LLVMType(), valReg))
			retReg = truncReg
		}

		for i := len(g.deferStack) - 1; i >= 0; i-- {
			g.emitCallExpr(b, g.deferStack[i])
		}

		b.WriteString(fmt.Sprintf("  ret i32 %s\n", retReg))
		return
	}

	fnType := g.lookupFunction(currentFn)

	if len(s.Values) == 0 {
		for i := len(g.deferStack) - 1; i >= 0; i-- {
			g.emitCallExpr(b, g.deferStack[i])
		}
		b.WriteString("  ret void\n")
		return
	}

	if len(s.Values) == 1 {
		valReg, valType := g.resolveValue(b, s.Values[0])

		targetTypeStr := valType.LLVMType()
		var targetType sema.Type = valType
		if fnType != nil && len(fnType.ReturnTypes) == 1 {
			targetType = fnType.ReturnTypes[0]
			targetTypeStr = targetType.LLVMType()
		}

		if _, isNil := s.Values[0].(*ast.NilLiteral); isNil {
			for i := len(g.deferStack) - 1; i >= 0; i-- {
				g.emitCallExpr(b, g.deferStack[i])
			}
			b.WriteString(fmt.Sprintf("  ret %s null\n", targetTypeStr))
			return
		}

		if strings.HasSuffix(targetTypeStr, "*") {
			if valReg == "0" || valReg == "null" || valReg == "" {
				valReg = "null"
			}
		}

		finalReg := g.emitArgConversion(b, valReg, valType, targetType)

		for i := len(g.deferStack) - 1; i >= 0; i-- {
			g.emitCallExpr(b, g.deferStack[i])
		}

		b.WriteString(fmt.Sprintf("  ret %s %s\n", targetTypeStr, finalReg))
		return
	}

	if len(s.Values) > 1 {
		types := []string{}
		valRegs := []string{}
		for i, v := range s.Values {
			vReg, vType := g.resolveValue(b, v)
			var targetType sema.Type = vType
			if fnType != nil && i < len(fnType.ReturnTypes) {
				targetType = fnType.ReturnTypes[i]
			}
			finalReg := g.emitArgConversion(b, vReg, vType, targetType)
			types = append(types, targetType.LLVMType())
			valRegs = append(valRegs, finalReg)
		}
		aggType := fmt.Sprintf("{ %s }", strings.Join(types, ", "))
		curAgg := "undef"
		for i, vReg := range valRegs {
			nextAgg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, %d\n", nextAgg, aggType, curAgg, types[i], vReg, i))
			curAgg = nextAgg
		}

		for i := len(g.deferStack) - 1; i >= 0; i-- {
			g.emitCallExpr(b, g.deferStack[i])
		}

		b.WriteString(fmt.Sprintf("  ret %s %s\n", aggType, curAgg))
		return
	}
}

func (g *CodeGenerator) emitCallExpr(b *strings.Builder, call *ast.CallExpr) (string, sema.Type) {
	return g.emitCallInternal(b, call)
}

func (g *CodeGenerator) resolveValue(b *strings.Builder, expr ast.Expression) (string, sema.Type) {
	if expr == nil {
		return "0", sema.TypeInt
	}

	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value), sema.TypeInt

	case *ast.StringLiteral:
		label, length := g.getStringLiteral(e.Value)
		gepReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0\n",
			gepReg, length, length, label))
		return gepReg, sema.TypeString

	case *ast.NilLiteral:
		return "null", &sema.PointerType{Base: sema.TypeByte}

	case *ast.FuncLit:
		g.anonFuncCount++
		fnName := fmt.Sprintf("__anon_func_%d", g.anonFuncCount)
		captures := g.scanCaptures(e)

		fnDecl := &ast.FuncDecl{
			Token:       e.Token,
			Name:        &ast.Identifier{Token: e.Token, Value: fnName},
			Params:      e.Params,
			ReturnTypes: e.ReturnTypes,
			Body:        e.Body,
		}

		fnType := &sema.FuncType{
			Name:        fnName,
			ParamTypes:  []sema.Type{},
			ReturnTypes: []sema.Type{},
		}
		for _, p := range e.Params {
			fnType.ParamTypes = append(fnType.ParamTypes, g.semaCtx.ResolveType(p.Type))
		}
		for _, rt := range e.ReturnTypes {
			fnType.ReturnTypes = append(fnType.ReturnTypes, g.semaCtx.ResolveType(rt))
		}

		var envStructName string = ""
		outerTypes := make(map[string]sema.Type)

		retTypeStr := "void"
		if len(fnType.ReturnTypes) == 1 {
			retTypeStr = fnType.ReturnTypes[0].LLVMType()
		} else if len(fnType.ReturnTypes) > 1 {
			types := []string{}
			for _, rt := range fnType.ReturnTypes {
				types = append(types, rt.LLVMType())
			}
			retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
		}

		sigParams := []string{"i8*"}
		for _, pt := range fnType.ParamTypes {
			sigParams = append(sigParams, pt.LLVMType())
		}
		rawFnSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(sigParams, ", "))

		var envMemReg string = "null"

		if len(captures) > 0 {
			envStructName = fmt.Sprintf("__env_%d", g.anonFuncCount)
			fieldTypes := []string{}
			for _, capName := range captures {
				sym := g.symbols[capName]
				outerTypes[capName] = sym.Type
				fieldTypes = append(fieldTypes, sym.Type.LLVMType()+"*")
			}
			g.envStructDefs = append(g.envStructDefs, fmt.Sprintf("%%struct.%s = type { %s }\n", envStructName, strings.Join(fieldTypes, ", ")))

			mallocReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", mallocReg, len(captures)*8))
			envTyped := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%struct.%s*\n", envTyped, mallocReg, envStructName))

			for idx, capName := range captures {
				sym := g.symbols[capName]
				gepReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
					gepReg, envStructName, envStructName, envTyped, idx))
				b.WriteString(fmt.Sprintf("  store %s* %s, %s** %s\n", sym.Type.LLVMType(), sym.LLVMName, sym.Type.LLVMType(), gepReg))
			}
			envMemReg = mallocReg
		}

		g.anonFuncs = append(g.anonFuncs, anonFuncMeta{
			Name:       fnName,
			Decl:       fnDecl,
			EnvStruct:  envStructName,
			Captures:   captures,
			OuterTypes: outerTypes,
			FuncType:   fnType,
		})

		fnPtrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast %s @%s to i8*\n", fnPtrReg, rawFnSig, fnName))
		c0 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } undef, i8* %s, 0\n", c0, fnPtrReg))
		c1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } %s, i8* %s, 1\n", c1, c0, envMemReg))
		return c1, fnType

	case *ast.Identifier:
		if e.Value == "true" {
			return "true", sema.TypeBool
		}
		if e.Value == "false" {
			return "false", sema.TypeBool
		}

		if val, ok := g.semaCtx.Constants[e.Value]; ok {
			return fmt.Sprintf("%d", val), sema.TypeInt
		}

		if sym, exists := g.symbols[e.Value]; exists {
			loadReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, sym.Type.LLVMType(), sym.Type.LLVMType(), sym.LLVMName))
			return loadReg, sym.Type
		}

		if gType, exists := g.semaCtx.Globals[e.Value]; exists {
			loadReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = load %s, %s* @%s\n", loadReg, gType.LLVMType(), gType.LLVMType(), e.Value))
			return loadReg, gType
		}

		if fn, exists := g.semaCtx.Functions[e.Value]; exists {
			thunkName := g.getOrCreateThunk(e.Value)
			retTypeStr := "void"
			if len(fn.ReturnTypes) == 1 {
				retTypeStr = fn.ReturnTypes[0].LLVMType()
			} else if len(fn.ReturnTypes) > 1 {
				types := []string{}
				for _, rt := range fn.ReturnTypes {
					types = append(types, rt.LLVMType())
				}
				retTypeStr = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
			}
			sigParams := []string{"i8*"}
			for _, pt := range fn.ParamTypes {
				sigParams = append(sigParams, pt.LLVMType())
			}
			rawFnSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(sigParams, ", "))

			thunkPtrReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast %s @%s to i8*\n", thunkPtrReg, rawFnSig, thunkName))
			c0 := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } undef, i8* %s, 0\n", c0, thunkPtrReg))
			c1 := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i8* } %s, i8* %s, 1\n", c1, c0, "null"))
			return c1, fn
		}

		return "0", sema.TypeInt

	case *ast.BinaryExpr:
		return g.emitBinaryExpr(b, e)

	case *ast.StructLiteral:
		st, structName := g.findStructByName(e.Type.Name.Value)
		if st == nil {
			panic(fmt.Sprintf("[Codegen Error] unknown struct type %s", e.Type.Name.Value))
		}

		allocaReg := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %%struct.%s\n", allocaReg, structName))

		for i, fVal := range e.Fields {
			valReg, valType := g.resolveValue(b, fVal.Value)

			fieldIdx := i
			var fieldType sema.Type
			if fVal.Name != nil {
				for idx, sf := range st.Fields {
					if sf.Name == fVal.Name.Value {
						fieldIdx = idx
						fieldType = sf.Type
						break
					}
				}
			} else if i < len(st.Fields) {
				fieldType = st.Fields[i].Type
			}

			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
				gepReg, structName, structName, allocaReg, fieldIdx))

			valToStore := g.emitArgConversion(b, valReg, valType, fieldType)
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldType.LLVMType(), valToStore, fieldType.LLVMType(), gepReg))
		}

		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %%struct.%s, %%struct.%s* %s\n", loadReg, structName, structName, allocaReg))
		return loadReg, st

	case *ast.PrefixExpr:
		if e.Operator == "^" {
			valReg, _ := g.resolveValue(b, e.Right)
			xorReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = xor i64 -1, %s\n", xorReg, valReg))
			return xorReg, sema.TypeInt
		}
		if e.Operator == "&" {
			if ident, ok := e.Right.(*ast.Identifier); ok {
				if sym, exists := g.symbols[ident.Value]; exists {
					return sym.LLVMName, &sema.PointerType{Base: sym.Type}
				}
				if gType, exists := g.semaCtx.Globals[ident.Value]; exists {
					return "@" + ident.Value, &sema.PointerType{Base: gType}
				}
			} else if stLit, ok := e.Right.(*ast.StructLiteral); ok {
				targetStructName := stLit.Type.Name.Value
				if stLit.Type.Package != nil {
					targetStructName = stLit.Type.Package.Value + "_" + stLit.Type.Name.Value
				}

				st, structName := g.findStructByName(targetStructName)
				if st == nil {
					panic(fmt.Sprintf("[Codegen Error] unknown struct type %s", targetStructName))
				}

				// ヒープメモリを確保してダングリングポインタを防止
				structSize := st.Size()
				if structSize <= 0 {
					structSize = 8
				}
				mallocReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", mallocReg, structSize))

				heapPtrReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%struct.%s*\n", heapPtrReg, mallocReg, structName))

				for i, fVal := range stLit.Fields {
					valReg, valType := g.resolveValue(b, fVal.Value)

					fieldIdx := i
					var fieldType sema.Type
					if fVal.Name != nil {
						for idx, sf := range st.Fields {
							if sf.Name == fVal.Name.Value {
								fieldIdx = idx
								fieldType = sf.Type
								break
							}
						}
					} else if i < len(st.Fields) {
						fieldType = st.Fields[i].Type
					}

					gepReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
						gepReg, structName, structName, heapPtrReg, fieldIdx))

					valToStore := g.emitArgConversion(b, valReg, valType, fieldType)
					b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldType.LLVMType(), valToStore, fieldType.LLVMType(), gepReg))
				}
				return heapPtrReg, &sema.PointerType{Base: st}
			}
		} else if e.Operator == "*" {
			valReg, valType := g.resolveValue(b, e.Right)
			if ptrType, ok := valType.(*sema.PointerType); ok {
				loadReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, ptrType.Base.LLVMType(), ptrType.Base.LLVMType(), valReg))
				return loadReg, ptrType.Base
			}
		} else if e.Operator == "!" {
			valReg, valType := g.resolveValue(b, e.Right)
			bReg := valReg
			if valType != nil && valType.LLVMType() != "i1" {
				tStr := valType.LLVMType()
				cmpVal := "0"
				if strings.HasSuffix(tStr, "*") {
					cmpVal = "null"
				}
				bReg = g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, %s\n", bReg, tStr, valReg, cmpVal))
			}
			notReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", notReg, bReg))
			return notReg, sema.TypeBool
		} else if e.Operator == "-" {
			valReg, _ := g.resolveValue(b, e.Right)
			negReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = sub i64 0, %s\n", negReg, valReg))
			return negReg, sema.TypeInt
		}

	case *ast.MemberExpr:
		// パッケージ定数・グローバル変数の参照解決（例: calc.DefaultBase）
		if objIdent, isIdent := e.Object.(*ast.Identifier); isIdent {
			if _, exists := g.symbols[objIdent.Value]; !exists {
				pkgSymbol := objIdent.Value + "_" + e.Field.Value
				if val, ok := g.semaCtx.Constants[pkgSymbol]; ok {
					return fmt.Sprintf("%d", val), sema.TypeInt
				}
				if gType, exists := g.semaCtx.Globals[pkgSymbol]; exists {
					loadReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = load %s, %s* @%s\n", loadReg, gType.LLVMType(), gType.LLVMType(), pkgSymbol))
					return loadReg, gType
				}
			}
		}

		objPtrReg, _, st, structName := g.resolveStructPtr(b, e.Object)
		if st == nil {
			panic(fmt.Sprintf("[Codegen Error] struct type not found for member expr %s", e.Field.Value))
		}

		gepReg, fieldType, _, found := g.resolveFieldPath(st, structName, objPtrReg, e.Field.Value, b)
		if !found {
			panic(fmt.Sprintf("[Codegen Error] unknown field %s in struct %s", e.Field.Value, structName))
		}

		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, fieldType.LLVMType(), fieldType.LLVMType(), gepReg))
		return loadReg, fieldType

	case *ast.IndexExpr:
		baseReg, baseType := g.resolveValue(b, e.Left)
		idxReg, _ := g.resolveValue(b, e.Index)

		if arr, isArr := baseType.(*sema.ArrayType); isArr {
			arrPtrReg := baseReg
			if objIdent, isIdent := e.Left.(*ast.Identifier); isIdent {
				if sym, ok := g.symbols[objIdent.Value]; ok {
					arrPtrReg = sym.LLVMName
				}
			}
			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
				gepReg, arr.LLVMType(), arr.LLVMType(), arrPtrReg, idxReg))
			loadReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, arr.Elem.LLVMType(), arr.Elem.LLVMType(), gepReg))
			return loadReg, arr.Elem
		}

		var dataPtrReg string
		var elemType sema.Type = sema.TypeByte

		if sl, isSlice := baseType.(*sema.SliceType); isSlice {
			elemType = sl.Elem
			dataPtrReg = g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtrReg, sl.LLVMType(), baseReg))
		} else {
			dataPtrReg = baseReg
		}

		typedPtrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedPtrReg, dataPtrReg, elemType.LLVMType()))

		gepReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", gepReg, elemType.LLVMType(), elemType.LLVMType(), typedPtrReg, idxReg))

		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, elemType.LLVMType(), elemType.LLVMType(), gepReg))
		return loadReg, elemType

	case *ast.ArrayLiteral:
		arrType := g.semaCtx.ResolveType(e.Type)
		arr, ok := arrType.(*sema.ArrayType)
		if !ok {
			panic("[Codegen Error] invalid array type in literal")
		}

		allocaReg := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", allocaReg, arr.LLVMType()))

		for i, el := range e.Elements {
			elReg, elType := g.resolveValue(b, el)
			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %d\n",
				gepReg, arr.LLVMType(), arr.LLVMType(), allocaReg, i))

			valToStore := g.emitArgConversion(b, elReg, elType, arr.Elem)
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", arr.Elem.LLVMType(), valToStore, arr.Elem.LLVMType(), gepReg))
		}

		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, arr.LLVMType(), arr.LLVMType(), allocaReg))
		return loadReg, arr

	case *ast.SliceLiteral:
		sliceType := g.semaCtx.ResolveType(e.Type)
		sl, ok := sliceType.(*sema.SliceType)
		if !ok {
			panic("[Codegen Error] invalid slice type in literal")
		}

		count := len(e.Elements)
		elemSize := sl.Elem.Size()
		if elemSize <= 0 {
			elemSize = 1
		}
		totalBytes := count * elemSize

		mallocReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", mallocReg, totalBytes))

		typedPtrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedPtrReg, mallocReg, sl.Elem.LLVMType()))

		for i, el := range e.Elements {
			elReg, elType := g.resolveValue(b, el)
			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %d\n",
				gepReg, sl.Elem.LLVMType(), sl.Elem.LLVMType(), typedPtrReg, i))

			valToStore := g.emitArgConversion(b, elReg, elType, sl.Elem)
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", sl.Elem.LLVMType(), valToStore, sl.Elem.LLVMType(), gepReg))
		}

		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, sl.LLVMType(), mallocReg))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %d, 1\n", t2, sl.LLVMType(), t1, count))
		t3 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %d, 2\n", t3, sl.LLVMType(), t2, count))
		return t3, sl

	case *ast.SliceExpr:
		baseReg, baseType := g.resolveValue(b, e.Left)

		var dataPtrReg string
		var capReg string
		var elemType sema.Type = sema.TypeByte

		if sl, isSlice := baseType.(*sema.SliceType); isSlice {
			elemType = sl.Elem
			dataPtrReg = g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtrReg, sl.LLVMType(), baseReg))
			capReg = g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 2\n", capReg, sl.LLVMType(), baseReg))
		} else if arr, isArr := baseType.(*sema.ArrayType); isArr {
			elemType = arr.Elem
			capReg = fmt.Sprintf("%d", arr.Len)
			tempAlloca := g.nextReg()
			g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", tempAlloca, arr.LLVMType()))
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", arr.LLVMType(), baseReg, arr.LLVMType(), tempAlloca))
			dataPtrReg = g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast %s* %s to i8*\n", dataPtrReg, arr.LLVMType(), tempAlloca))
		} else {
			dataPtrReg = baseReg
			capReg = g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", capReg, baseReg))
		}

		lowReg := "0"
		if e.Low != nil {
			lowReg, _ = g.resolveValue(b, e.Low)
		}

		highReg := capReg
		if e.High != nil {
			highReg, _ = g.resolveValue(b, e.High)
		}

		if baseType == sema.TypeString || (baseType != nil && baseType.TypeName() == "string") {
			subReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = call i8* @hike_substr(i8* %s, i64 %s, i64 %s)\n", subReg, baseReg, lowReg, highReg))
			return subReg, sema.TypeString
		}

		elemSize := elemType.Size()
		if elemSize <= 0 {
			elemSize = 1
		}

		offsetBytesReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", offsetBytesReg, lowReg, elemSize))

		subPtrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", subPtrReg, dataPtrReg, offsetBytesReg))

		newLenReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", newLenReg, highReg, lowReg))

		newCapReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", newCapReg, capReg, lowReg))

		resSliceType := &sema.SliceType{Elem: elemType}
		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, resSliceType.LLVMType(), subPtrReg))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 1\n", t2, resSliceType.LLVMType(), t1, newLenReg))
		t3 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 2\n", t3, resSliceType.LLVMType(), t2, newCapReg))

		return t3, resSliceType

	case *ast.TypeAssertExpr:
		ifaceReg, ifaceType := g.resolveValue(b, e.Expr)
		targetType := g.semaCtx.ResolveType(e.Target)
		targetTypeID := g.semaCtx.GetTypeID(targetType)

		dataPtrReg := g.nextReg()
		typeIDReg := g.nextReg()

		if it, ok := ifaceType.(*sema.InterfaceType); ok && !it.IsAny() {
			itabRawReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 0\n", dataPtrReg, ifaceReg))
			b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 1\n", itabRawReg, ifaceReg))
			typeIDPtr := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to i64*\n", typeIDPtr, itabRawReg))
			b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", typeIDReg, typeIDPtr))
		} else {
			b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i64 } %s, 0\n", dataPtrReg, ifaceReg))
			b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i64 } %s, 1\n", typeIDReg, ifaceReg))
		}

		matchReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, %d\n", matchReg, typeIDReg, targetTypeID))

		unpackedValReg := g.nextReg()
		if strings.HasSuffix(targetType.LLVMType(), "*") {
			b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", unpackedValReg, dataPtrReg, targetType.LLVMType()))
		} else if targetType.LLVMType() == "i64" {
			b.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", unpackedValReg, dataPtrReg))
		} else if targetType.LLVMType() == "i1" {
			pInt := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", pInt, dataPtrReg))
			b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i1\n", unpackedValReg, pInt))
		} else {
			castPtr := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", castPtr, dataPtrReg, targetType.LLVMType()))
			b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", unpackedValReg, targetType.LLVMType(), targetType.LLVMType(), castPtr))
		}

		tupleType := fmt.Sprintf("{ %s, i1 }", targetType.LLVMType())
		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, %s %s, 0\n", t1, tupleType, targetType.LLVMType(), unpackedValReg))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i1 %s, 1\n", t2, tupleType, t1, matchReg))

		return t2, &sema.TupleType{Types: []sema.Type{targetType, sema.TypeBool}}

	case *ast.CallExpr:
		return g.emitCallInternal(b, e)
	}

	return "0", sema.TypeInt
}

func (g *CodeGenerator) emitBinaryExpr(b *strings.Builder, e *ast.BinaryExpr) (string, sema.Type) {
	leftReg, leftType := g.resolveValue(b, e.Left)
	rightReg, rightType := g.resolveValue(b, e.Right)

	if e.Operator == "==" || e.Operator == "!=" {
		// Interface vs nil の比較 (例: err != nil, err == nil)
		if iface, isIface := leftType.(*sema.InterfaceType); isIface && (rightReg == "null" || rightReg == "zeroinitializer" || rightType == nil || strings.HasSuffix(rightType.LLVMType(), "*")) {
			dataPtr := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtr, iface.LLVMType(), leftReg))
			cmpOp := "icmp eq"
			if e.Operator == "!=" {
				cmpOp = "icmp ne"
			}
			cmpReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = %s i8* %s, null\n", cmpReg, cmpOp, dataPtr))
			return cmpReg, sema.TypeBool
		}
		if iface, isIface := rightType.(*sema.InterfaceType); isIface && (leftReg == "null" || leftReg == "zeroinitializer" || leftType == nil || strings.HasSuffix(leftType.LLVMType(), "*")) {
			dataPtr := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtr, iface.LLVMType(), rightReg))
			cmpOp := "icmp eq"
			if e.Operator == "!=" {
				cmpOp = "icmp ne"
			}
			cmpReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = %s i8* %s, null\n", cmpReg, cmpOp, dataPtr))
			return cmpReg, sema.TypeBool
		}
	}

	if leftType == sema.TypeString || rightType == sema.TypeString {
		if e.Operator == "+" {
			concatReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = call i8* @hike_strcat(i8* %s, i8* %s)\n", concatReg, leftReg, rightReg))
			return concatReg, sema.TypeString
		}
		if e.Operator == "==" || e.Operator == "!=" {
			eqReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = call i1 @hike_streq(i8* %s, i8* %s)\n", eqReg, leftReg, rightReg))
			if e.Operator == "!=" {
				neqReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", neqReg, eqReg))
				return neqReg, sema.TypeBool
			}
			return eqReg, sema.TypeBool
		}
	}

	opInst := "add"
	isCompare := false

	switch e.Operator {
	case "+":
		opInst = "add"
	case "-":
		opInst = "sub"
	case "*":
		opInst = "mul"
	case "/":
		opInst = "sdiv"
	case "&":
		opInst = "and"
	case "|=":
		opInst = "or"
	case "^=":
		opInst = "xor"
	case "<<=":
		opInst = "shl"
	case ">>=":
		opInst = "ashr"
	case "==":
		opInst = "icmp eq"
		isCompare = true
	case "!=":
		opInst = "icmp ne"
		isCompare = true
	case "<":
		opInst = "icmp slt"
		isCompare = true
	case ">":
		opInst = "icmp sgt"
		isCompare = true
	case "<=":
		opInst = "icmp sle"
		isCompare = true
	case ">=":
		opInst = "icmp sge"
		isCompare = true
	case "&&":
		opInst = "and"
	case "||":
		opInst = "or"
	}

	resReg := g.nextReg()
	opTypeStr := "i64"
	if leftType != nil && leftType.LLVMType() != "" {
		opTypeStr = leftType.LLVMType()
	}

	b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", resReg, opInst, opTypeStr, leftReg, rightReg))

	if isCompare || e.Operator == "&&" || e.Operator == "||" {
		return resReg, sema.TypeBool
	}
	return resReg, leftType
}

func (g *CodeGenerator) emitCallInternal(b *strings.Builder, call *ast.CallExpr) (string, sema.Type) {
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "append" && call.HasEllipsis && len(call.Args) == 2 {
		slice1Reg, slice1Type := g.resolveValue(b, call.Args[0])
		slice2Reg, slice2Type := g.resolveValue(b, call.Args[1])

		sl1, isSlice1 := slice1Type.(*sema.SliceType)
		sl2, _ := slice2Type.(*sema.SliceType)
		if !isSlice1 || sl2 == nil {
			panic("[Codegen Error] both arguments to append with ellipsis must be slices")
		}

		elemSize := sl1.Elem.Size()
		if elemSize <= 0 {
			elemSize = 1
		}

		s1Ptr := g.nextReg()
		s1Len := g.nextReg()
		s1Cap := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", s1Ptr, sl1.LLVMType(), slice1Reg))
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", s1Len, sl1.LLVMType(), slice1Reg))
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 2\n", s1Cap, sl1.LLVMType(), slice1Reg))

		s2Ptr := g.nextReg()
		s2Len := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", s2Ptr, sl2.LLVMType(), slice2Reg))
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", s2Len, sl2.LLVMType(), slice2Reg))

		totalLen := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = add i64 %s, %s\n", totalLen, s1Len, s2Len))

		condReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, %s\n", condReg, s1Cap, totalLen))

		lblGrow := g.nextLabel("appendslice.grow")
		lblNoGrow := g.nextLabel("appendslice.nogrow")
		lblCopy := g.nextLabel("appendslice.copy")

		resPtrAlloca := g.nextReg()
		resCapAlloca := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i8*\n", resPtrAlloca))
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i64\n", resCapAlloca))

		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", condReg, lblNoGrow, lblGrow))

		b.WriteString(fmt.Sprintf("%s:\n", lblNoGrow))
		b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", s1Ptr, resPtrAlloca))
		b.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", s1Cap, resCapAlloca))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCopy))

		b.WriteString(fmt.Sprintf("%s:\n", lblGrow))
		doubleCap := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, 2\n", doubleCap, s1Cap))
		cmpCap := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", cmpCap, doubleCap, totalLen))
		growCap := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", growCap, cmpCap, doubleCap, totalLen))
		isSmall := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, 4\n", isSmall, growCap))
		newCap := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 4, i64 %s\n", newCap, isSmall, growCap))

		newBytes := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", newBytes, newCap, elemSize))
		newPtr := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", newPtr, newBytes))

		lblS1Copy := g.nextLabel("appendslice.s1copy")
		lblAfterS1Copy := g.nextLabel("appendslice.after_s1copy")
		hasS1Len := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, 0\n", hasS1Len, s1Len))
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", hasS1Len, lblS1Copy, lblAfterS1Copy))

		b.WriteString(fmt.Sprintf("%s:\n", lblS1Copy))
		s1Bytes := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", s1Bytes, s1Len, elemSize))
		b.WriteString(fmt.Sprintf("  call i8* @memcpy(i8* %s, i8* %s, i64 %s)\n", newPtr, s1Ptr, s1Bytes))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblAfterS1Copy))

		b.WriteString(fmt.Sprintf("%s:\n", lblAfterS1Copy))
		b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", newPtr, resPtrAlloca))
		b.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", newCap, resCapAlloca))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCopy))

		b.WriteString(fmt.Sprintf("%s:\n", lblCopy))
		finalPtr := g.nextReg()
		finalCap := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load i8*, i8** %s\n", finalPtr, resPtrAlloca))
		b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", finalCap, resCapAlloca))

		lblS2Copy := g.nextLabel("appendslice.s2copy")
		lblDone := g.nextLabel("appendslice.done")
		hasS2Len := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, 0\n", hasS2Len, s2Len))
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", hasS2Len, lblS2Copy, lblDone))

		b.WriteString(fmt.Sprintf("%s:\n", lblS2Copy))
		s1OffsetBytes := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", s1OffsetBytes, s1Len, elemSize))
		destS2Ptr := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", destS2Ptr, finalPtr, s1OffsetBytes))
		s2Bytes := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", s2Bytes, s2Len, elemSize))
		b.WriteString(fmt.Sprintf("  call i8* @memcpy(i8* %s, i8* %s, i64 %s)\n", destS2Ptr, s2Ptr, s2Bytes))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblDone))

		b.WriteString(fmt.Sprintf("%s:\n", lblDone))
		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, sl1.LLVMType(), finalPtr))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 1\n", t2, sl1.LLVMType(), t1, totalLen))
		t3 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 2\n", t3, sl1.LLVMType(), t2, finalCap))

		return t3, sl1
	}

	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "append" && len(call.Args) >= 2 {
		sliceReg, sliceType := g.resolveValue(b, call.Args[0])

		sl, isSlice := sliceType.(*sema.SliceType)
		if !isSlice {
			panic("[Codegen Error] first argument to append must be a slice")
		}

		elemSize := sl.Elem.Size()
		if elemSize <= 0 {
			elemSize = 1
		}

		numElems := len(call.Args) - 1

		oldPtrReg := g.nextReg()
		oldLenReg := g.nextReg()
		oldCapReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", oldPtrReg, sl.LLVMType(), sliceReg))
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", oldLenReg, sl.LLVMType(), sliceReg))
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 2\n", oldCapReg, sl.LLVMType(), sliceReg))

		lblGrow := g.nextLabel("append.grow")
		lblNoGrow := g.nextLabel("append.nogrow")
		lblStore := g.nextLabel("append.store")

		resPtrAlloca := g.nextReg()
		resCapAlloca := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i8*\n", resPtrAlloca))
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i64\n", resCapAlloca))

		reqCapReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = add i64 %s, %d\n", reqCapReg, oldLenReg, numElems))

		condReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, %s\n", condReg, oldCapReg, reqCapReg))
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", condReg, lblNoGrow, lblGrow))

		b.WriteString(fmt.Sprintf("%s:\n", lblNoGrow))
		b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", oldPtrReg, resPtrAlloca))
		b.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", oldCapReg, resCapAlloca))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblStore))

		b.WriteString(fmt.Sprintf("%s:\n", lblGrow))
		doubleCapReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, 2\n", doubleCapReg, oldCapReg))

		cmpCapReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", cmpCapReg, doubleCapReg, reqCapReg))
		growCapReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 %s\n", growCapReg, cmpCapReg, doubleCapReg, reqCapReg))

		isSmallReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, 4\n", isSmallReg, growCapReg))
		newCapReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 4, i64 %s\n", newCapReg, isSmallReg, growCapReg))

		newBytesReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", newBytesReg, newCapReg, elemSize))
		newPtrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %s)\n", newPtrReg, newBytesReg))

		lblCopy := g.nextLabel("append.copy")
		lblAfterCopy := g.nextLabel("append.after_copy")
		hasOldLenReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, 0\n", hasOldLenReg, oldLenReg))
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", hasOldLenReg, lblCopy, lblAfterCopy))

		b.WriteString(fmt.Sprintf("%s:\n", lblCopy))
		copyBytesReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", copyBytesReg, oldLenReg, elemSize))
		b.WriteString(fmt.Sprintf("  call i8* @memcpy(i8* %s, i8* %s, i64 %s)\n", newPtrReg, oldPtrReg, copyBytesReg))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblAfterCopy))

		b.WriteString(fmt.Sprintf("%s:\n", lblAfterCopy))
		b.WriteString(fmt.Sprintf("  store i8* %s, i8** %s\n", newPtrReg, resPtrAlloca))
		b.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", newCapReg, resCapAlloca))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblStore))

		b.WriteString(fmt.Sprintf("%s:\n", lblStore))
		finalPtrReg := g.nextReg()
		finalCapReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load i8*, i8** %s\n", finalPtrReg, resPtrAlloca))
		b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", finalCapReg, resCapAlloca))

		typedFinalPtrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedFinalPtrReg, finalPtrReg, sl.Elem.LLVMType()))

		for i := 0; i < numElems; i++ {
			elemReg, elemType := g.resolveValue(b, call.Args[1+i])

			offsetReg := oldLenReg
			if i > 0 {
				offsetReg = g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = add i64 %s, %d\n", offsetReg, oldLenReg, i))
			}

			destElemPtrReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", destElemPtrReg, sl.Elem.LLVMType(), sl.Elem.LLVMType(), typedFinalPtrReg, offsetReg))

			valToStore := g.emitArgConversion(b, elemReg, elemType, sl.Elem)
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", sl.Elem.LLVMType(), valToStore, sl.Elem.LLVMType(), destElemPtrReg))
		}

		newLenReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = add i64 %s, %d\n", newLenReg, oldLenReg, numElems))

		retSliceType := "{ i8*, i64, i64 }"
		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, retSliceType, finalPtrReg))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 1\n", t2, retSliceType, t1, newLenReg))
		t3 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 2\n", t3, retSliceType, t2, finalCapReg))

		return t3, sl
	}

	if fnIdent, ok := call.Function.(*ast.Identifier); ok && len(call.Args) == 1 {
		if fnIdent.Value == "len" || fnIdent.Value == "cap" {
			argReg, argType := g.resolveValue(b, call.Args[0])
			if sl, isSlice := argType.(*sema.SliceType); isSlice {
				idx := 1
				if fnIdent.Value == "cap" {
					idx = 2
				}
				resReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, %d\n", resReg, sl.LLVMType(), argReg, idx))
				return resReg, sema.TypeInt
			}
			if fnIdent.Value == "len" && (argType == sema.TypeString || (argType != nil && (argType.TypeName() == "string" || argType.LLVMType() == "i8*"))) {
				lenReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", lenReg, argReg))
				return lenReg, sema.TypeInt
			}
		}
	}

	if pref, ok := call.Function.(*ast.PrefixExpr); ok && pref.Operator == "*" {
		var typeName string
		var isSlice bool
		var sliceElemType sema.Type

		if ident, ok := pref.Right.(*ast.Identifier); ok {
			typeName = ident.Value
		} else if mem, ok := pref.Right.(*ast.MemberExpr); ok {
			typeName = mem.Field.Value
		} else if sl, ok := pref.Right.(*ast.SliceType); ok {
			isSlice = true
			sliceElemType = g.semaCtx.ResolveType(sl.Elem)
		}

		if isSlice || typeName != "" {
			argReg, argType := g.resolveValue(b, call.Args[0])
			srcTypeStr := "i8*"
			if argType != nil && argType.LLVMType() != "" {
				srcTypeStr = argType.LLVMType()
			}

			var targetType sema.Type
			var targetTypeStr string
			if isSlice {
				sliceT := &sema.SliceType{Elem: sliceElemType}
				targetType = &sema.PointerType{Base: sliceT}
				targetTypeStr = "{ i8*, i64, i64 }*"
			} else if typeName == "byte" || typeName == "string" {
				targetType = &sema.PointerType{Base: sema.TypeByte}
				targetTypeStr = "i8*"
			} else if typeName == "int" {
				targetType = &sema.PointerType{Base: sema.TypeInt}
				targetTypeStr = "i64*"
			} else {
				st, structName := g.findStructByName(typeName)
				if st != nil {
					targetType = &sema.PointerType{Base: st}
					targetTypeStr = fmt.Sprintf("%%struct.%s*", structName)
				} else {
					targetType = &sema.PointerType{Base: &sema.BasicType{Name: typeName, ByteSize: 8, LLVM: "%struct." + typeName}}
					targetTypeStr = fmt.Sprintf("%%struct.%s*", typeName)
				}
			}

			if targetType != nil {
				if srcTypeStr == targetTypeStr {
					return argReg, targetType
				}
				castReg := g.nextReg()
				if !strings.HasSuffix(srcTypeStr, "*") {
					b.WriteString(fmt.Sprintf("  %s = inttoptr %s %s to %s\n", castReg, srcTypeStr, argReg, targetTypeStr))
				} else {
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", castReg, srcTypeStr, argReg, targetTypeStr))
				}
				return castReg, targetType
			}
		}
	}

	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "int" && len(call.Args) == 1 {
		argReg, argType := g.resolveValue(b, call.Args[0])
		srcTypeStr := "i64"
		if argType != nil && argType.LLVMType() != "" {
			srcTypeStr = argType.LLVMType()
		}
		if srcTypeStr == "i64" {
			return argReg, sema.TypeInt
		}
		castReg := g.nextReg()
		if strings.HasSuffix(srcTypeStr, "*") {
			b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", castReg, srcTypeStr, argReg))
		} else if srcTypeStr == "i1" {
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", castReg, argReg))
		} else {
			b.WriteString(fmt.Sprintf("  %s = sext %s %s to i64\n", castReg, srcTypeStr, argReg))
		}
		return castReg, sema.TypeInt
	}

	if memExpr, ok := call.Function.(*ast.MemberExpr); ok {
		isVariable := false
		if objIdent, ok := memExpr.Object.(*ast.Identifier); ok {
			if _, exists := g.symbols[objIdent.Value]; exists {
				isVariable = true
			}
		}

		if !isVariable {
			if pkgIdent, isIdent := memExpr.Object.(*ast.Identifier); isIdent {
				methodName := memExpr.Field.Value
				targetFnName := pkgIdent.Value + "_" + methodName
				targetFn := g.lookupFunction(targetFnName)
				if targetFn == nil {
					targetFn = g.lookupFunction(methodName)
				}

				if targetFn != nil {
					args := []string{}
					for i, arg := range call.Args {
						argReg, argType := g.resolveValue(b, arg)
						var t sema.Type = argType
						if i < len(targetFn.ParamTypes) {
							t = targetFn.ParamTypes[i]
						}
						convReg := g.emitArgConversion(b, argReg, argType, t)
						targetTypeStr := "i64"
						if t != nil && t.LLVMType() != "" {
							targetTypeStr = t.LLVMType()
						}
						args = append(args, fmt.Sprintf("%s %s", targetTypeStr, convReg))
					}

					retType := "void"
					var semaRet sema.Type = sema.TypeVoid
					if len(targetFn.ReturnTypes) == 1 {
						retType = targetFn.ReturnTypes[0].LLVMType()
						semaRet = targetFn.ReturnTypes[0]
					} else if len(targetFn.ReturnTypes) > 1 {
						tupleTypes := []string{}
						for _, rt := range targetFn.ReturnTypes {
							tupleTypes = append(tupleTypes, rt.LLVMType())
						}
						retType = fmt.Sprintf("{ %s }", strings.Join(tupleTypes, ", "))
						semaRet = &sema.TupleType{Types: targetFn.ReturnTypes}
					}

					emitFnName := targetFn.Name
					if retType == "void" {
						b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", emitFnName, strings.Join(args, ", ")))
						return "", sema.TypeVoid
					}

					callReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", callReg, retType, emitFnName, strings.Join(args, ", ")))
					return callReg, semaRet
				} else {
					panic(fmt.Sprintf("[Codegen Error] undefined package function or symbol '%s.%s'", pkgIdent.Value, methodName))
				}
			}
		}

		objReg, objType := g.resolveValue(b, memExpr.Object)

		if iface, isIface := objType.(*sema.InterfaceType); isIface && !iface.IsAny() {
			methodIdx := -1
			var targetMethod sema.Method
			for idx, m := range iface.Methods {
				if m.Name == memExpr.Field.Value {
					methodIdx = idx
					targetMethod = m
					break
				}
			}
			if methodIdx == -1 {
				panic(fmt.Sprintf("[Codegen Error] method %s not found in interface %s", memExpr.Field.Value, iface.Name))
			}

			ifaceStructName := iface.Name
			if ifaceStructName == "" {
				ifaceStructName = "anon_iface"
			}

			dataPtr := g.nextReg()
			itabRaw := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 0\n", dataPtr, objReg))
			b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 1\n", itabRaw, objReg))

			itabTyped := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%struct.__itab_%s*\n", itabTyped, itabRaw, ifaceStructName))

			retTypeStr := "void"
			var semaRet sema.Type = sema.TypeVoid
			if len(targetMethod.ReturnTypes) == 1 {
				retTypeStr = targetMethod.ReturnTypes[0].LLVMType()
				semaRet = targetMethod.ReturnTypes[0]
			} else if len(targetMethod.ReturnTypes) > 1 {
				tupleTypes := []string{}
				for _, rt := range targetMethod.ReturnTypes {
					tupleTypes = append(tupleTypes, rt.LLVMType())
				}
				retTypeStr = fmt.Sprintf("{ %s }", strings.Join(tupleTypes, ", "))
				semaRet = &sema.TupleType{Types: targetMethod.ReturnTypes}
			}

			sigParams := []string{"i8*"}
			for _, pt := range targetMethod.ParamTypes {
				sigParams = append(sigParams, pt.LLVMType())
			}
			rawFnSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(sigParams, ", "))

			gepMethod := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.__itab_%s, %%struct.__itab_%s* %s, i32 0, i32 %d\n",
				gepMethod, ifaceStructName, ifaceStructName, itabTyped, methodIdx+1))

			fnPtr := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", fnPtr, rawFnSig, rawFnSig, gepMethod))

			args := []string{fmt.Sprintf("i8* %s", dataPtr)}
			for i, arg := range call.Args {
				argReg, argType := g.resolveValue(b, arg)
				var t sema.Type = argType
				if i < len(targetMethod.ParamTypes) {
					t = targetMethod.ParamTypes[i]
				}
				convReg := g.emitArgConversion(b, argReg, argType, t)
				targetTypeStr := "i64"
				if t != nil && t.LLVMType() != "" {
					targetTypeStr = t.LLVMType()
				}
				args = append(args, fmt.Sprintf("%s %s", targetTypeStr, convReg))
			}

			if retTypeStr == "void" {
				b.WriteString(fmt.Sprintf("  call void %s(%s)\n", fnPtr, strings.Join(args, ", ")))
				return "", sema.TypeVoid
			}

			callReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", callReg, retTypeStr, fnPtr, strings.Join(args, ", ")))
			return callReg, semaRet
		}

		objPtrReg, _, st, structName := g.resolveStructPtr(b, memExpr.Object)
		if st != nil && structName != "" {
			targetFnName, targetFn, finalRecvPtr, found := g.resolveMethodPath(st, structName, objPtrReg, memExpr.Field.Value, b)
			if found && targetFn != nil {
				args := []string{}
				var expectedRecvType string = fmt.Sprintf("%%struct.%s*", structName)
				if len(targetFn.ParamTypes) > 0 {
					expectedRecvType = targetFn.ParamTypes[0].LLVMType()
				}

				actualRecvReg := finalRecvPtr
				args = append(args, fmt.Sprintf("%s %s", expectedRecvType, actualRecvReg))

				for i, arg := range call.Args {
					valReg, valType := g.resolveValue(b, arg)
					var t sema.Type = valType
					if i+1 < len(targetFn.ParamTypes) {
						t = targetFn.ParamTypes[i+1]
					}
					convReg := g.emitArgConversion(b, valReg, valType, t)
					targetTypeStr := "i64"
					if t != nil && t.LLVMType() != "" {
						targetTypeStr = t.LLVMType()
					}
					args = append(args, fmt.Sprintf("%s %s", targetTypeStr, convReg))
				}

				retType := "void"
				var semaRet sema.Type = sema.TypeVoid
				if len(targetFn.ReturnTypes) == 1 {
					retType = targetFn.ReturnTypes[0].LLVMType()
					semaRet = targetFn.ReturnTypes[0]
				} else if len(targetFn.ReturnTypes) > 1 {
					tupleTypes := []string{}
					for _, rt := range targetFn.ReturnTypes {
						tupleTypes = append(tupleTypes, rt.LLVMType())
					}
					retType = fmt.Sprintf("{ %s }", strings.Join(tupleTypes, ", "))
					semaRet = &sema.TupleType{Types: targetFn.ReturnTypes}
				}

				if retType == "void" {
					b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", targetFnName, strings.Join(args, ", ")))
					return "", sema.TypeVoid
				}

				callReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", callReg, retType, targetFnName, strings.Join(args, ", ")))
				return callReg, semaRet
			}
		}
	}

	if fnIdent, ok := call.Function.(*ast.Identifier); ok {
		_, isLocal := g.symbols[fnIdent.Value]
		_, isGlobal := g.semaCtx.Globals[fnIdent.Value]

		if !isLocal && !isGlobal {
			targetFn := g.lookupFunction(fnIdent.Value)
			if targetFn != nil {
				args := []string{}
				for i, arg := range call.Args {
					argReg, argType := g.resolveValue(b, arg)
					var t sema.Type = argType
					if i < len(targetFn.ParamTypes) {
						t = targetFn.ParamTypes[i]
					}
					convReg := g.emitArgConversion(b, argReg, argType, t)
					targetTypeStr := "i64"
					if t != nil && t.LLVMType() != "" {
						targetTypeStr = t.LLVMType()
					}
					args = append(args, fmt.Sprintf("%s %s", targetTypeStr, convReg))
				}

				retType := "void"
				var semaRet sema.Type = sema.TypeVoid
				sigParamTypes := []string{}

				if len(targetFn.ReturnTypes) == 1 {
					retType = targetFn.ReturnTypes[0].LLVMType()
					semaRet = targetFn.ReturnTypes[0]
				} else if len(targetFn.ReturnTypes) > 1 {
					tupleTypes := []string{}
					for _, rt := range targetFn.ReturnTypes {
						tupleTypes = append(tupleTypes, rt.LLVMType())
					}
					retType = fmt.Sprintf("{ %s }", strings.Join(tupleTypes, ", "))
					semaRet = &sema.TupleType{Types: targetFn.ReturnTypes}
				}

				for _, p := range targetFn.ParamTypes {
					sigParamTypes = append(sigParamTypes, p.LLVMType())
				}
				if targetFn.IsVariadic {
					sigParamTypes = append(sigParamTypes, "...")
				}

				emitFnName := targetFn.Name
				if targetFn.IsExtern {
					emitFnName = fnIdent.Value
				}

				if retType == "void" {
					if len(sigParamTypes) > 0 {
						b.WriteString(fmt.Sprintf("  call void (%s) @%s(%s)\n", strings.Join(sigParamTypes, ", "), emitFnName, strings.Join(args, ", ")))
					} else {
						b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", emitFnName, strings.Join(args, ", ")))
					}
					return "", sema.TypeVoid
				}

				callReg := g.nextReg()
				if len(sigParamTypes) > 0 {
					b.WriteString(fmt.Sprintf("  %s = call %s (%s) @%s(%s)\n", callReg, retType, strings.Join(sigParamTypes, ", "), emitFnName, strings.Join(args, ", ")))
				} else {
					b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", callReg, retType, emitFnName, strings.Join(args, ", ")))
				}
				return callReg, semaRet
			}
		}
	}

	fnReg, fnValType := g.resolveValue(b, call.Function)
	ft, isFuncType := fnValType.(*sema.FuncType)
	if !isFuncType {
		panic(fmt.Sprintf("[Codegen Error] expression is not a function: %v", fnValType))
	}

	fnRawPtr := g.nextReg()
	envRawPtr := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 0\n", fnRawPtr, fnReg))
	b.WriteString(fmt.Sprintf("  %s = extractvalue { i8*, i8* } %s, 1\n", envRawPtr, fnReg))

	retTypeStr := "void"
	var semaRet sema.Type = sema.TypeVoid
	if len(ft.ReturnTypes) == 1 {
		retTypeStr = ft.ReturnTypes[0].LLVMType()
		semaRet = ft.ReturnTypes[0]
	} else if len(ft.ReturnTypes) > 1 {
		tupleTypes := []string{}
		for _, rt := range ft.ReturnTypes {
			tupleTypes = append(tupleTypes, rt.LLVMType())
		}
		retTypeStr = fmt.Sprintf("{ %s }", strings.Join(tupleTypes, ", "))
		semaRet = &sema.TupleType{Types: ft.ReturnTypes}
	}

	sigParams := []string{"i8*"}
	for _, pt := range ft.ParamTypes {
		sigParams = append(sigParams, pt.LLVMType())
	}
	rawFnSig := fmt.Sprintf("%s (%s)*", retTypeStr, strings.Join(sigParams, ", "))

	typedFnPtr := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", typedFnPtr, fnRawPtr, rawFnSig))

	args := []string{fmt.Sprintf("i8* %s", envRawPtr)}
	for i, arg := range call.Args {
		argReg, argType := g.resolveValue(b, arg)
		var t sema.Type = argType
		if i < len(ft.ParamTypes) {
			t = ft.ParamTypes[i]
		}
		convReg := g.emitArgConversion(b, argReg, argType, t)
		targetTypeStr := "i64"
		if t != nil && t.LLVMType() != "" {
			targetTypeStr = t.LLVMType()
		}
		args = append(args, fmt.Sprintf("%s %s", targetTypeStr, convReg))
	}

	if retTypeStr == "void" {
		b.WriteString(fmt.Sprintf("  call void %s(%s)\n", typedFnPtr, strings.Join(args, ", ")))
		return "", sema.TypeVoid
	}

	callReg := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = call %s %s(%s)\n", callReg, retTypeStr, typedFnPtr, strings.Join(args, ", ")))
	return callReg, semaRet
}
