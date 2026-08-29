package codegen

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/sema"
)

type Symbol struct {
	Name     string
	LLVMName string
	Type     sema.Type
}

type StringConst struct {
	Label  string
	Value  string
	Length int
}

type loopContext struct {
	breakLabel    string
	continueLabel string
}

// 1. 構造体定義
type CodeGenerator struct {
	prog           *ast.Program
	semaCtx        *sema.Context
	output         strings.Builder
	regCount       int
	labelCount     int
	stringLiterals []StringConst
	symbols        map[string]Symbol
	deferStack     []*ast.CallExpr // 修正: *ast.CallExpr のスライス
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
		deferStack:     []*ast.CallExpr{}, // 初期化
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

func escapeLLVMString(str string) (string, int) {
	var raw []byte
	for i := 0; i < len(str); i++ {
		if str[i] == '\\' && i+1 < len(str) {
			switch str[i+1] {
			case 'n':
				raw = append(raw, '\n')
				i++
			case 't':
				raw = append(raw, '\t')
				i++
			case 'r':
				raw = append(raw, '\r')
				i++
			case '"':
				raw = append(raw, '"')
				i++
			case '\\':
				raw = append(raw, '\\')
				i++
			case '0':
				raw = append(raw, 0)
				i++
			default:
				raw = append(raw, str[i])
			}
		} else {
			raw = append(raw, str[i])
		}
	}

	length := len(raw) + 1

	var sb strings.Builder
	for _, b := range raw {
		if b >= 32 && b <= 126 && b != '"' && b != '\\' {
			sb.WriteByte(b)
		} else {
			sb.WriteString(fmt.Sprintf("\\%02X", b))
		}
	}
	sb.WriteString("\\00")

	return sb.String(), length
}

func (g *CodeGenerator) addStringLiteral(str string) (string, int) {
	escapedVal, length := escapeLLVMString(str)
	label := fmt.Sprintf("@.str.%d", len(g.stringLiterals)+1)
	g.stringLiterals = append(g.stringLiterals, StringConst{
		Label:  label,
		Value:  escapedVal,
		Length: length,
	})
	return label, length
}

func (g *CodeGenerator) Generate() string {
	var bodyBuilder strings.Builder

	for _, decl := range g.prog.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Body != nil {
			g.emitFuncDecl(&bodyBuilder, fnDecl)
		}
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
	g.output.WriteString("declare i64 @printf(i8*, ...)\n") // 追加

	// 1. emitPrologue にグローバル変数定義を追加
	for name, gType := range g.semaCtx.Globals {
		g.output.WriteString(fmt.Sprintf("@%s = global %s 0, align 8\n", name, gType.LLVMType()))
	}
	g.output.WriteString("\n")

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
	g.output.WriteString("\n")

	// hike_streq
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

	// hike_substr
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

	// hike_strcat
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
		if exists && i < len(fnMeta.ParamTypes) {
			pType = fnMeta.ParamTypes[i]
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
		if exists && i < len(fnMeta.ParamTypes) {
			pType = fnMeta.ParamTypes[i]
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

	// 末尾到達時のフォールバック (明示的な return がない場合)
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

func (g *CodeGenerator) emitStatement(b *strings.Builder, stmt ast.Statement, currentFn string) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if call, ok := s.Expr.(*ast.CallExpr); ok {
			g.emitCallExpr(b, call)
		}
	case *ast.DeferStmt:
		if s.Call != nil {
			g.deferStack = append(g.deferStack, s.Call)
		}
	case *ast.AssignStmt:
		g.emitAssignStmt(b, s)
	case *ast.IfStmt:
		g.emitIfStmt(b, s, currentFn)
	case *ast.ForStmt:
		g.emitForStmt(b, s, currentFn)
	case *ast.ForRangeStmt:
		g.emitForRangeStmt(b, s, currentFn)
	case *ast.SwitchStmt:
		g.emitSwitchStmt(b, s, currentFn)
	case *ast.ReturnStmt:
		g.emitReturnStmt(b, s, currentFn)
	case *ast.BreakStmt:
		if len(g.loopStack) > 0 {
			top := g.loopStack[len(g.loopStack)-1]
			b.WriteString(fmt.Sprintf("  br label %%%s\n\n", top.breakLabel))
		}
	case *ast.ContinueStmt:
		if len(g.loopStack) > 0 {
			top := g.loopStack[len(g.loopStack)-1]
			b.WriteString(fmt.Sprintf("  br label %%%s\n\n", top.continueLabel))
		}
	}
}

func (g *CodeGenerator) emitForStmt(b *strings.Builder, s *ast.ForStmt, currentFn string) {
	lblCond := g.nextLabel("for.cond")
	lblBody := g.nextLabel("for.body")
	lblPost := g.nextLabel("for.post")
	lblEnd := g.nextLabel("for.end")

	if s.Init != nil {
		g.emitStatement(b, s.Init, currentFn)
	}

	contLbl := lblPost
	if s.Post == nil {
		contLbl = lblCond
	}
	g.loopStack = append(g.loopStack, loopContext{breakLabel: lblEnd, continueLabel: contLbl})
	defer func() {
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
	}()

	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCond))

	b.WriteString(fmt.Sprintf("%s:\n", lblCond))
	if s.Cond != nil {
		condReg, condType := g.resolveValue(b, s.Cond)
		condBool := condReg
		if condType != nil && condType.LLVMType() != "i1" {
			tStr := condType.LLVMType()
			cmpVal := "0"
			if strings.HasSuffix(tStr, "*") {
				cmpVal = "null"
			}
			boolReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, %s\n", boolReg, tStr, condReg, cmpVal))
			condBool = boolReg
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", condBool, lblBody, lblEnd))
	} else {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblBody))
	}

	b.WriteString(fmt.Sprintf("%s:\n", lblBody))
	if s.Body != nil {
		for _, stmt := range s.Body.Statements {
			g.emitStatement(b, stmt, currentFn)
		}
	}

	if s.Post != nil {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblPost))
		b.WriteString(fmt.Sprintf("%s:\n", lblPost))
		g.emitStatement(b, s.Post, currentFn)
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCond))
	} else {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCond))
	}

	b.WriteString(fmt.Sprintf("%s:\n", lblEnd))
}

