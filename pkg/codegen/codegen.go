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
	target         *Target       // 追加
	debugMgr       *DebugManager // 追加
	currentFunc    string        // 追加
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

func New(prog *ast.Program, semaCtx *sema.Context, target *Target, sourcePath string, debugEnabled bool) *CodeGenerator {
	if target == nil {
		target = DefaultTarget()
	}
	return &CodeGenerator{
		prog:           prog,
		semaCtx:        semaCtx,
		target:         target,
		debugMgr:       NewDebugManager(sourcePath, debugEnabled),
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

func (g *CodeGenerator) dbg(line, col int) string {
	if g.debugMgr == nil {
		return ""
	}
	return g.debugMgr.GetLocationTag(line, col)
}
func (g *CodeGenerator) SetVerbose(v bool) {
	g.verbose = v
}

func (g *CodeGenerator) SetDebug(sourcePath string, enabled bool) {
	g.debugMgr = NewDebugManager(sourcePath, enabled)
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
			if argReg == "null" || argReg == "0" || argReg == "zeroinitializer" || argType == nil {
				return "zeroinitializer"
			}
			return g.boxToInterface(b, argReg, argType, iface)
		}
	}

	if (argReg == "null" || argReg == "0") && !strings.HasSuffix(targetType.LLVMType(), "*") {
		return "zeroinitializer"
	}

	src := argType.LLVMType()
	dst := targetType.LLVMType()
	if src == dst {
		return argReg
	}

	// 1. ポインタ間の bitcast
	if strings.HasSuffix(src, "*") && strings.HasSuffix(dst, "*") {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, src, argReg, dst))
		return convReg
	}
	if strings.HasSuffix(src, "*") && dst == "i64" {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", convReg, src, argReg))
		return convReg
	}
	if src == "i64" && strings.HasSuffix(dst, "*") {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", convReg, argReg, dst))
		return convReg
	}

	// 2. 整数 -> 浮動小数点数 (sitofp)
	if (src == "i64" || src == "i32" || src == "i8") && (dst == "double" || dst == "float") {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = sitofp %s %s to %s\n", convReg, src, argReg, dst))
		return convReg
	}
	if src == "i1" && (dst == "double" || dst == "float") {
		zextReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", zextReg, argReg))
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = sitofp i64 %s to %s\n", convReg, zextReg, dst))
		return convReg
	}

	// 3. 浮動小数点数 -> 整数 (fptosi)
	if (src == "double" || src == "float") && (dst == "i64" || dst == "i32" || dst == "i8") {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = fptosi %s %s to %s\n", convReg, src, argReg, dst))
		return convReg
	}

	// 4. 浮動小数点数間の拡張・縮小 (fpext / fptrunc)
	if src == "float" && dst == "double" {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = fpext float %s to double\n", convReg, argReg))
		return convReg
	}
	if src == "double" && dst == "float" {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = fptrunc double %s to float\n", convReg, argReg))
		return convReg
	}

	// 5. 整数間の変換
	if src == "i1" && dst == "i64" {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", convReg, argReg))
		return convReg
	}
	if src == "i8" && dst == "i64" {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", convReg, argReg))
		return convReg
	}
	if src == "i64" && dst == "i8" {
		convReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", convReg, argReg))
		return convReg
	}

	return argReg
}