func (g *CodeGenerator) emitForRangeStmt(b *strings.Builder, s *ast.ForRangeStmt, currentFn string) {
	sliceReg, sliceType := g.resolveValue(b, s.X)
	sl, isSlice := sliceType.(*sema.SliceType)
	if !isSlice {
		panic("[Codegen Error] range expression must be a slice")
	}

	elemType := sl.Elem

	sPtr := g.nextReg()
	sLen := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", sPtr, sl.LLVMType(), sliceReg))
	b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", sLen, sl.LLVMType(), sliceReg))

	idxAlloca := g.nextReg()
	g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca i64\n", idxAlloca))
	b.WriteString(fmt.Sprintf("  store i64 0, i64* %s\n", idxAlloca))

	var keySymName string
	if s.Key != nil {
		if kIdent, ok := s.Key.(*ast.Identifier); ok && kIdent.Value != "_" {
			if _, exists := g.symbols[kIdent.Value]; !exists {
				g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca i64\n", kIdent.Value))
				g.symbols[kIdent.Value] = Symbol{Name: kIdent.Value, LLVMName: "%" + kIdent.Value, Type: sema.TypeInt}
			}
			keySymName = "%" + kIdent.Value
		}
	}

	var valSymName string
	if s.Value != nil {
		if vIdent, ok := s.Value.(*ast.Identifier); ok && vIdent.Value != "_" {
			if _, exists := g.symbols[vIdent.Value]; !exists {
				g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", vIdent.Value, elemType.LLVMType()))
				g.symbols[vIdent.Value] = Symbol{Name: vIdent.Value, LLVMName: "%" + vIdent.Value, Type: elemType}
			}
			valSymName = "%" + vIdent.Value
		}
	}

	lblCond := g.nextLabel("range.cond")
	lblBody := g.nextLabel("range.body")
	lblPost := g.nextLabel("range.post")
	lblEnd := g.nextLabel("range.end")

	g.loopStack = append(g.loopStack, loopContext{breakLabel: lblEnd, continueLabel: lblPost})
	defer func() {
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
	}()

	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCond))

	b.WriteString(fmt.Sprintf("%s:\n", lblCond))
	curIdxReg := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", curIdxReg, idxAlloca))
	cmpReg := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, %s\n", cmpReg, curIdxReg, sLen))
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", cmpReg, lblBody, lblEnd))

	b.WriteString(fmt.Sprintf("%s:\n", lblBody))

	if keySymName != "" {
		b.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", curIdxReg, keySymName))
	}

	if valSymName != "" {
		typedPtr := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedPtr, sPtr, elemType.LLVMType()))
		elemPtr := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", elemPtr, elemType.LLVMType(), elemType.LLVMType(), typedPtr, curIdxReg))
		elemVal := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", elemVal, elemType.LLVMType(), elemType.LLVMType(), elemPtr))
		b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", elemType.LLVMType(), elemVal, elemType.LLVMType(), valSymName))
	}

	if s.Body != nil {
		for _, stmt := range s.Body.Statements {
			g.emitStatement(b, stmt, currentFn)
		}
	}

	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblPost))

	b.WriteString(fmt.Sprintf("%s:\n", lblPost))
	postIdx := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = load i64, i64* %s\n", postIdx, idxAlloca))
	nextIdx := g.nextReg()
	b.WriteString(fmt.Sprintf("  %s = add i64 %s, 1\n", nextIdx, postIdx))
	b.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", nextIdx, idxAlloca))
	b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblCond))

	b.WriteString(fmt.Sprintf("%s:\n", lblEnd))
}

func (g *CodeGenerator) emitSwitchStmt(b *strings.Builder, s *ast.SwitchStmt, currentFn string) {
	if s.Init != nil {
		g.emitStatement(b, s.Init, currentFn)
	}

	valReg, _ := g.resolveValue(b, s.Value)

	lblEnd := g.nextLabel("switch.end")
	lblDefault := lblEnd

	type caseTarget struct {
		valReg string
		label  string
		body   []ast.Statement
	}

	var targets []caseTarget
	var defaultBody []ast.Statement

	for _, c := range s.Cases {
		lblCase := g.nextLabel("switch.case")
		if len(c.Values) == 0 {
			lblDefault = lblCase
			defaultBody = c.Body
		} else {
			for _, v := range c.Values {
				cValReg, _ := g.resolveValue(b, v)
				targets = append(targets, caseTarget{
					valReg: cValReg,
					label:  lblCase,
					body:   c.Body,
				})
			}
		}
	}

	b.WriteString(fmt.Sprintf("  switch i64 %s, label %%%s [\n", valReg, lblDefault))
	for _, t := range targets {
		b.WriteString(fmt.Sprintf("    i64 %s, label %%%s\n", t.valReg, t.label))
	}
	b.WriteString("  ]\n\n")

	emittedLabels := make(map[string]bool)

	for _, t := range targets {
		if emittedLabels[t.label] {
			continue
		}
		emittedLabels[t.label] = true

		b.WriteString(fmt.Sprintf("%s:\n", t.label))
		hasTerminator := false
		for _, stmt := range t.body {
			if _, ok := stmt.(*ast.ReturnStmt); ok {
				hasTerminator = true
			}
			if _, ok := stmt.(*ast.BreakStmt); ok {
				hasTerminator = true
			}
			if _, ok := stmt.(*ast.ContinueStmt); ok {
				hasTerminator = true
			}
			g.emitStatement(b, stmt, currentFn)
		}
		if !hasTerminator {
			b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblEnd))
		}
	}

	if len(defaultBody) > 0 && !emittedLabels[lblDefault] {
		b.WriteString(fmt.Sprintf("%s:\n", lblDefault))
		hasTerminator := false
		for _, stmt := range defaultBody {
			if _, ok := stmt.(*ast.ReturnStmt); ok {
				hasTerminator = true
			}
			if _, ok := stmt.(*ast.BreakStmt); ok {
				hasTerminator = true
			}
			if _, ok := stmt.(*ast.ContinueStmt); ok {
				hasTerminator = true
			}
			g.emitStatement(b, stmt, currentFn)
		}
		if !hasTerminator {
			b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblEnd))
		}
	}

	b.WriteString(fmt.Sprintf("%s:\n", lblEnd))
}

func (g *CodeGenerator) findStructByName(name string) (*sema.StructType, string) {
	if st, ok := g.semaCtx.Structs[name]; ok {
		return st, st.Name
	}
	for sName, st := range g.semaCtx.Structs {
		if strings.HasSuffix(sName, "_"+name) || strings.HasSuffix(sName, name) {
			return st, st.Name
		}
	}
	return nil, ""
}

func (g *CodeGenerator) findStruct(t sema.Type, fieldName string) (*sema.StructType, string) {
	if t != nil {
		if st, ok := t.(*sema.StructType); ok {
			return st, st.Name
		}
		if ptr, ok := t.(*sema.PointerType); ok {
			if st, ok := ptr.Base.(*sema.StructType); ok {
				return st, st.Name
			}
			if basic, ok := ptr.Base.(*sema.BasicType); ok {
				if st, name := g.findStructByName(basic.Name); st != nil {
					return st, name
				}
			}
		}
		llvmStr := t.LLVMType()
		cleanName := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(llvmStr, "*", ""), "%struct.", ""))
		if st, name := g.findStructByName(cleanName); st != nil {
			return st, name
		}
	}

	if fieldName != "" {
		for _, st := range g.semaCtx.Structs {
			for _, f := range st.Fields {
				if f.Name == fieldName {
					return st, st.Name
				}
			}
		}
	}

	return nil, ""
}

func (g *CodeGenerator) emitAssignStmt(b *strings.Builder, s *ast.AssignStmt) {
	// A. 多値返り値 / 型アサーションのアンパック代入: a, b := fn() または val, ok := obj.(int)
	if len(s.Left) > 1 && len(s.Right) == 1 {
		rhsReg, rhsType := g.resolveValue(b, s.Right[0])

		var tupleElemTypes []sema.Type
		if tup, ok := rhsType.(*sema.TupleType); ok {
			tupleElemTypes = tup.Types
		}

		for i, leftExpr := range s.Left {
			if lhsIdent, ok := leftExpr.(*ast.Identifier); ok {
				if lhsIdent.Value == "_" {
					continue // ブランク識別子は代入をスキップ
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

	// B. 単一変数の代入 / 複合代入
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

		// 1. スライスまたは固定長配列の要素への代入 (s[i] = val / arr[i] = val)
		if lhsIndex, isLhsIndex := s.Left[0].(*ast.IndexExpr); isLhsIndex {
			baseReg, baseType := g.resolveValue(b, lhsIndex.Left)
			idxReg, _ := g.resolveValue(b, lhsIndex.Index)
			valReg, valType := g.resolveValue(b, s.Right[0])

			// 固定長配列 [N]T への要素代入 (arr[i] = val)
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

			// スライス []T への要素代入
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

		// 2. emitAssignStmt の単一変数代入の先頭でグローバル変数を判定
		// 2. 変数への代入 (x = val / var x any = val)
		if lhsIdent, isLhsIdent := s.Left[0].(*ast.Identifier); isLhsIdent {
			// グローバル変数への代入 (追加)
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
					if valType != nil && valType.LLVMType() != gType.LLVMType() {
						convReg := g.nextReg()
						if strings.HasSuffix(gType.LLVMType(), "*") && strings.HasSuffix(valType.LLVMType(), "*") {
							b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, gType.LLVMType()))
							finalValReg = convReg
						}
					}
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

			// 宣言型（var x any = ... 等）がある場合はその型を採用
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

			finalValReg := valReg

			// インターフェース型（any / interface{}）への自動ボクシング
			if _, isIface := targetType.(*sema.InterfaceType); isIface {
				if _, srcIsIface := valType.(*sema.InterfaceType); !srcIsIface {
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
					finalValReg = t2
				}
			}

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

		// 3. 構造体フィールドへの代入 (s.Field = val / s.Field += val)
		if lhsMember, isLhsMember := s.Left[0].(*ast.MemberExpr); isLhsMember {
			objReg, objType := g.resolveValue(b, lhsMember.Object)
			st, structName := g.findStruct(objType, lhsMember.Field.Value)
			if st == nil {
				panic(fmt.Sprintf("[Codegen Error] cannot assign to field %s on non-struct type %v", lhsMember.Field.Value, objType))
			}

			fieldIdx := -1
			var fieldType sema.Type
			for i, f := range st.Fields {
				if f.Name == lhsMember.Field.Value {
					fieldIdx = i
					fieldType = f.Type
					break
				}
			}
			if fieldIdx == -1 {
				panic(fmt.Sprintf("[Codegen Error] unknown field %s in struct %s", lhsMember.Field.Value, structName))
			}

			valReg, valType := g.resolveValue(b, s.Right[0])

			objPtrReg := objReg
			if objIdent, isIdent := lhsMember.Object.(*ast.Identifier); isIdent {
				if sym, ok := g.symbols[objIdent.Value]; ok && !strings.HasSuffix(sym.Type.LLVMType(), "*") {
					objPtrReg = sym.LLVMName
				}
			}

			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
				gepReg, structName, structName, objPtrReg, fieldIdx))

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
				if strings.HasSuffix(fieldType.LLVMType(), "*") {
					if valReg == "0" || valReg == "null" || valReg == "" {
						finalValReg = "null"
					} else if valType != nil && !strings.HasSuffix(valType.LLVMType(), "*") {
						convReg := g.nextReg()
						b.WriteString(fmt.Sprintf("  %s = inttoptr %s %s to %s\n", convReg, valType.LLVMType(), valReg, fieldType.LLVMType()))
						finalValReg = convReg
					} else if valType != nil && valType.LLVMType() != fieldType.LLVMType() {
						convReg := g.nextReg()
						b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, fieldType.LLVMType()))
						finalValReg = convReg
					}
				}
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
	g.emitIfStmtWithEnd(b, s, currentFn, "")
}

func (g *CodeGenerator) emitIfStmtWithEnd(b *strings.Builder, s *ast.IfStmt, currentFn string, outerEndLabel string) {
	condReg, condType := g.resolveValue(b, s.Condition)
	condBool := condReg
	if condType != nil && condType.LLVMType() != "i1" {
		tStr := condType.LLVMType()
		cmpVal := "0"
		if strings.HasSuffix(tStr, "*") {
			cmpVal = "null"
		}
		boolReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, %s\n", boolReg, tStr, condReg, cmpVal))
		condBool = boolReg
	}

	lblThen := g.nextLabel("if.then")
	lblElse := ""
	if s.Alternative != nil {
		lblElse = g.nextLabel("if.else")
	}

	lblEnd := outerEndLabel
	isOuter := false
	if lblEnd == "" {
		lblEnd = g.nextLabel("if.end")
		isOuter = true
	}

	if s.Alternative != nil {
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", condBool, lblThen, lblElse))
	} else {
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", condBool, lblThen, lblEnd))
	}

	b.WriteString(fmt.Sprintf("%s:\n", lblThen))
	hasTerminatorThen := false
	for _, stmt := range s.Consequence.Statements {
		if _, ok := stmt.(*ast.ReturnStmt); ok {
			hasTerminatorThen = true
		}
		if _, ok := stmt.(*ast.BreakStmt); ok {
			hasTerminatorThen = true
		}
		if _, ok := stmt.(*ast.ContinueStmt); ok {
			hasTerminatorThen = true
		}
		g.emitStatement(b, stmt, currentFn)
	}
	if !hasTerminatorThen {
		b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblEnd))
	}

	if s.Alternative != nil {
		b.WriteString(fmt.Sprintf("%s:\n", lblElse))
		switch alt := s.Alternative.(type) {
		case *ast.BlockStmt:
			hasTerminatorElse := false
			for _, stmt := range alt.Statements {
				if _, ok := stmt.(*ast.ReturnStmt); ok {
					hasTerminatorElse = true
				}
				if _, ok := stmt.(*ast.BreakStmt); ok {
					hasTerminatorElse = true
				}
				if _, ok := stmt.(*ast.ContinueStmt); ok {
					hasTerminatorElse = true
				}
				g.emitStatement(b, stmt, currentFn)
			}
			if !hasTerminatorElse {
				b.WriteString(fmt.Sprintf("  br label %%%s\n\n", lblEnd))
			}
		case *ast.IfStmt:
			g.emitIfStmtWithEnd(b, alt, currentFn, lblEnd)
		}
	}

	if isOuter {
		b.WriteString(fmt.Sprintf("%s:\n", lblEnd))
	}
}