func (g *CodeGenerator) Generate() string {
	var bodyBuilder strings.Builder

	for i := 0; i < len(g.prog.Decls); i++ {
		decl := g.prog.Decls[i]
		if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Body != nil {
			if len(fnDecl.TypeParams) > 0 {
				continue
			}
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

	// デバッグメタデータの出力
	if g.debugMgr != nil && g.debugMgr.enabled {
		g.output.WriteString(g.debugMgr.EmitMetadata())
	}

	return g.output.String()
}

func (g *CodeGenerator) emitPrologue() {
	srcName := "main.hike"
	if g.debugMgr != nil && g.debugMgr.filename != "" {
		srcName = g.debugMgr.filename
	}
	g.output.WriteString(fmt.Sprintf("; ModuleID = '%s'\n", srcName))
	g.output.WriteString(fmt.Sprintf("source_filename = \"%s\"\n", srcName))
	g.output.WriteString(fmt.Sprintf("target triple = \"%s\"\n\n", g.target.Triple))

	// デバッグ用組み込み関数
	g.output.WriteString("declare void @llvm.dbg.declare(metadata, metadata, metadata)\n\n")

	// C/WASI 標準ライブラリ宣言
	g.output.WriteString("declare noalias i8* @malloc(i64)\n")
	g.output.WriteString("declare noalias i8* @calloc(i64, i64)\n")
	g.output.WriteString("declare void @free(i8*)\n")
	g.output.WriteString("declare i32 @strcmp(i8*, i8*)\n")
	g.output.WriteString("declare i64 @strlen(i8*)\n")
	g.output.WriteString("declare i8* @memcpy(i8*, i8*, i64)\n")
	g.output.WriteString("declare i32 @memcmp(i8*, i8*, i64)\n")
	g.output.WriteString("declare i64 @printf(i8*, ...)\n\n")

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
		g.output.WriteString(fmt.Sprintf("@%s = global %s zeroinitializer, align 8\n", name, gType.LLVMType()))
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

	g.output.WriteString("%struct.__hike_map_entry = type { i64, i64, i64, %struct.__hike_map_entry* }\n")
	g.output.WriteString("%struct.__hike_map = type { %struct.__hike_map_entry**, i64, i64, i64 }\n\n")

	g.output.WriteString(`define i64 @__hike_hash_str(i8* %s) {
entry:
  %null_chk = icmp eq i8* %s, null
  br i1 %null_chk, label %ret_zero, label %loop_init
ret_zero:
  ret i64 0
loop_init:
  br label %loop.cond
loop.cond:
  %h = phi i64 [ -3750763034362895579, %loop_init ], [ %h.next, %loop.body ]
  %ptr = phi i8* [ %s, %loop_init ], [ %ptr.next, %loop.body ]
  %ch = load i8, i8* %ptr
  %is_null = icmp eq i8 %ch, 0
  br i1 %is_null, label %loop.end, label %loop.body
loop.body:
  %ch.zext = zext i8 %ch to i64
  %h.xor = xor i64 %h, %ch.zext
  %h.next = mul i64 %h.xor, 1099511628211
  %ptr.next = getelementptr inbounds i8, i8* %ptr, i64 1
  br label %loop.cond
loop.end:
  ret i64 %h
}

define i1 @__hike_map_key_eq(i64 %k1, i64 %k2, i64 %is_str) {
entry:
  %is_s = icmp ne i64 %is_str, 0
  br i1 %is_s, label %check_str, label %check_int
check_int:
  %eq_int = icmp eq i64 %k1, %k2
  ret i1 %eq_int
check_str:
  %p1 = inttoptr i64 %k1 to i8*
  %p2 = inttoptr i64 %k2 to i8*
  %eq_ptr = icmp eq i8* %p1, %p2
  br i1 %eq_ptr, label %ret_true, label %check_null
check_null:
  %n1 = icmp eq i8* %p1, null
  %n2 = icmp eq i8* %p2, null
  %either_null = or i1 %n1, %n2
  br i1 %either_null, label %ret_false, label %do_cmp
do_cmp:
  %res = call i32 @strcmp(i8* %p1, i8* %p2)
  %is_z = icmp eq i32 %res, 0
  ret i1 %is_z
ret_true:
  ret i1 true
ret_false:
  ret i1 false
}

define %struct.__hike_map* @__hike_map_create(i64 %cap, i64 %is_str) {
entry:
  %raw = call i8* @malloc(i64 32)
  %m = bitcast i8* %raw to %struct.__hike_map*
  %buckets_raw = call i8* @calloc(i64 16, i64 8)
  %buckets = bitcast i8* %buckets_raw to %struct.__hike_map_entry**
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  store %struct.__hike_map_entry** %buckets, %struct.__hike_map_entry*** %p_b
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  store i64 16, i64* %p_nb
  %p_len = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 2
  store i64 0, i64* %p_len
  %p_str = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 3
  store i64 %is_str, i64* %p_str
  ret %struct.__hike_map* %m
}

define void @__hike_map_grow(%struct.__hike_map* %m) {
entry:
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %old_nb = load i64, i64* %p_nb
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  %old_b = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_b
  %new_nb = mul i64 %old_nb, 2
  %new_raw = call i8* @calloc(i64 %new_nb, i64 8)
  %new_b = bitcast i8* %new_raw to %struct.__hike_map_entry**
  br label %loop.i
loop.i:
  %i = phi i64 [ 0, %entry ], [ %i.next, %loop.i.inc ]
  %cmp.i = icmp slt i64 %i, %old_nb
  br i1 %cmp.i, label %loop.entry.init, label %loop.i.done
loop.entry.init:
  %p_cur_head = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %old_b, i64 %i
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_cur_head
  br label %loop.entry
loop.entry:
  %cur = phi %struct.__hike_map_entry* [ %head, %loop.entry.init ], [ %nxt, %loop.entry.body ]
  %has_cur = icmp ne %struct.__hike_map_entry* %cur, null
  br i1 %has_cur, label %loop.entry.body, label %loop.i.inc
loop.entry.body:
  %p_nxt = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %nxt = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_nxt
  %p_hash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 0
  %h_val = load i64, i64* %p_hash
  %h_pos = and i64 %h_val, 9223372036854775807
  %new_idx = urem i64 %h_pos, %new_nb
  %p_new_slot = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %new_b, i64 %new_idx
  %existing = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_new_slot
  store %struct.__hike_map_entry* %existing, %struct.__hike_map_entry** %p_nxt
  store %struct.__hike_map_entry* %cur, %struct.__hike_map_entry** %p_new_slot
  br label %loop.entry
loop.i.inc:
  %i.next = add i64 %i, 1
  br label %loop.i
loop.i.done:
  %old_b_raw = bitcast %struct.__hike_map_entry** %old_b to i8*
  call void @free(i8* %old_b_raw)
  store %struct.__hike_map_entry** %new_b, %struct.__hike_map_entry*** %p_b
  store i64 %new_nb, i64* %p_nb
  ret void
}

define void @__hike_map_set(%struct.__hike_map* %m, i64 %key, i64 %val) {
entry:
  %null_m = icmp eq %struct.__hike_map* %m, null
  br i1 %null_m, label %ret_void, label %check_grow
ret_void:
  ret void
check_grow:
  %p_len = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 2
  %cur_len = load i64, i64* %p_len
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %nb = load i64, i64* %p_nb
  %limit = mul i64 %nb, 3
  %limit_div = sdiv i64 %limit, 4
  %needs_grow = icmp sge i64 %cur_len, %limit_div
  br i1 %needs_grow, label %do_grow, label %do_hash
do_grow:
  call void @__hike_map_grow(%struct.__hike_map* %m)
  br label %do_hash
do_hash:
  %p_str = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 3
  %is_str = load i64, i64* %p_str
  %is_s = icmp ne i64 %is_str, 0
  br i1 %is_s, label %hash_str, label %hash_int
hash_str:
  %k_ptr = inttoptr i64 %key to i8*
  %h_s = call i64 @__hike_hash_str(i8* %k_ptr)
  br label %lookup
hash_int:
  br label %lookup
lookup:
  %hash = phi i64 [ %h_s, %hash_str ], [ %key, %hash_int ]
  %p_nb_2 = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %nb_cur = load i64, i64* %p_nb_2
  %h_pos = and i64 %hash, 9223372036854775807
  %idx = urem i64 %h_pos, %nb_cur
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  %buckets = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_b
  %p_head = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets, i64 %idx
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  br label %search.entry
search.entry:
  %cur = phi %struct.__hike_map_entry* [ %head, %lookup ], [ %cur.next, %search.next ]
  %has_entry = icmp ne %struct.__hike_map_entry* %cur, null
  br i1 %has_entry, label %search.body, label %insert_new
search.body:
  %p_ehash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 0
  %ehash = load i64, i64* %p_ehash
  %hash_match = icmp eq i64 %ehash, %hash
  br i1 %hash_match, label %search.key_check, label %search.next
search.key_check:
  %p_ekey = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 1
  %ekey = load i64, i64* %p_ekey
  %key_match = call i1 @__hike_map_key_eq(i64 %ekey, i64 %key, i64 %is_str)
  br i1 %key_match, label %update_val, label %search.next
update_val:
  %p_eval = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 2
  store i64 %val, i64* %p_eval
  ret void
search.next:
  %p_enext = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %cur.next = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_enext
  br label %search.entry
insert_new:
  %new_entry_raw = call i8* @malloc(i64 32)
  %new_e = bitcast i8* %new_entry_raw to %struct.__hike_map_entry*
  %np_hash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %new_e, i32 0, i32 0
  store i64 %hash, i64* %np_hash
  %np_key = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %new_e, i32 0, i32 1
  store i64 %key, i64* %np_key
  %np_val = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %new_e, i32 0, i32 2
  store i64 %val, i64* %np_val
  %np_next = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %new_e, i32 0, i32 3
  %cur_head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  store %struct.__hike_map_entry* %cur_head, %struct.__hike_map_entry** %np_next
  store %struct.__hike_map_entry* %new_e, %struct.__hike_map_entry** %p_head
  %new_len = add i64 %cur_len, 1
  store i64 %new_len, i64* %p_len
  ret void
}

define i1 @__hike_map_get(%struct.__hike_map* %m, i64 %key, i64* %out_val) {
entry:
  %null_m = icmp eq %struct.__hike_map* %m, null
  br i1 %null_m, label %ret_not_found, label %do_lookup
ret_not_found:
  store i64 0, i64* %out_val
  ret i1 false
do_lookup:
  %p_str = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 3
  %is_str = load i64, i64* %p_str
  %is_s = icmp ne i64 %is_str, 0
  br i1 %is_s, label %hash_str, label %hash_int
hash_str:
  %k_ptr = inttoptr i64 %key to i8*
  %h_s = call i64 @__hike_hash_str(i8* %k_ptr)
  br label %search_init
hash_int:
  br label %search_init
search_init:
  %hash = phi i64 [ %h_s, %hash_str ], [ %key, %hash_int ]
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %nb = load i64, i64* %p_nb
  %h_pos = and i64 %hash, 9223372036854775807
  %idx = urem i64 %h_pos, %nb
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  %buckets = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_b
  %p_head = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets, i64 %idx
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  br label %search.entry
search.entry:
  %cur = phi %struct.__hike_map_entry* [ %head, %search_init ], [ %cur.next, %search.next ]
  %has_entry = icmp ne %struct.__hike_map_entry* %cur, null
  br i1 %has_entry, label %search.body, label %ret_not_found
search.body:
  %p_ehash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 0
  %ehash = load i64, i64* %p_ehash
  %hash_match = icmp eq i64 %ehash, %hash
  br i1 %hash_match, label %search.key_check, label %search.next
search.key_check:
  %p_ekey = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 1
  %ekey = load i64, i64* %p_ekey
  %key_match = call i1 @__hike_map_key_eq(i64 %ekey, i64 %key, i64 %is_str)
  br i1 %key_match, label %found, label %search.next
found:
  %p_eval = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 2
  %val = load i64, i64* %p_eval
  store i64 %val, i64* %out_val
  ret i1 true
search.next:
  %p_enext = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %cur.next = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_enext
  br label %search.entry
}

define void @__hike_map_delete(%struct.__hike_map* %m, i64 %key) {
entry:
  %null_m = icmp eq %struct.__hike_map* %m, null
  br i1 %null_m, label %ret_void, label %do_del
ret_void:
  ret void
do_del:
  %p_str = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 3
  %is_str = load i64, i64* %p_str
  %is_s = icmp ne i64 %is_str, 0
  br i1 %is_s, label %hash_str, label %hash_int
hash_str:
  %k_ptr = inttoptr i64 %key to i8*
  %h_s = call i64 @__hike_hash_str(i8* %k_ptr)
  br label %search_init
hash_int:
  br label %search_init
search_init:
  %hash = phi i64 [ %h_s, %hash_str ], [ %key, %hash_int ]
  %p_nb = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 1
  %nb = load i64, i64* %p_nb
  %h_pos = and i64 %hash, 9223372036854775807
  %idx = urem i64 %h_pos, %nb
  %p_b = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 0
  %buckets = load %struct.__hike_map_entry**, %struct.__hike_map_entry*** %p_b
  %p_head = getelementptr inbounds %struct.__hike_map_entry*, %struct.__hike_map_entry** %buckets, i64 %idx
  %head = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_head
  br label %search.entry
search.entry:
  %prev = phi %struct.__hike_map_entry* [ null, %search_init ], [ %cur, %search.next ]
  %cur = phi %struct.__hike_map_entry* [ %head, %search_init ], [ %cur.next, %search.next ]
  %has_entry = icmp ne %struct.__hike_map_entry* %cur, null
  br i1 %has_entry, label %search.body, label %ret_void
search.body:
  %p_ehash = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 0
  %ehash = load i64, i64* %p_ehash
  %hash_match = icmp eq i64 %ehash, %hash
  br i1 %hash_match, label %search.key_check, label %search.next
search.key_check:
  %p_ekey = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 1
  %ekey = load i64, i64* %p_ekey
  %key_match = call i1 @__hike_map_key_eq(i64 %ekey, i64 %key, i64 %is_str)
  br i1 %key_match, label %do_unlink, label %search.next
do_unlink:
  %p_enext = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %nxt = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_enext
  %has_prev = icmp ne %struct.__hike_map_entry* %prev, null
  br i1 %has_prev, label %unlink_prev, label %unlink_head
unlink_prev:
  %p_pnext = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %prev, i32 0, i32 3
  store %struct.__hike_map_entry* %nxt, %struct.__hike_map_entry** %p_pnext
  br label %after_unlink
unlink_head:
  store %struct.__hike_map_entry* %nxt, %struct.__hike_map_entry** %p_head
  br label %after_unlink
after_unlink:
  %cur_raw = bitcast %struct.__hike_map_entry* %cur to i8*
  call void @free(i8* %cur_raw)
  %p_len = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 2
  %cur_len = load i64, i64* %p_len
  %new_len = sub i64 %cur_len, 1
  store i64 %new_len, i64* %p_len
  ret void
search.next:
  %p_enext2 = getelementptr inbounds %struct.__hike_map_entry, %struct.__hike_map_entry* %cur, i32 0, i32 3
  %cur.next = load %struct.__hike_map_entry*, %struct.__hike_map_entry** %p_enext2
  br label %search.entry
}

define i64 @__hike_map_len(%struct.__hike_map* %m) {
entry:
  %null_m = icmp eq %struct.__hike_map* %m, null
  br i1 %null_m, label %ret_zero, label %get_len
ret_zero:
  ret i64 0
get_len:
  %p_len = getelementptr inbounds %struct.__hike_map, %struct.__hike_map* %m, i32 0, i32 2
  %l = load i64, i64* %p_len
  ret i64 %l
}
` + "\n")
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

// ポインタを i64（Hikeのint）に変換
func (g *CodeGenerator) emitPtrToInt(b *strings.Builder, ptrReg string, ptrType string) string {
	if g.target.PtrType == "i32" {
		p32 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i32\n", p32, ptrType, ptrReg))
		p64 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = zext i32 %s to i64\n", p64, p32))
		return p64
	}
	pInt := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", pInt, ptrType, ptrReg))
	return pInt
}

// i64（Hikeのint）をポインタに変換
func (g *CodeGenerator) emitIntToPtr(b *strings.Builder, intReg string, targetPtrType string) string {
	if g.target.PtrType == "i32" {
		t32 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i32\n", t32, intReg))
		pReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = inttoptr i32 %s to %s\n", pReg, t32, targetPtrType))
		return pReg
	}
	pReg := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", pReg, intReg, targetPtrType))
	return pReg
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

	// ★★★ 最重要: 関数本体のコンパイル前にスコープIDを切り替える ★★★
	dbgFuncTag := ""
	if g.debugMgr != nil && g.debugMgr.enabled {
		spID := g.debugMgr.StartFunction(funcMangledName, fn.Token.Line)
		dbgFuncTag = fmt.Sprintf(" !dbg !%d", spID)
	}

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

	params := []string{}
	if fn.Name.Value == "main" {
		retType = "i32"
		params = []string{"i32 %argc", "i8** %argv"}
	} else {
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
		if g.debugMgr != nil && g.debugMgr.enabled {
			varID, locID := g.debugMgr.RegisterLocalVariable(p.Name.Value, p.Token.Line, p.Token.Col, pType, true, i+1)
			if varID > 0 {
				entryAllocas.WriteString(fmt.Sprintf("  call void @llvm.dbg.declare(metadata %s* %%%s, metadata !%d, metadata !DIExpression()), !dbg !%d\n",
					pType.LLVMType(), p.Name.Value, varID, locID))
			}
		}
		bodyBuilder.WriteString(fmt.Sprintf("  store %s %%%s_arg, %s* %%%s\n", pType.LLVMType(), p.Name.Value, pType.LLVMType(), p.Name.Value))
		g.symbols[p.Name.Value] = Symbol{
			Name:     p.Name.Value,
			LLVMName: "%" + p.Name.Value,
			Type:     pType,
		}
	}

	if fn.Name.Value == "main" {
		if _, exists := g.semaCtx.Globals["os_Args"]; exists {
			bodyBuilder.WriteString("  ; initialize os.Args ([]string)\n")
			bodyBuilder.WriteString("  %argc_64 = sext i32 %argc to i64\n")
			bodyBuilder.WriteString("  %argv_bytes = mul i64 %argc_64, 8\n")
			bodyBuilder.WriteString("  %argv_buf = call i8* @calloc(i64 %argc_64, i64 8)\n")
			bodyBuilder.WriteString("  %argv_buf_typed = bitcast i8* %argv_buf to i8**\n")
			bodyBuilder.WriteString("  br label %argv.loop.cond\n\n")

			bodyBuilder.WriteString("argv.loop.cond:\n")
			bodyBuilder.WriteString("  %i.arg = phi i64 [ 0, %entry ], [ %i.arg.next, %argv.loop.body ]\n")
			bodyBuilder.WriteString("  %cmp.arg = icmp slt i64 %i.arg, %argc_64\n")
			bodyBuilder.WriteString("  br i1 %cmp.arg, label %argv.loop.body, label %argv.loop.end\n\n")

			bodyBuilder.WriteString("argv.loop.body:\n")
			bodyBuilder.WriteString("  %src.ptr = getelementptr inbounds i8*, i8** %argv, i64 %i.arg\n")
			bodyBuilder.WriteString("  %arg.val = load i8*, i8** %src.ptr\n")
			bodyBuilder.WriteString("  %dst.ptr = getelementptr inbounds i8*, i8** %argv_buf_typed, i64 %i.arg\n")
			bodyBuilder.WriteString("  store i8* %arg.val, i8** %dst.ptr\n")
			bodyBuilder.WriteString("  %i.arg.next = add i64 %i.arg, 1\n")
			bodyBuilder.WriteString("  br label %argv.loop.cond\n\n")

			bodyBuilder.WriteString("argv.loop.end:\n")
			bodyBuilder.WriteString("  %args.t1 = insertvalue { i8*, i64, i64 } undef, i8* %argv_buf, 0\n")
			bodyBuilder.WriteString("  %args.t2 = insertvalue { i8*, i64, i64 } %args.t1, i64 %argc_64, 1\n")
			bodyBuilder.WriteString("  %args.t3 = insertvalue { i8*, i64, i64 } %args.t2, i64 %argc_64, 2\n")
			bodyBuilder.WriteString("  store { i8*, i64, i64 } %args.t3, { i8*, i64, i64 }* @os_Args\n\n")
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

	b.WriteString(fmt.Sprintf("define %s @%s(%s)%s {\nentry:\n", retType, funcMangledName, strings.Join(params, ", "), dbgFuncTag))
	b.WriteString(entryAllocas.String())
	b.WriteString(bodyBuilder.String())
	b.WriteString("}\n\n")
}

func (g *CodeGenerator) getOrCreateSpecializedFunc(baseName string, typeArgs []sema.Type) string {
	argNames := []string{}
	for _, t := range typeArgs {
		name := strings.ReplaceAll(t.TypeName(), "*", "Ptr")
		name = strings.ReplaceAll(name, "[]", "Slice_")
		argNames = append(argNames, name)
	}
	specializedName := fmt.Sprintf("%s__%s", baseName, strings.Join(argNames, "_"))

	if _, exists := g.semaCtx.Functions[specializedName]; exists {
		return specializedName
	}

	template, exists := g.semaCtx.GenericFuncs[baseName]
	if !exists {
		return baseName
	}

	typeMap := make(map[string]ast.TypeExpr)
	for i, tp := range template.TypeParams {
		if i < len(typeArgs) {
			typeMap[tp.Name.Value] = &ast.NamedType{
				Token: tp.Token,
				Name:  &ast.Identifier{Token: tp.Token, Value: typeArgs[i].TypeName()},
			}
		}
	}

	// AST のクローンと型置換
	cloned := g.cloneFuncDecl(template, specializedName, typeMap)

	// セマンティクスに関数を登録
	fnType := &sema.FuncType{
		Name:        specializedName,
		ParamTypes:  []sema.Type{},
		ReturnTypes: []sema.Type{},
	}
	for _, p := range cloned.Params {
		fnType.ParamTypes = append(fnType.ParamTypes, g.semaCtx.ResolveType(p.Type))
	}
	for _, rt := range cloned.ReturnTypes {
		fnType.ReturnTypes = append(fnType.ReturnTypes, g.semaCtx.ResolveType(rt))
	}
	g.semaCtx.Functions[specializedName] = fnType

	// プログラムの関数一覧に追加してLLVMコード生成対象にする
	g.prog.Decls = append(g.prog.Decls, cloned)
	return specializedName
}

func (g *CodeGenerator) cloneFuncDecl(fn *ast.FuncDecl, newName string, typeMap map[string]ast.TypeExpr) *ast.FuncDecl {
	newParams := []*ast.ParamDecl{}
	for _, p := range fn.Params {
		newParams = append(newParams, &ast.ParamDecl{
			Token: p.Token,
			Name:  p.Name,
			Type:  substituteAstType(p.Type, typeMap),
		})
	}
	newReturns := []ast.TypeExpr{}
	for _, rt := range fn.ReturnTypes {
		newReturns = append(newReturns, substituteAstType(rt, typeMap))
	}
	return &ast.FuncDecl{
		Token:       fn.Token,
		Receiver:    fn.Receiver,
		Name:        &ast.Identifier{Token: fn.Name.Token, Value: newName},
		TypeParams:  nil, // 特殊化済み具象関数のため nil に設定
		Params:      newParams,
		IsVariadic:  fn.IsVariadic,
		ReturnTypes: newReturns,
		Body:        fn.Body,
	}
}

func substituteAstType(t ast.TypeExpr, typeMap map[string]ast.TypeExpr) ast.TypeExpr {
	if t == nil {
		return nil
	}
	switch node := t.(type) {
	case *ast.NamedType:
		if node.Package == nil && len(node.TypeArgs) == 0 {
			if rep, ok := typeMap[node.Name.Value]; ok {
				return rep
			}
		}
		newArgs := []ast.TypeExpr{}
		for _, arg := range node.TypeArgs {
			newArgs = append(newArgs, substituteAstType(arg, typeMap))
		}
		return &ast.NamedType{Token: node.Token, Package: node.Package, Name: node.Name, TypeArgs: newArgs}
	case *ast.PointerType:
		return &ast.PointerType{Token: node.Token, Base: substituteAstType(node.Base, typeMap)}
	case *ast.SliceType:
		return &ast.SliceType{Token: node.Token, Elem: substituteAstType(node.Elem, typeMap)}
	case *ast.ArrayType:
		return &ast.ArrayType{Token: node.Token, Len: node.Len, Elem: substituteAstType(node.Elem, typeMap)}
	case *ast.MapType:
		return &ast.MapType{
			Token: node.Token,
			Key:   substituteAstType(node.Key, typeMap),
			Value: substituteAstType(node.Value, typeMap),
		}
	}
	return t
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
	dTag := g.dbg(s.Token.Line, s.Token.Col)

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
	if g.debugMgr != nil && g.debugMgr.enabled {
		varID, locID := g.debugMgr.RegisterLocalVariable(name, s.Token.Line, s.Token.Col, targetType, false, 0)
		if varID > 0 {
			g.entryAllocas.WriteString(fmt.Sprintf("  call void @llvm.dbg.declare(metadata %s* %%%s, metadata !%d, metadata !DIExpression()), !dbg !%d\n",
				targetType.LLVMType(), name, varID, locID))
		}
	}
	g.symbols[name] = Symbol{Name: name, LLVMName: "%" + name, Type: targetType}

	if s.Value != nil {
		finalValReg := g.emitArgConversion(b, valReg, valType, targetType)
		b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s%s\n", targetType.LLVMType(), finalValReg, targetType.LLVMType(), name, dTag))
	} else {
		b.WriteString(fmt.Sprintf("  store %s zeroinitializer, %s* %%%s%s\n", targetType.LLVMType(), targetType.LLVMType(), name, dTag))
	}
}

func (g *CodeGenerator) emitAssignStmt(b *strings.Builder, s *ast.AssignStmt) {
	dTag := g.dbg(s.Token.Line, s.Token.Col)

	// =========================================================================
	// 1. マップの存在確認（カンマokイディオム: val, ok := m[key]）
	// =========================================================================
	if len(s.Left) == 2 && len(s.Right) == 1 {
		if idxExpr, isIdx := s.Right[0].(*ast.IndexExpr); isIdx {
			baseReg, baseType := g.resolveValue(b, idxExpr.Left)
			if mp, isMap := baseType.(*sema.MapType); isMap {
				keyReg, keyType := g.resolveValue(b, idxExpr.Index)
				keyArg := keyReg
				if keyType == sema.TypeString || strings.HasSuffix(keyType.LLVMType(), "*") {
					pInt := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", pInt, keyType.LLVMType(), keyReg))
					keyArg = pInt
				} else if keyType == sema.TypeFloat64 {
					castReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast double %s to i64\n", castReg, keyReg))
					keyArg = castReg
				}

				outAlloca := g.nextReg()
				g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i64\n", outAlloca))
				okReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = call i1 @__hike_map_get(%%struct.__hike_map* %s, i64 %s, i64* %s)%s\n", okReg, baseReg, keyArg, outAlloca, dTag))

				rawValReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", rawValReg, outAlloca))

				var finalValReg string = rawValReg
				if mp.Value == sema.TypeString || strings.HasSuffix(mp.Value.LLVMType(), "*") {
					castReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", castReg, rawValReg, mp.Value.LLVMType()))
					finalValReg = castReg
				} else if mp.Value == sema.TypeFloat64 {
					castReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast i64 %s to double\n", castReg, rawValReg))
					finalValReg = castReg
				} else if mp.Value == sema.TypeBool {
					truncReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i1\n", truncReg, rawValReg))
					finalValReg = truncReg
				} else if mp.Value == sema.TypeByte {
					truncReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", truncReg, rawValReg))
					finalValReg = truncReg
				}

				// 左辺 1 個目 (val)
				if vIdent, ok := s.Left[0].(*ast.Identifier); ok && vIdent.Value != "_" {
					if _, exists := g.symbols[vIdent.Value]; !exists {
						g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", vIdent.Value, mp.Value.LLVMType()))
						if g.debugMgr != nil && g.debugMgr.enabled {
							varID, locID := g.debugMgr.RegisterLocalVariable(vIdent.Value, s.Token.Line, s.Token.Col, mp.Value, false, 0)
							if varID > 0 {
								g.entryAllocas.WriteString(fmt.Sprintf("  call void @llvm.dbg.declare(metadata %s* %%%s, metadata !%d, metadata !DIExpression()), !dbg !%d\n",
									mp.Value.LLVMType(), vIdent.Value, varID, locID))
							}
						}
						g.symbols[vIdent.Value] = Symbol{Name: vIdent.Value, LLVMName: "%" + vIdent.Value, Type: mp.Value}
					}
					b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s%s\n", mp.Value.LLVMType(), finalValReg, mp.Value.LLVMType(), vIdent.Value, dTag))
				}
				// 左辺 2 個目 (ok)
				if okIdent, ok := s.Left[1].(*ast.Identifier); ok && okIdent.Value != "_" {
					if _, exists := g.symbols[okIdent.Value]; !exists {
						g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca i1\n", okIdent.Value))
						if g.debugMgr != nil && g.debugMgr.enabled {
							varID, locID := g.debugMgr.RegisterLocalVariable(okIdent.Value, s.Token.Line, s.Token.Col, sema.TypeBool, false, 0)
							if varID > 0 {
								g.entryAllocas.WriteString(fmt.Sprintf("  call void @llvm.dbg.declare(metadata i1* %%%s, metadata !%d, metadata !DIExpression()), !dbg !%d\n",
									okIdent.Value, varID, locID))
							}
						}
						g.symbols[okIdent.Value] = Symbol{Name: okIdent.Value, LLVMName: "%" + okIdent.Value, Type: sema.TypeBool}
					}
					b.WriteString(fmt.Sprintf("  store i1 %s, i1* %%%s%s\n", okReg, okIdent.Value, dTag))
				}
				return
			}
		}
	}

	// =========================================================================
	// 2. 通常の多値返却代入（タプル代入: a, b := fn()）
	// =========================================================================
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
					if g.debugMgr != nil && g.debugMgr.enabled {
						varID, locID := g.debugMgr.RegisterLocalVariable(lhsIdent.Value, s.Token.Line, s.Token.Col, elemType, false, 0)
						if varID > 0 {
							g.entryAllocas.WriteString(fmt.Sprintf("  call void @llvm.dbg.declare(metadata %s* %%%s, metadata !%d, metadata !DIExpression()), !dbg !%d\n",
								elemType.LLVMType(), lhsIdent.Value, varID, locID))
						}
					}
					g.symbols[lhsIdent.Value] = Symbol{Name: lhsIdent.Value, LLVMName: "%" + lhsIdent.Value, Type: elemType}
				}
				sym := g.symbols[lhsIdent.Value]

				elemReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, %d\n", elemReg, rhsType.LLVMType(), rhsReg, i))
				b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s%s\n", sym.Type.LLVMType(), elemReg, sym.Type.LLVMType(), lhsIdent.Value, dTag))
			}
		}
		return
	}

	// =========================================================================
	// 3. 単一値代入（x = y, m[k] = v, a[i] = v, s.f = v など）
	// =========================================================================
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

		// (A) インデックス代入: m[k] = v, arr[i] = v, slice[i] = v
		if lhsIndex, isLhsIndex := s.Left[0].(*ast.IndexExpr); isLhsIndex {
			baseReg, baseType := g.resolveValue(b, lhsIndex.Left)

			// --- マップ要素代入: m[k] = v ---
			if mp, isMap := baseType.(*sema.MapType); isMap {
				keyReg, keyType := g.resolveValue(b, lhsIndex.Index)
				valReg, valType := g.resolveValue(b, s.Right[0])

				keyArg := keyReg
				if keyType == sema.TypeString || strings.HasSuffix(keyType.LLVMType(), "*") {
					pInt := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", pInt, keyType.LLVMType(), keyReg))
					keyArg = pInt
				} else if keyType == sema.TypeFloat64 {
					castReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast double %s to i64\n", castReg, keyReg))
					keyArg = castReg
				}

				convVal := g.emitArgConversion(b, valReg, valType, mp.Value)
				valArg := convVal
				if mp.Value == sema.TypeString || strings.HasSuffix(mp.Value.LLVMType(), "*") {
					pInt := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", pInt, mp.Value.LLVMType(), convVal))
					valArg = pInt
				} else if mp.Value == sema.TypeFloat64 {
					castReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast double %s to i64\n", castReg, convVal))
					valArg = castReg
				} else if mp.Value == sema.TypeBool || mp.Value == sema.TypeByte {
					zextReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = zext %s %s to i64\n", zextReg, mp.Value.LLVMType(), convVal))
					valArg = zextReg
				}

				b.WriteString(fmt.Sprintf("  call void @__hike_map_set(%%struct.__hike_map* %s, i64 %s, i64 %s)%s\n", baseReg, keyArg, valArg, dTag))
				return
			}

			// --- 配列要素代入: arr[i] = v ---
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
				b.WriteString(fmt.Sprintf("  store %s %s, %s* %s%s\n", arr.Elem.LLVMType(), finalValReg, arr.Elem.LLVMType(), gepReg, dTag))
				return
			}

			// --- スライス要素代入: slice[i] = v ---
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

			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s%s\n", elemType.LLVMType(), finalValReg, elemType.LLVMType(), gepReg, dTag))
			return
		}

		// (B) 単純変数への代入: x = v
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
				b.WriteString(fmt.Sprintf("  store %s %s, %s* @%s%s\n", gType.LLVMType(), finalValReg, gType.LLVMType(), lhsIdent.Value, dTag))
				return
			}

			if call, ok := s.Right[0].(*ast.CallExpr); ok {
				if memExpr, ok := call.Function.(*ast.MemberExpr); ok && memExpr.Field.Value == "NewArena" {
					g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %%struct.Arena\n", lhsIdent.Value))
					arenaType := &sema.BasicType{Name: "Arena", LLVM: "%struct.Arena"}
					if g.debugMgr != nil && g.debugMgr.enabled {
						varID, locID := g.debugMgr.RegisterLocalVariable(lhsIdent.Value, s.Token.Line, s.Token.Col, arenaType, false, 0)
						if varID > 0 {
							g.entryAllocas.WriteString(fmt.Sprintf("  call void @llvm.dbg.declare(metadata %%struct.Arena* %%%s, metadata !%d, metadata !DIExpression()), !dbg !%d\n",
								lhsIdent.Value, varID, locID))
						}
					}
					b.WriteString(fmt.Sprintf("  call void @hike_arena_init(%%struct.Arena* %%%s, i64 65536)%s\n", lhsIdent.Value, dTag))
					g.symbols[lhsIdent.Value] = Symbol{Name: lhsIdent.Value, LLVMName: "%" + lhsIdent.Value, Type: arenaType}
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
				if g.debugMgr != nil && g.debugMgr.enabled {
					varID, locID := g.debugMgr.RegisterLocalVariable(lhsIdent.Value, s.Token.Line, s.Token.Col, targetType, false, 0)
					if varID > 0 {
						g.entryAllocas.WriteString(fmt.Sprintf("  call void @llvm.dbg.declare(metadata %s* %%%s, metadata !%d, metadata !DIExpression()), !dbg !%d\n",
							targetType.LLVMType(), lhsIdent.Value, varID, locID))
					}
				}
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

			b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s%s\n", sym.Type.LLVMType(), finalValReg, sym.Type.LLVMType(), lhsIdent.Value, dTag))
			return
		}

		// (C) 構造体フィールドへの代入: obj.field = v
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

			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s%s\n",
				fieldType.LLVMType(), finalValReg, fieldType.LLVMType(), gepReg, dTag))
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
	dTag := g.dbg(s.Token.Line, s.Token.Col)

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
			b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0%s\n", cmpReg, condType.LLVMType(), condReg, dTag))
			finalCondReg = cmpReg
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s%s\n\n", finalCondReg, bodyLabel, endLabel, dTag))
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

	if mp, isMap := xType.(*sema.MapType); isMap {
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

		if keyIdent != nil && keyIdent.Value != "_" {
			if _, exists := g.symbols[keyIdent.Value]; !exists {
				g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", keyIdent.Value, mp.Key.LLVMType()))
				g.symbols[keyIdent.Value] = Symbol{Name: keyIdent.Value, LLVMName: "%" + keyIdent.Value, Type: mp.Key}
			}
		}
		if valIdent != nil && valIdent.Value != "_" {
			if _, exists := g.symbols[valIdent.Value]; !exists {
				g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", valIdent.Value, mp.Value.LLVMType()))
				g.symbols[valIdent.Value] = Symbol{Name: valIdent.Value, LLVMName: "%" + valIdent.Value, Type: mp.Value}
			}
		}

		lblBucketCond := g.nextLabel("maprange.bcond")
		lblBucketBody := g.nextLabel("maprange.bbody")
		lblBucketPost := g.nextLabel("maprange.bpost")
		lblEntryCond := g.nextLabel("maprange.econd")
		lblEntryBody := g.nextLabel("maprange.ebody")
		lblEntryPost := g.nextLabel("maprange.epost")
		lblEnd := g.nextLabel("maprange.end")

		g.loopStack = append(g.loopStack, loopContext{breakLabel: lblEnd, continueLabel: lblEntryPost})
		defer func() {
			g.loopStack = g.loopStack[:len(g.loopStack)-1]
		}()

		pBuckets := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.__hike_map, %%struct.__hike_map* %s, i32 0, i32 0\n", pBuckets, xReg))
		buckets := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %%struct.__hike_map_entry**, %%struct.__hike_map_entry*** %s\n", buckets, pBuckets))

		pNumBuckets := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.__hike_map, %%struct.__hike_map* %s, i32 0, i32 1\n", pNumBuckets, xReg))
		numBuckets := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", numBuckets, pNumBuckets))

		bIdxAlloca := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i64\n", bIdxAlloca))
		b.WriteString(fmt.Sprintf("  store i64 0, i64* %s\n", bIdxAlloca))

		curEntryAlloca := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %%struct.__hike_map_entry*\n", curEntryAlloca))

		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblBucketCond))

		// Bucket Loop Condition
		b.WriteString(fmt.Sprintf("%s:\n", lblBucketCond))
		curBIdx := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", curBIdx, bIdxAlloca))
		cmpB := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, %s\n", cmpB, curBIdx, numBuckets))
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", cmpB, lblBucketBody, lblEnd))

		// Bucket Loop Body
		b.WriteString(fmt.Sprintf("%s:\n", lblBucketBody))
		pHead := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.__hike_map_entry*, %%struct.__hike_map_entry** %s, i64 %s\n", pHead, buckets, curBIdx))
		head := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %%struct.__hike_map_entry*, %%struct.__hike_map_entry** %s\n", head, pHead))
		b.WriteString(fmt.Sprintf("  store %%struct.__hike_map_entry* %s, %%struct.__hike_map_entry** %s\n", head, curEntryAlloca))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblEntryCond))

		// Entry Loop Condition
		b.WriteString(fmt.Sprintf("%s:\n", lblEntryCond))
		curEntry := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %%struct.__hike_map_entry*, %%struct.__hike_map_entry** %s\n", curEntry, curEntryAlloca))
		hasEntry := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp ne %%struct.__hike_map_entry* %s, null\n", hasEntry, curEntry))
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", hasEntry, lblEntryBody, lblBucketPost))

		// Entry Loop Body (User Body)
		b.WriteString(fmt.Sprintf("%s:\n", lblEntryBody))
		if keyIdent != nil && keyIdent.Value != "_" {
			pRawKey := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.__hike_map_entry, %%struct.__hike_map_entry* %s, i32 0, i32 1\n", pRawKey, curEntry))
			rawKey := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", rawKey, pRawKey))

			var finalKey string = rawKey
			if mp.Key == sema.TypeString || strings.HasSuffix(mp.Key.LLVMType(), "*") {
				castReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", castReg, rawKey, mp.Key.LLVMType()))
				finalKey = castReg
			}
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", mp.Key.LLVMType(), finalKey, mp.Key.LLVMType(), keyIdent.Value))
		}

		if valIdent != nil && valIdent.Value != "_" {
			pRawVal := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.__hike_map_entry, %%struct.__hike_map_entry* %s, i32 0, i32 2\n", pRawVal, curEntry))
			rawVal := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", rawVal, pRawVal))

			var finalVal string = rawVal
			if mp.Value == sema.TypeString || strings.HasSuffix(mp.Value.LLVMType(), "*") {
				castReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", castReg, rawVal, mp.Value.LLVMType()))
				finalVal = castReg
			} else if mp.Value == sema.TypeFloat64 {
				castReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = bitcast i64 %s to double\n", castReg, rawVal))
				finalVal = castReg
			} else if mp.Value == sema.TypeBool {
				truncReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i1\n", truncReg, rawVal))
				finalVal = truncReg
			} else if mp.Value == sema.TypeByte {
				truncReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", truncReg, rawVal))
				finalVal = truncReg
			}
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", mp.Value.LLVMType(), finalVal, mp.Value.LLVMType(), valIdent.Value))
		}

		g.emitStatement(b, s.Body, currentFn)
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblEntryPost))

		// Entry Loop Post
		b.WriteString(fmt.Sprintf("%s:\n", lblEntryPost))
		curE := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %%struct.__hike_map_entry*, %%struct.__hike_map_entry** %s\n", curE, curEntryAlloca))
		pNextE := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.__hike_map_entry, %%struct.__hike_map_entry* %s, i32 0, i32 3\n", pNextE, curE))
		nextE := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %%struct.__hike_map_entry*, %%struct.__hike_map_entry** %s\n", nextE, pNextE))
		b.WriteString(fmt.Sprintf("  store %%struct.__hike_map_entry* %s, %%struct.__hike_map_entry** %s\n", nextE, curEntryAlloca))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblEntryCond))

		// Bucket Loop Post
		b.WriteString(fmt.Sprintf("%s:\n", lblBucketPost))
		nextBIdx := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", nextBIdx, curBIdx))
		b.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", nextBIdx, bIdxAlloca))
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblBucketCond))

		b.WriteString(fmt.Sprintf("%s:\n", lblEnd))
		return
	}

	// (以降の Slice / Array / String range 走査処理はそのまま維持)
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
	dTag := g.dbg(s.Token.Line, s.Token.Col)

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

		b.WriteString(fmt.Sprintf("  ret i32 %s%s\n", retReg, dTag))
		return
	}

	fnType := g.lookupFunction(currentFn)

	if len(s.Values) == 0 {
		for i := len(g.deferStack) - 1; i >= 0; i-- {
			g.emitCallExpr(b, g.deferStack[i])
		}
		b.WriteString(fmt.Sprintf("  ret void%s\n", dTag))
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
			if strings.HasSuffix(targetTypeStr, "*") {
				b.WriteString(fmt.Sprintf("  ret %s null%s\n", targetTypeStr, dTag))
			} else {
				b.WriteString(fmt.Sprintf("  ret %s zeroinitializer%s\n", targetTypeStr, dTag))
			}
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

		b.WriteString(fmt.Sprintf("  ret %s %s%s\n", targetTypeStr, finalReg, dTag))
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

		b.WriteString(fmt.Sprintf("  ret %s %s%s\n", aggType, curAgg, dTag))
		return
	}
}