// 4. emitReturnStmt: 戻り値の確定 -> defer の逆順インライン展開 -> ret
func (g *CodeGenerator) emitReturnStmt(b *strings.Builder, s *ast.ReturnStmt, currentFn string) {
	// 1. main関数の場合
	if currentFn == "main" {
		var retReg string = "0"
		if len(s.Values) == 1 {
			valReg, valType := g.resolveValue(b, s.Values[0])
			truncReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = trunc %s %s to i32\n", truncReg, valType.LLVMType(), valReg))
			retReg = truncReg
		}

		// defer 実行
		for i := len(g.deferStack) - 1; i >= 0; i-- {
			g.emitCallExpr(b, g.deferStack[i])
		}

		b.WriteString(fmt.Sprintf("  ret i32 %s\n", retReg))
		return
	}

	fnType := g.lookupFunction(currentFn)

	// 2. 戻り値なし (void)
	if len(s.Values) == 0 {
		for i := len(g.deferStack) - 1; i >= 0; i-- {
			g.emitCallExpr(b, g.deferStack[i])
		}
		b.WriteString("  ret void\n")
		return
	}

	// 3. 単一戻り値
	if len(s.Values) == 1 {
		valReg, valType := g.resolveValue(b, s.Values[0])

		targetTypeStr := valType.LLVMType()
		if fnType != nil && len(fnType.ReturnTypes) == 1 {
			targetTypeStr = fnType.ReturnTypes[0].LLVMType()
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

		finalReg := valReg
		if valType != nil && valType.LLVMType() != targetTypeStr {
			if targetTypeStr == "i64" && valType.LLVMType() == "i1" {
				zextReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", zextReg, valReg))
				finalReg = zextReg
			} else if targetTypeStr == "i1" && valType.LLVMType() == "i64" {
				cmpReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = icmp ne i64 %s, 0\n", cmpReg, valReg))
				finalReg = cmpReg
			} else if strings.HasSuffix(targetTypeStr, "*") && strings.HasSuffix(valType.LLVMType(), "*") {
				convReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, targetTypeStr))
				finalReg = convReg
			}
		}

		// defer 実行
		for i := len(g.deferStack) - 1; i >= 0; i-- {
			g.emitCallExpr(b, g.deferStack[i])
		}

		b.WriteString(fmt.Sprintf("  ret %s %s\n", targetTypeStr, finalReg))
		return
	}

	// 4. 複数戻り値
	if len(s.Values) > 1 {
		types := []string{}
		valRegs := []string{}
		for _, v := range s.Values {
			vReg, vType := g.resolveValue(b, v)
			types = append(types, vType.LLVMType())
			valRegs = append(valRegs, vReg)
		}
		aggType := fmt.Sprintf("{ %s }", strings.Join(types, ", "))
		curAgg := "undef"
		for i, vReg := range valRegs {
			nextAgg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, %d\n", nextAgg, aggType, curAgg, types[i], vReg, i))
			curAgg = nextAgg
		}

		// defer 実行
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
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value), sema.TypeInt

	case *ast.IotaExpr:
		return fmt.Sprintf("%d", e.Value), sema.TypeInt

	case *ast.NilLiteral:
		return "null", &sema.PointerType{Base: sema.TypeByte}

	case *ast.StringLiteral:
		label, length := g.addStringLiteral(e.Value)
		ptrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0\n", ptrReg, length, length, label))
		return ptrReg, sema.TypeString

	// resolveValue 内の PrefixExpr (&StructLiteral{...}) の追加
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
				st, structName := g.findStructByName(stLit.Type.Name.Value)
				if st == nil {
					panic(fmt.Sprintf("[Codegen Error] unknown struct type %s", stLit.Type.Name.Value))
				}

				allocaReg := g.nextReg()
				g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %%struct.%s\n", allocaReg, structName))

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
						gepReg, structName, structName, allocaReg, fieldIdx))

					valToStore := valReg
					if fieldType != nil && valType != nil && valType.LLVMType() != fieldType.LLVMType() {
						convReg := g.nextReg()
						if strings.HasSuffix(fieldType.LLVMType(), "*") && strings.HasSuffix(valType.LLVMType(), "*") {
							b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, fieldType.LLVMType()))
							valToStore = convReg
						}
					}

					b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldType.LLVMType(), valToStore, fieldType.LLVMType(), gepReg))
				}
				return allocaReg, &sema.PointerType{Base: st}
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

		// グローバル変数の参照 (追加)
		if gType, exists := g.semaCtx.Globals[e.Value]; exists {
			loadReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = load %s, %s* @%s\n", loadReg, gType.LLVMType(), gType.LLVMType(), e.Value))
			return loadReg, gType
		}

		return "0", sema.TypeInt

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

			valToStore := valReg
			if fieldType != nil && valType != nil && valType.LLVMType() != fieldType.LLVMType() {
				convReg := g.nextReg()
				if strings.HasSuffix(fieldType.LLVMType(), "*") && strings.HasSuffix(valType.LLVMType(), "*") {
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, fieldType.LLVMType()))
					valToStore = convReg
				}
			}

			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", fieldType.LLVMType(), valToStore, fieldType.LLVMType(), gepReg))
		}

		loadValReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %%struct.%s, %%struct.%s* %s\n", loadValReg, structName, structName, allocaReg))
		return loadValReg, st

	// 1. resolveValue 内に ArrayLiteral, TypeAssertExpr を追加
	case *ast.ArrayLiteral:
		arrType := g.semaCtx.ResolveType(e.Type).(*sema.ArrayType)
		allocaReg := g.nextReg()
		g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", allocaReg, arrType.LLVMType()))

		for i, elem := range e.Elements {
			valReg, valType := g.resolveValue(b, elem)
			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %d\n",
				gepReg, arrType.LLVMType(), arrType.LLVMType(), allocaReg, i))

			valToStore := valReg
			if valType != nil && valType.LLVMType() != arrType.Elem.LLVMType() {
				convReg := g.nextReg()
				if arrType.Elem.LLVMType() == "i8" && valType.LLVMType() == "i64" {
					b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", convReg, valReg))
					valToStore = convReg
				} else if arrType.Elem.LLVMType() == "i64" && valType.LLVMType() == "i8" {
					b.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", convReg, valReg))
					valToStore = convReg
				}
			}
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", arrType.Elem.LLVMType(), valToStore, arrType.Elem.LLVMType(), gepReg))
		}
		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, arrType.LLVMType(), arrType.LLVMType(), allocaReg))
		return loadReg, arrType

	case *ast.TypeAssertExpr:
		ifaceReg, ifaceType := g.resolveValue(b, e.Expr)
		targetType := g.semaCtx.ResolveType(e.Target)
		targetID := g.semaCtx.GetTypeID(targetType)

		dataPtr := g.nextReg()
		typeIDReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", dataPtr, ifaceType.LLVMType(), ifaceReg))
		b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", typeIDReg, ifaceType.LLVMType(), ifaceReg))

		matchReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i64 %s, %d\n", matchReg, typeIDReg, targetID))

		castReg := g.nextReg()
		if strings.HasSuffix(targetType.LLVMType(), "*") {
			b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s\n", castReg, dataPtr, targetType.LLVMType()))
		} else if targetType.LLVMType() == "i64" {
			b.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", castReg, dataPtr))
		} else if targetType.LLVMType() == "i1" {
			pInt := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = ptrtoint i8* %s to i64\n", pInt, dataPtr))
			b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i1\n", castReg, pInt))
		} else {
			typedPtr := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedPtr, dataPtr, targetType.LLVMType()))
			b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", castReg, targetType.LLVMType(), targetType.LLVMType(), typedPtr))
		}

		// (targetValue, ok) のタプル型として返却
		tupT := &sema.TupleType{Types: []sema.Type{targetType, sema.TypeBool}}
		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, %s %s, 0\n", t1, tupT.LLVMType(), targetType.LLVMType(), castReg))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i1 %s, 1\n", t2, tupT.LLVMType(), t1, matchReg))

		return t2, tupT

	case *ast.MemberExpr:
		if pkgIdent, isIdent := e.Object.(*ast.Identifier); isIdent {
			if _, isVar := g.symbols[pkgIdent.Value]; !isVar {
				candNames := []string{
					pkgIdent.Value + "_" + e.Field.Value,
					e.Field.Value,
				}
				for _, cand := range candNames {
					if val, ok := g.semaCtx.Constants[cand]; ok {
						g.log(fmt.Sprintf("Resolved constant: %s.%s = %d", pkgIdent.Value, e.Field.Value, val))
						return fmt.Sprintf("%d", val), sema.TypeInt
					}
				}
				panic(fmt.Sprintf("[Codegen Error] undefined package constant or member: %s.%s", pkgIdent.Value, e.Field.Value))
			}
		}

		objReg, objType := g.resolveValue(b, e.Object)
		st, structName := g.findStruct(objType, e.Field.Value)
		if st == nil {
			panic(fmt.Sprintf("[Codegen Error] cannot access field '%s' on non-struct type '%v'", e.Field.Value, objType))
		}

		fieldIdx := -1
		var fieldType sema.Type
		for i, f := range st.Fields {
			if f.Name == e.Field.Value {
				fieldIdx = i
				fieldType = f.Type
				break
			}
		}
		if fieldIdx == -1 {
			panic(fmt.Sprintf("[Codegen Error] unknown field '%s' in struct '%s'", e.Field.Value, structName))
		}

		objPtrReg := objReg
		if objIdent, isIdent := e.Object.(*ast.Identifier); isIdent {
			if sym, ok := g.symbols[objIdent.Value]; ok && !strings.HasSuffix(sym.Type.LLVMType(), "*") {
				objPtrReg = sym.LLVMName
			}
		} else if !strings.HasSuffix(objType.LLVMType(), "*") {
			tempAlloca := g.nextReg()
			g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %%struct.%s\n", tempAlloca, structName))
			b.WriteString(fmt.Sprintf("  store %%struct.%s %s, %%struct.%s* %s\n", structName, objReg, structName, tempAlloca))
			objPtrReg = tempAlloca
		}

		gepReg := g.nextReg()
		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
			gepReg, structName, structName, objPtrReg, fieldIdx))
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n",
			loadReg, fieldType.LLVMType(), fieldType.LLVMType(), gepReg))
		return loadReg, fieldType

	// 2. resolveValue の IndexExpr で固定長配列に対応
	case *ast.IndexExpr:
		baseReg, baseType := g.resolveValue(b, e.Left)
		idxReg, _ := g.resolveValue(b, e.Index)

		if arr, isArr := baseType.(*sema.ArrayType); isArr {
			arrPtrReg := baseReg
			if objIdent, isIdent := e.Left.(*ast.Identifier); isIdent {
				if sym, ok := g.symbols[objIdent.Value]; ok {
					arrPtrReg = sym.LLVMName
				}
			} else {
				tempAlloca := g.nextReg()
				g.entryAllocas.WriteString(fmt.Sprintf("  %s = alloca %s\n", tempAlloca, arr.LLVMType()))
				b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", arr.LLVMType(), baseReg, arr.LLVMType(), tempAlloca))
				arrPtrReg = tempAlloca
			}

			gepReg := g.nextReg()
			loadReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 0, i64 %s\n",
				gepReg, arr.LLVMType(), arr.LLVMType(), arrPtrReg, idxReg))
			b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n",
				loadReg, arr.Elem.LLVMType(), arr.Elem.LLVMType(), gepReg))
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
		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %s\n", gepReg, elemType.LLVMType(), elemType.LLVMType(), typedPtrReg, idxReg))
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, elemType.LLVMType(), elemType.LLVMType(), gepReg))

		if elemType.LLVMType() == "i8" {
			extReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", extReg, loadReg))
			return extReg, sema.TypeInt
		}
		return loadReg, elemType

	case *ast.SliceLiteral:
		slType := g.semaCtx.ResolveType(e.Type).(*sema.SliceType)
		elemSize := slType.Elem.Size()
		if elemSize <= 0 {
			elemSize = 1
		}
		numElems := len(e.Elements)
		allocCap := numElems
		if allocCap < 4 {
			allocCap = 4
		}

		rawPtrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", rawPtrReg, allocCap*elemSize))

		typedPtrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %s*\n", typedPtrReg, rawPtrReg, slType.Elem.LLVMType()))

		for i, elemExpr := range e.Elements {
			valReg, valType := g.resolveValue(b, elemExpr)
			destPtrReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %s, %s* %s, i64 %d\n", destPtrReg, slType.Elem.LLVMType(), slType.Elem.LLVMType(), typedPtrReg, i))

			valToStore := valReg
			if valType != nil && valType.LLVMType() != slType.Elem.LLVMType() {
				convReg := g.nextReg()
				if slType.Elem.LLVMType() == "i8" && valType.LLVMType() == "i64" {
					b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", convReg, valReg))
					valToStore = convReg
				} else if slType.Elem.LLVMType() == "i64" && valType.LLVMType() == "i8" {
					b.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", convReg, valReg))
					valToStore = convReg
				} else if strings.HasSuffix(slType.Elem.LLVMType(), "*") && strings.HasSuffix(valType.LLVMType(), "*") {
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, slType.Elem.LLVMType()))
					valToStore = convReg
				}
			}
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n", slType.Elem.LLVMType(), valToStore, slType.Elem.LLVMType(), destPtrReg))
		}

		t1 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i64, i64 } undef, i8* %s, 0\n", t1, rawPtrReg))
		t2 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i64, i64 } %s, i64 %d, 1\n", t2, t1, numElems))
		t3 := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue { i8*, i64, i64 } %s, i64 %d, 2\n", t3, t2, allocCap))

		return t3, slType

	case *ast.SliceExpr:
		baseReg, baseType := g.resolveValue(b, e.Left)

		if sliceType, isSlice := baseType.(*sema.SliceType); isSlice {
			lowReg := "0"
			if e.Low != nil {
				lowReg, _ = g.resolveValue(b, e.Low)
			}

			rawPtrReg := g.nextReg()
			oldLenReg := g.nextReg()
			oldCapReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 0\n", rawPtrReg, sliceType.LLVMType(), baseReg))
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 1\n", oldLenReg, sliceType.LLVMType(), baseReg))
			b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, 2\n", oldCapReg, sliceType.LLVMType(), baseReg))

			highReg := oldLenReg
			if e.High != nil {
				highReg, _ = g.resolveValue(b, e.High)
			}

			elemSize := sliceType.Elem.Size()
			if elemSize <= 0 {
				elemSize = 1
			}
			byteOffsetReg := lowReg
			if elemSize > 1 {
				byteOffsetReg = g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %d\n", byteOffsetReg, lowReg, elemSize))
			}

			newPtrReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", newPtrReg, rawPtrReg, byteOffsetReg))

			newLenReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", newLenReg, highReg, lowReg))

			newCapReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", newCapReg, oldCapReg, lowReg))

			retSliceType := "{ i8*, i64, i64 }"
			t1 := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = insertvalue %s undef, i8* %s, 0\n", t1, retSliceType, newPtrReg))
			t2 := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 1\n", t2, retSliceType, t1, newLenReg))
			t3 := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, i64 %s, 2\n", t3, retSliceType, t2, newCapReg))

			return t3, sliceType
		}

		lowReg := "0"
		if e.Low != nil {
			lowReg, _ = g.resolveValue(b, e.Low)
		}
		highReg := g.nextReg()
		if e.High != nil {
			highReg, _ = g.resolveValue(b, e.High)
		} else {
			b.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", highReg, baseReg))
		}
		subReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = call i8* @hike_substr(i8* %s, i64 %s, i64 %s)\n", subReg, baseReg, lowReg, highReg))
		return subReg, sema.TypeString

	case *ast.CallExpr:
		reg, retType := g.emitCallInternal(b, e)
		if reg == "" {
			return "0", sema.TypeVoid
		}
		return reg, retType

	case *ast.BinaryExpr:
		lhs, lhsType := g.resolveValue(b, e.Left)
		rhs, rhsType := g.resolveValue(b, e.Right)
		resReg := g.nextReg()

		isLhsStr := lhsType == sema.TypeString || (lhsType != nil && lhsType.TypeName() == "string")
		isRhsStr := rhsType == sema.TypeString || (rhsType != nil && rhsType.TypeName() == "string")

		if e.Operator == "+" && (isLhsStr || isRhsStr) {
			b.WriteString(fmt.Sprintf("  %s = call i8* @hike_strcat(i8* %s, i8* %s)\n", resReg, lhs, rhs))
			return resReg, sema.TypeString
		}

		if (e.Operator == "==" || e.Operator == "!=") && (isLhsStr || isRhsStr) && lhs != "null" && rhs != "null" {
			streqReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = call i1 @hike_streq(i8* %s, i8* %s)\n", streqReg, lhs, rhs))
			if e.Operator == "!=" {
				b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", resReg, streqReg))
				return resReg, sema.TypeBool
			}
			return streqReg, sema.TypeBool
		}

		if e.Operator == "&&" || e.Operator == "||" {
			lBool := lhs
			if lhsType == nil || lhsType.LLVMType() != "i1" {
				tStr := "i64"
				if lhsType != nil && lhsType.LLVMType() != "" {
					tStr = lhsType.LLVMType()
				}
				cmpVal := "0"
				if strings.HasSuffix(tStr, "*") {
					cmpVal = "null"
				}
				boolReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, %s\n", boolReg, tStr, lhs, cmpVal))
				lBool = boolReg
			}
			rBool := rhs
			if rhsType == nil || rhsType.LLVMType() != "i1" {
				tStr := "i64"
				if rhsType != nil && rhsType.LLVMType() != "" {
					tStr = rhsType.LLVMType()
				}
				cmpVal := "0"
				if strings.HasSuffix(tStr, "*") {
					cmpVal = "null"
				}
				boolReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, %s\n", boolReg, tStr, rhs, cmpVal))
				rBool = boolReg
			}

			if e.Operator == "&&" {
				b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", resReg, lBool, rBool))
			} else {
				b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", resReg, lBool, rBool))
			}
			return resReg, sema.TypeBool
		}

		switch e.Operator {
		case "&":
			b.WriteString(fmt.Sprintf("  %s = and i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case "|":
			b.WriteString(fmt.Sprintf("  %s = or i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case "^":
			b.WriteString(fmt.Sprintf("  %s = xor i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case "<<":
			b.WriteString(fmt.Sprintf("  %s = shl i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case ">>":
			b.WriteString(fmt.Sprintf("  %s = ashr i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case "+":
			if lhsType != nil && strings.HasSuffix(lhsType.LLVMType(), "*") && rhsType != nil && rhsType.LLVMType() == "i64" {
				b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", resReg, lhs, rhs))
				return resReg, lhsType
			}
			b.WriteString(fmt.Sprintf("  %s = add i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case "-":
			b.WriteString(fmt.Sprintf("  %s = sub i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case "*":
			b.WriteString(fmt.Sprintf("  %s = mul i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case "/":
			b.WriteString(fmt.Sprintf("  %s = sdiv i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, sema.TypeInt
		case "<", ">", "<=", ">=":
			opCode := "slt"
			switch e.Operator {
			case "<":
				opCode = "slt"
			case ">":
				opCode = "sgt"
			case "<=":
				opCode = "sle"
			case ">=":
				opCode = "sge"
			}
			b.WriteString(fmt.Sprintf("  %s = icmp %s i64 %s, %s\n", resReg, opCode, lhs, rhs))
			return resReg, sema.TypeBool
		case "==", "!=":
			opCode := "eq"
			if e.Operator == "!=" {
				opCode = "ne"
			}

			isLhsPtr := (lhsType != nil && strings.HasSuffix(lhsType.LLVMType(), "*")) || lhs == "null"
			isRhsPtr := (rhsType != nil && strings.HasSuffix(rhsType.LLVMType(), "*")) || rhs == "null"

			if isLhsPtr || isRhsPtr {
				ptrType := "i8*"
				if lhsType != nil && strings.HasSuffix(lhsType.LLVMType(), "*") {
					ptrType = lhsType.LLVMType()
				} else if rhsType != nil && strings.HasSuffix(rhsType.LLVMType(), "*") {
					ptrType = rhsType.LLVMType()
				}

				actualLhs := lhs
				actualRhs := rhs

				if actualLhs == "null" || actualLhs == "0" {
					actualLhs = "null"
				} else if lhsType != nil && lhsType.LLVMType() != ptrType {
					convReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, lhsType.LLVMType(), actualLhs, ptrType))
					actualLhs = convReg
				}

				if actualRhs == "null" || actualRhs == "0" {
					actualRhs = "null"
				} else if rhsType != nil && rhsType.LLVMType() != ptrType {
					convReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, rhsType.LLVMType(), actualRhs, ptrType))
					actualRhs = convReg
				}

				b.WriteString(fmt.Sprintf("  %s = icmp %s %s %s, %s\n", resReg, opCode, ptrType, actualLhs, actualRhs))
				return resReg, sema.TypeBool
			}

			if (lhsType != nil && lhsType.LLVMType() == "i1") || (rhsType != nil && rhsType.LLVMType() == "i1") {
				actualLhs := lhs
				actualRhs := rhs
				if lhsType != nil && lhsType.LLVMType() != "i1" {
					convReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = trunc %s %s to i1\n", convReg, lhsType.LLVMType(), actualLhs))
					actualLhs = convReg
				}
				if rhsType != nil && rhsType.LLVMType() != "i1" {
					convReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = trunc %s %s to i1\n", convReg, rhsType.LLVMType(), actualRhs))
					actualRhs = convReg
				}
				b.WriteString(fmt.Sprintf("  %s = icmp %s i1 %s, %s\n", resReg, opCode, actualLhs, actualRhs))
				return resReg, sema.TypeBool
			}

			cmpType := "i64"
			if lhsType != nil && lhsType.LLVMType() != "" {
				cmpType = lhsType.LLVMType()
			}
			b.WriteString(fmt.Sprintf("  %s = icmp %s %s %s, %s\n", resReg, opCode, cmpType, lhs, rhs))
			return resReg, sema.TypeBool
		}
	}
	return "0", sema.TypeInt
}

func (g *CodeGenerator) emitCallInternal(b *strings.Builder, call *ast.CallExpr) (string, sema.Type) {
	// 1. append(s1, s2...) スライス連結の展開
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

	// 2. append(s, elem1, elem2...) 単一・複数要素の追加
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

			valToStore := elemReg
			if elemType != nil && elemType.LLVMType() != sl.Elem.LLVMType() {
				convReg := g.nextReg()
				if sl.Elem.LLVMType() == "i8" && elemType.LLVMType() == "i64" {
					b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", convReg, elemReg))
					valToStore = convReg
				} else if sl.Elem.LLVMType() == "i64" && elemType.LLVMType() == "i8" {
					b.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", convReg, elemReg))
					valToStore = convReg
				} else if strings.HasSuffix(sl.Elem.LLVMType(), "*") && strings.HasSuffix(elemType.LLVMType(), "*") {
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, elemType.LLVMType(), elemReg, sl.Elem.LLVMType()))
					valToStore = convReg
				}
			}
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

	// 3. 組み込み関数 len() / cap()
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

	// 4. 型キャスト (*TypeName)(ptr)
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

	// 5. 基本型キャスト int(val)
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

	// 6. パッケージ関数呼び出し or 構造体メソッド呼び出し (obj.Method)
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
						t := argType
						if i < len(targetFn.ParamTypes) {
							t = targetFn.ParamTypes[i]
						}
						if argType != nil && t != nil && argType.LLVMType() != t.LLVMType() {
							convReg := g.nextReg()
							if strings.HasSuffix(argType.LLVMType(), "*") && strings.HasSuffix(t.LLVMType(), "*") {
								b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, argType.LLVMType(), argReg, t.LLVMType()))
								argReg = convReg
							} else if strings.HasSuffix(argType.LLVMType(), "*") && t.LLVMType() == "i64" {
								b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", convReg, argType.LLVMType(), argReg))
								argReg = convReg
							} else if argType.LLVMType() == "i64" && strings.HasSuffix(t.LLVMType(), "*") {
								b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", convReg, argReg, t.LLVMType()))
								argReg = convReg
							}
						}
						targetTypeStr := "i64"
						if t != nil && t.LLVMType() != "" {
							targetTypeStr = t.LLVMType()
						}
						args = append(args, fmt.Sprintf("%s %s", targetTypeStr, argReg))
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
				}
			}
		}

		objReg, objType := g.resolveValue(b, memExpr.Object)
		if objType != nil {
			_, structName := g.findStruct(objType, "")
			if structName != "" {
				methodName := memExpr.Field.Value
				targetFn := g.lookupFunction(structName + "_" + methodName)
				if targetFn == nil {
					targetFn = g.lookupFunction(methodName)
				}

				if targetFn != nil {
					emitTargetName := structName + "_" + methodName
					if targetFn.Name != "" && strings.Contains(targetFn.Name, "_") {
						emitTargetName = targetFn.Name
					}

					args := []string{}
					expectedRecvType := fmt.Sprintf("%%struct.%s*", structName)
					actualRecvReg := objReg
					if objType.LLVMType() != expectedRecvType {
						convReg := g.nextReg()
						if strings.HasSuffix(objType.LLVMType(), "*") {
							b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, objType.LLVMType(), objReg, expectedRecvType))
						} else {
							b.WriteString(fmt.Sprintf("  %s = inttoptr %s %s to %s\n", convReg, objType.LLVMType(), objReg, expectedRecvType))
						}
						actualRecvReg = convReg
					}
					args = append(args, fmt.Sprintf("%s %s", expectedRecvType, actualRecvReg))

					for i, arg := range call.Args {
						valReg, valType := g.resolveValue(b, arg)
						t := valType
						if i < len(targetFn.ParamTypes) {
							t = targetFn.ParamTypes[i]
						}
						if valType != nil && t != nil && valType.LLVMType() != t.LLVMType() {
							convReg := g.nextReg()
							if strings.HasSuffix(valType.LLVMType(), "*") && strings.HasSuffix(t.LLVMType(), "*") {
								b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, t.LLVMType()))
								valReg = convReg
							} else if strings.HasSuffix(valType.LLVMType(), "*") && t.LLVMType() == "i64" {
								b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", convReg, valType.LLVMType(), valReg))
								valReg = convReg
							} else if valType.LLVMType() == "i64" && strings.HasSuffix(t.LLVMType(), "*") {
								b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", convReg, valReg, t.LLVMType()))
								valReg = convReg
							}
						}
						targetTypeStr := "i64"
						if t != nil && t.LLVMType() != "" {
							targetTypeStr = t.LLVMType()
						}
						args = append(args, fmt.Sprintf("%s %s", targetTypeStr, valReg))
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
						b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", emitTargetName, strings.Join(args, ", ")))
						return "", sema.TypeVoid
					}

					callReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", callReg, retType, emitTargetName, strings.Join(args, ", ")))
					return callReg, semaRet
				}
			}
		}
	}

	// 7. 直接関数呼び出し (fn(a, b...))
	if fnIdent, ok := call.Function.(*ast.Identifier); ok {
		targetFn := g.lookupFunction(fnIdent.Value)
		if targetFn != nil {
			args := []string{}
			for i, arg := range call.Args {
				argReg, argType := g.resolveValue(b, arg)
				t := argType
				if i < len(targetFn.ParamTypes) {
					t = targetFn.ParamTypes[i]
				}
				if argType != nil && t != nil && argType.LLVMType() != t.LLVMType() {
					convReg := g.nextReg()
					if strings.HasSuffix(argType.LLVMType(), "*") && strings.HasSuffix(t.LLVMType(), "*") {
						b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, argType.LLVMType(), argReg, t.LLVMType()))
						argReg = convReg
					} else if strings.HasSuffix(argType.LLVMType(), "*") && t.LLVMType() == "i64" {
						b.WriteString(fmt.Sprintf("  %s = ptrtoint %s %s to i64\n", convReg, argType.LLVMType(), argReg))
						argReg = convReg
					} else if argType.LLVMType() == "i64" && strings.HasSuffix(t.LLVMType(), "*") {
						b.WriteString(fmt.Sprintf("  %s = inttoptr i64 %s to %s\n", convReg, argReg, t.LLVMType()))
						argReg = convReg
					}
				}
				targetTypeStr := "i64"
				if t != nil && t.LLVMType() != "" {
					targetTypeStr = t.LLVMType()
				}
				args = append(args, fmt.Sprintf("%s %s", targetTypeStr, argReg))
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

	return "", sema.TypeVoid
}

func (g *CodeGenerator) lookupFunction(name string) *sema.FuncType {
	if fn, ok := g.semaCtx.Functions[name]; ok {
		return fn
	}

	if idx := strings.Index(name, "_"); idx != -1 {
		baseName := name[idx+1:]
		if fn, ok := g.semaCtx.Functions[baseName]; ok {
			return fn
		}
		if lastIdx := strings.LastIndex(name, "_"); lastIdx != idx {
			lastBase := name[lastIdx+1:]
			if fn, ok := g.semaCtx.Functions[lastBase]; ok {
				return fn
			}
			structMethod := name[idx+1:]
			if fn, ok := g.semaCtx.Functions[structMethod]; ok {
				return fn
			}
		}
	}

	for fnName, fn := range g.semaCtx.Functions {
		if strings.HasSuffix(fnName, "_"+name) || strings.HasSuffix(fnName, name) {
			return fn
		}
	}

	return nil
}