func (g *CodeGenerator) emitCallExpr(b *strings.Builder, call *ast.CallExpr) (string, sema.Type) {
	return g.emitCallInternal(b, call)
}

func (g *CodeGenerator) resolveTypeFromExpr(e ast.Expression) sema.Type {
	if e == nil {
		return sema.TypeVoid
	}
	if te, ok := e.(ast.TypeExpr); ok {
		return g.semaCtx.ResolveType(te)
	}
	if id, ok := e.(*ast.Identifier); ok {
		return g.semaCtx.ResolveType(&ast.NamedType{Token: id.Token, Name: id})
	}
	return sema.TypeVoid
}

func (g *CodeGenerator) resolveValue(b *strings.Builder, expr ast.Expression) (string, sema.Type) {
	if expr == nil {
		return "0", sema.TypeInt
	}

	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value), sema.TypeInt

	case *ast.FloatLiteral:
		s := fmt.Sprintf("%f", e.Value)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s, sema.TypeFloat64

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
		if val, ok := g.semaCtx.FloatConstants[e.Value]; ok {
			s := fmt.Sprintf("%f", val)
			if !strings.Contains(s, ".") {
				s += ".0"
			}
			return s, sema.TypeFloat64
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
		typeName := e.Type.Name.Value
		if len(e.Type.TypeArgs) > 0 {
			resolvedSt := g.semaCtx.ResolveType(e.Type)
			if st, ok := resolvedSt.(*sema.StructType); ok {
				typeName = st.Name
			}
		}

		st, structName := g.findStructByName(typeName)
		if st == nil {
			panic(fmt.Sprintf("[Codegen Error] unknown struct type %s", typeName))
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
			} else if idxExpr, ok := e.Right.(*ast.IndexExpr); ok {
				baseReg, baseType := g.resolveValue(b, idxExpr.Left)
				idxReg, _ := g.resolveValue(b, idxExpr.Index)

				if sl, isSlice := baseType.(*sema.SliceType); isSlice {
					dataPtr := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtr, sl.LLVMType(), baseReg))
					typedPtr := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedPtr, dataPtr, sl.Elem.LLVMType()))
					gepReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", gepReg, sl.Elem.LLVMType(), sl.Elem.LLVMType(), typedPtr, idxReg))
					return gepReg, &sema.PointerType{Base: sl.Elem}
				} else if arr, isArr := baseType.(*sema.ArrayType); isArr {
					arrPtrReg := baseReg
					if objIdent, isIdent := idxExpr.Left.(*ast.Identifier); isIdent {
						if sym, ok := g.symbols[objIdent.Value]; ok {
							arrPtrReg = sym.LLVMName
						}
					}
					gepReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n", gepReg, arr.LLVMType(), arr.LLVMType(), arrPtrReg, idxReg))
					return gepReg, &sema.PointerType{Base: arr.Elem}
				}
			} else if memExpr, ok := e.Right.(*ast.MemberExpr); ok {
				objPtrReg, _, st, structName := g.resolveStructPtr(b, memExpr.Object)
				if st != nil {
					gepReg, fieldType, _, found := g.resolveFieldPath(st, structName, objPtrReg, memExpr.Field.Value, b)
					if found {
						return gepReg, &sema.PointerType{Base: fieldType}
					}
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
			valReg, valType := g.resolveValue(b, e.Right)
			if valType == sema.TypeFloat64 || valType == sema.TypeFloat32 {
				negReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = fsub %s 0.0, %s\n", negReg, valType.LLVMType(), valReg))
				return negReg, valType
			}
			negReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = sub i64 0, %s\n", negReg, valReg))
			return negReg, sema.TypeInt
		}

	case *ast.MemberExpr:
		// パッケージ定数・グローバル変数の参照解決（例: calc.DefaultBase）
		if pkgIdent, ok := e.Object.(*ast.Identifier); ok {
			qualified := pkgIdent.Value + "_" + e.Field.Value
			if val, isConst := g.semaCtx.Constants[qualified]; isConst {
				return fmt.Sprintf("%d", val), sema.TypeInt
			}
			if val, isFloatConst := g.semaCtx.FloatConstants[qualified]; isFloatConst {
				s := fmt.Sprintf("%f", val)
				if !strings.Contains(s, ".") {
					s += ".0"
				}
				return s, sema.TypeFloat64
			}
			if gType, isGlobal := g.semaCtx.Globals[qualified]; isGlobal {
				resReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = load %s, %s* @%s\n", resReg, gType.LLVMType(), gType.LLVMType(), qualified))
				return resReg, gType
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

		if mp, isMap := baseType.(*sema.MapType); isMap {
			keyReg, keyType := g.resolveValue(b, e.Index)
			keyArg := keyReg
			if keyType == sema.TypeString || strings.HasSuffix(keyType.LLVMType(), "*") {
				pInt := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", pInt, keyType.LLVMType(), keyReg))
				keyArg = pInt
			} else if keyType == sema.TypeFloat64 {
				castReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = bitcast double %s to i64\n", castReg, keyReg))
				keyArg = castReg
			}

			outAlloca := g.nextReg()
			g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i64\n", outAlloca))
			b.WriteString(fmt.Sprintf("  call i1 @__hike_map_get(%%struct.__hike_map* %s, i64 %s, i64* %s)\n", baseReg, keyArg, outAlloca))

			rawValReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", rawValReg, outAlloca))

			if mp.Value == sema.TypeString || strings.HasSuffix(mp.Value.LLVMType(), "*") {
				castReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", castReg, rawValReg, mp.Value.LLVMType()))
				return castReg, mp.Value
			} else if mp.Value == sema.TypeFloat64 {
				castReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = bitcast i64 %s to double\n", castReg, rawValReg))
				return castReg, mp.Value
			} else if mp.Value == sema.TypeBool {
				truncReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i1\n", truncReg, rawValReg))
				return truncReg, mp.Value
			} else if mp.Value == sema.TypeByte {
				truncReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", truncReg, rawValReg))
				return truncReg, mp.Value
			}
			return rawValReg, mp.Value
		}
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
		// Interface vs nil の比較
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

	// 文字列連結・比較
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

	// ポインタ演算 (ptr + offset, ptr - offset)
	if ptrType, isPtr := leftType.(*sema.PointerType); isPtr && (rightType == sema.TypeInt || rightType == sema.TypeByte || rightType == nil) {
		if e.Operator == "+" {
			elemType := ptrType.Base
			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s %s, i64 %s\n", gepReg, elemType.LLVMType(), ptrType.LLVMType(), leftReg, rightReg))
			return gepReg, ptrType
		}
		if e.Operator == "-" {
			elemType := ptrType.Base
			negReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = sub i64 0, %s\n", negReg, rightReg))
			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s %s, i64 %s\n", gepReg, elemType.LLVMType(), ptrType.LLVMType(), leftReg, negReg))
			return gepReg, ptrType
		}
	}

	// 浮動小数点演算 (double または float が含まれる場合)
	isFloat64 := (leftType == sema.TypeFloat64 || rightType == sema.TypeFloat64)
	isFloat32 := (!isFloat64 && (leftType == sema.TypeFloat32 || rightType == sema.TypeFloat32))

	if isFloat64 || isFloat32 {
		targetFloatType := sema.TypeFloat64
		if isFloat32 {
			targetFloatType = sema.TypeFloat32
		}

		leftConv := g.emitArgConversion(b, leftReg, leftType, targetFloatType)
		rightConv := g.emitArgConversion(b, rightReg, rightType, targetFloatType)

		opInst := "fadd"
		isCompare := false

		switch e.Operator {
		case "+":
			opInst = "fadd"
		case "-":
			opInst = "fsub"
		case "*":
			opInst = "fmul"
		case "/":
			opInst = "fdiv"
		case "==":
			opInst = "fcmp oeq"
			isCompare = true
		case "!=":
			opInst = "fcmp one"
			isCompare = true
		case "<":
			opInst = "fcmp olt"
			isCompare = true
		case ">":
			opInst = "fcmp ogt"
			isCompare = true
		case "<=":
			opInst = "fcmp ole"
			isCompare = true
		case ">=":
			opInst = "fcmp oge"
			isCompare = true
		}

		resReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = %s %s %s, %s\n", resReg, opInst, targetFloatType.LLVMType(), leftConv, rightConv))
		if isCompare {
			return resReg, sema.TypeBool
		}
		return resReg, targetFloatType
	}

	// 整数演算
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
	case "%":
		opInst = "srem"
	case "&":
		opInst = "and"
	case "|":
		opInst = "or"
	case "^":
		opInst = "xor"
	case "<<":
		opInst = "shl"
	case ">>":
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
	// =========================================================================
	// 0. ジェネリック関数呼び出しの単相化（Monomorphization）解決
	// =========================================================================
	if idxExpr, isIdx := call.Function.(*ast.IndexExpr); isIdx {
		// 単一型引数の明示的呼び出し: Min[int](10, 20)
		if id, isId := idxExpr.Left.(*ast.Identifier); isId {
			if _, isGen := g.semaCtx.GenericFuncs[id.Value]; isGen {
				tArg := g.resolveTypeFromExpr(idxExpr.Index)
				specName := g.getOrCreateSpecializedFunc(id.Value, []sema.Type{tArg})
				call.Function = &ast.Identifier{Token: id.Token, Value: specName}
			}
		}
	} else if genExpr, isGenExpr := call.Function.(*ast.GenericInstExpr); isGenExpr {
		// 複数型引数の明示的呼び出し: Pair[int, string](...)
		if id, isId := genExpr.Left.(*ast.Identifier); isId {
			if _, isGen := g.semaCtx.GenericFuncs[id.Value]; isGen {
				var resArgs []sema.Type
				for _, tArg := range genExpr.TypeArgs {
					resArgs = append(resArgs, g.semaCtx.ResolveType(tArg))
				}
				specName := g.getOrCreateSpecializedFunc(id.Value, resArgs)
				call.Function = &ast.Identifier{Token: id.Token, Value: specName}
			}
		}
	} else if id, isId := call.Function.(*ast.Identifier); isId {
		// 引数からの型推論付き呼び出し: Identity(999) -> Identity__int(999)
		if genTemplate, isGen := g.semaCtx.GenericFuncs[id.Value]; isGen {
			var infTypes []sema.Type
			for i, arg := range call.Args {
				if i < len(genTemplate.Params) {
					_, aType := g.resolveValue(b, arg)
					infTypes = append(infTypes, aType)
				}
			}
			specName := g.getOrCreateSpecializedFunc(id.Value, infTypes)
			call.Function = &ast.Identifier{Token: id.Token, Value: specName}
		}
	}

	// =========================================================================
	// 1. make / delete / len / append / 型キャスト等の組み込み処理
	// =========================================================================

	// make(map[K]V, [cap])
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "make" && len(call.Args) >= 1 {
		if mapTypeNode, okMap := call.Args[0].(*ast.MapType); okMap {
			kType := g.semaCtx.ResolveType(mapTypeNode.Key)
			vType := g.semaCtx.ResolveType(mapTypeNode.Value)
			resMapType := &sema.MapType{Key: kType, Value: vType}

			isStr := 0
			if kType == sema.TypeString || (kType != nil && kType.TypeName() == "string") {
				isStr = 1
			}

			capReg := "16"
			if len(call.Args) >= 2 {
				capReg, _ = g.resolveValue(b, call.Args[1])
			}

			callReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = call %%struct.__hike_map* @__hike_map_create(i64 %s, i64 %d)\n", callReg, capReg, isStr))
			return callReg, resMapType
		}
	}

	// delete(m, key)
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "delete" && len(call.Args) == 2 {
		mReg, _ := g.resolveValue(b, call.Args[0])
		keyReg, keyType := g.resolveValue(b, call.Args[1])

		keyArg := keyReg
		if keyType == sema.TypeString || strings.HasSuffix(keyType.LLVMType(), "*") {
			pInt := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", pInt, keyType.LLVMType(), keyReg))
			keyArg = pInt
		} else if keyType == sema.TypeFloat64 {
			castReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast double %s to i64\n", castReg, keyReg))
			keyArg = castReg
		}

		b.WriteString(fmt.Sprintf("  call void @__hike_map_delete(%%struct.__hike_map* %s, i64 %s)\n", mReg, keyArg))
		return "", sema.TypeVoid
	}

	// make([]T, len, [cap])
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "make" && len(call.Args) >= 2 {
		var elemType sema.Type = sema.TypeByte
		if slType, okSlice := call.Args[0].(*ast.SliceType); okSlice {
			elemType = g.semaCtx.ResolveType(slType.Elem)
		}

		lenReg, _ := g.resolveValue(b, call.Args[1])
		capReg := lenReg
		if len(call.Args) >= 3 {
			capReg, _ = g.resolveValue(b, call.Args[2])
		}

		elemSize := elemType.Size()
		if elemSize <= 0 {
			elemSize = 1
		}

		callocReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call i8* @calloc(i64 %s, i64 %d)\n", callocReg, capReg, elemSize))

		resSliceType := &sema.SliceType{Elem: elemType}
		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, resSliceType.LLVMType(), callocReg))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 1\n", t2, resSliceType.LLVMType(), t1, lenReg))
		t3 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 2\n", t3, resSliceType.LLVMType(), t2, capReg))

		return t3, resSliceType
	}

	// []byte(msg) キャスト
	if slType, ok := call.Function.(*ast.SliceType); ok && len(call.Args) == 1 {
		elemType := g.semaCtx.ResolveType(slType.Elem)
		argReg, _ := g.resolveValue(b, call.Args[0])

		resSliceType := &sema.SliceType{Elem: elemType}
		lenReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", lenReg, argReg))

		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, resSliceType.LLVMType(), argReg))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 1\n", t2, resSliceType.LLVMType(), t1, lenReg))
		t3 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 2\n", t3, resSliceType.LLVMType(), t2, lenReg))

		return t3, resSliceType
	}

	// string(buf[0:n]) キャスト
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "string" && len(call.Args) == 1 {
		argReg, argType := g.resolveValue(b, call.Args[0])
		if sl, isSlice := argType.(*sema.SliceType); isSlice {
			dataPtr := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtr, sl.LLVMType(), argReg))
			return dataPtr, sema.TypeString
		}
		return argReg, sema.TypeString
	}

	// byte(val) キャスト
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "byte" && len(call.Args) == 1 {
		argReg, argType := g.resolveValue(b, call.Args[0])
		convReg := g.emitArgConversion(b, argReg, argType, sema.TypeByte)
		return convReg, sema.TypeByte
	}

	// float64(x) / float(x) キャスト
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && (fnIdent.Value == "float64" || fnIdent.Value == "float") && len(call.Args) == 1 {
		argReg, argType := g.resolveValue(b, call.Args[0])
		convReg := g.emitArgConversion(b, argReg, argType, sema.TypeFloat64)
		return convReg, sema.TypeFloat64
	}

	// float32(x) キャスト
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "float32" && len(call.Args) == 1 {
		argReg, argType := g.resolveValue(b, call.Args[0])
		convReg := g.emitArgConversion(b, argReg, argType, sema.TypeFloat32)
		return convReg, sema.TypeFloat32
	}

	// int(x) キャスト
	if fnIdent, ok := call.Function.(*ast.Identifier); ok && fnIdent.Value == "int" && len(call.Args) == 1 {
		argReg, argType := g.resolveValue(b, call.Args[0])
		convReg := g.emitArgConversion(b, argReg, argType, sema.TypeInt)
		return convReg, sema.TypeInt
	}

	// append(slice, elems...)
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

	// append(slice, val1, val2, ...)
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

	// len / cap
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
			if _, isMap := argType.(*sema.MapType); isMap {
				lenReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = call i64 @__hike_map_len(%%struct.__hike_map* %s)\n", lenReg, argReg))
				return lenReg, sema.TypeInt
			}
		}
	}

	// *T(...) ポインタ型キャスト
	if pref, ok := call.Function.(*ast.PrefixExpr); ok && pref.Operator == "*" {
		var typeName string
		var isSlice bool
		var sliceElemType sema.Type

		if ident, okId := pref.Right.(*ast.Identifier); okId {
			typeName = ident.Value
		} else if mem, okMem := pref.Right.(*ast.MemberExpr); okMem {
			typeName = mem.Field.Value
		} else if sl, okSl := pref.Right.(*ast.SliceType); okSl {
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

	// =========================================================================
	// 2. 構造体メソッド / インターフェース / パッケージ修飾関数呼び出し
	// =========================================================================
	if memExpr, ok := call.Function.(*ast.MemberExpr); ok {
		isVariable := false
		if objIdent, okObj := memExpr.Object.(*ast.Identifier); okObj {
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

	// =========================================================================
	// 3. 通常のトップレベル関数呼び出し
	// =========================================================================
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

				dTag := g.dbg(call.Token.Line, call.Token.Col)

				if retType == "void" {
					if len(sigParamTypes) > 0 {
						b.WriteString(fmt.Sprintf("  call void (%s) @%s(%s)%s\n", strings.Join(sigParamTypes, ", "), emitFnName, strings.Join(args, ", "), dTag))
					} else {
						b.WriteString(fmt.Sprintf("  call void @%s(%s)%s\n", emitFnName, strings.Join(args, ", "), dTag))
					}
					return "", sema.TypeVoid
				}

				callReg := g.nextReg()
				if len(sigParamTypes) > 0 {
					b.WriteString(fmt.Sprintf("  %s = call %s (%s) @%s(%s)%s\n", callReg, retType, strings.Join(sigParamTypes, ", "), emitFnName, strings.Join(args, ", "), dTag))
				} else {
					b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)%s\n", callReg, retType, emitFnName, strings.Join(args, ", "), dTag))
				}
				return callReg, semaRet
			}
		}
	}

	// =========================================================================
	// 4. 関数ポインタ / クロージャ値の呼び出し
	// =========================================================================
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
