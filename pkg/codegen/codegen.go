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

type CodeGenerator struct {
	prog           *ast.Program
	semaCtx        *sema.Context
	output         strings.Builder
	regCount       int
	labelCount     int
	stringLiterals []StringConst
	symbols        map[string]Symbol
	deferStack     []string
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

	for _, fn := range g.semaCtx.Functions {
		if fn.IsExtern {
			if fn.Name == "malloc" || fn.Name == "free" || fn.Name == "calloc" {
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

	g.output.WriteString("define void @hike_arena_init(%struct.Arena* %a, i64 %size) {\n")
	g.output.WriteString("  %buf = call i8* @calloc(i64 1, i64 %size)\n")
	g.output.WriteString("  %pbuf = getelementptr inbounds %struct.Arena, %struct.Arena* %a, i32 0, i32 0\n")
	g.output.WriteString("  store i8* %buf, i8** %pbuf\n")
	g.output.WriteString("  %pcap = getelementptr inbounds %struct.Arena, %struct.Arena* %a, i32 0, i32 1\n")
	g.output.WriteString("  store i64 %size, i64* %pcap\n")
	g.output.WriteString("  %poff = getelementptr inbounds %struct.Arena, %struct.Arena* %a, i32 0, i32 2\n")
	g.output.WriteString("  store i64 0, i64* %poff\n")
	g.output.WriteString("  ret void\n")
	g.output.WriteString("}\n\n")

	g.output.WriteString("define void @hike_arena_free(%struct.Arena* %a) {\n")
	g.output.WriteString("  %pbuf = getelementptr inbounds %struct.Arena, %struct.Arena* %a, i32 0, i32 0\n")
	g.output.WriteString("  %buf = load i8*, i8** %pbuf\n")
	g.output.WriteString("  call void @free(i8* %buf)\n")
	g.output.WriteString("  ret void\n")
	g.output.WriteString("}\n\n")

	g.output.WriteString("define i8* @hike_arena_alloc(%struct.Arena* %a, i64 %size) {\n")
	g.output.WriteString("  %poff = getelementptr inbounds %struct.Arena, %struct.Arena* %a, i32 0, i32 2\n")
	g.output.WriteString("  %off = load i64, i64* %poff\n")
	g.output.WriteString("  %pbuf = getelementptr inbounds %struct.Arena, %struct.Arena* %a, i32 0, i32 0\n")
	g.output.WriteString("  %buf = load i8*, i8** %pbuf\n")
	g.output.WriteString("  %ptr = getelementptr inbounds i8, i8* %buf, i64 %off\n")
	g.output.WriteString("  %align = add i64 %size, 7\n")
	g.output.WriteString("  %aligned = and i64 %align, -8\n")
	g.output.WriteString("  %new_off = add i64 %off, %aligned\n")
	g.output.WriteString("  store i64 %new_off, i64* %poff\n")
	g.output.WriteString("  ret i8* %ptr\n")
	g.output.WriteString("}\n\n")
}

func (g *CodeGenerator) emitFuncDecl(b *strings.Builder, fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
	}

	g.symbols = make(map[string]Symbol)

	funcMangledName := fn.Name.Value
	var recvType sema.Type = nil
	var recvTypeName string = ""

	if fn.Receiver != nil {
		if pt, ok := fn.Receiver.Type.(*ast.PointerType); ok {
			if named, ok := pt.Base.(*ast.NamedType); ok {
				recvTypeName = named.Name.Value
				if st, ok := g.semaCtx.Structs[recvTypeName]; ok {
					recvType = &sema.PointerType{Base: st}
				}
			}
		} else if named, ok := fn.Receiver.Type.(*ast.NamedType); ok {
			recvTypeName = named.Name.Value
			if st, ok := g.semaCtx.Structs[recvTypeName]; ok {
				recvType = st
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
		if pt, ok := fn.ReturnTypes[0].(*ast.PointerType); ok {
			if named, ok := pt.Base.(*ast.NamedType); ok {
				if named.Name.Value == "byte" {
					retType = "i8*"
				} else if st, ok := g.semaCtx.Structs[named.Name.Value]; ok {
					retType = fmt.Sprintf("%%struct.%s*", st.Name)
				} else {
					retType = "i8*"
				}
			}
		} else if named, ok := fn.ReturnTypes[0].(*ast.NamedType); ok {
			if named.Name.Value == "int" {
				retType = "i64"
			} else if named.Name.Value == "bool" {
				retType = "i1"
			}
		}
	}

	if fn.Name.Value == "main" {
		retType = "i32"
	}

	params := []string{}
	if fn.Receiver != nil {
		params = append(params, fmt.Sprintf("%s %%%s_arg", recvType.LLVMType(), fn.Receiver.Name.Value))
	}

	for i, p := range fn.Params {
		pTypeStr := "i64"
		if exists && i < len(fnMeta.ParamTypes) {
			pTypeStr = fnMeta.ParamTypes[i].LLVMType()
		} else if pt, ok := p.Type.(*ast.PointerType); ok {
			if named, ok := pt.Base.(*ast.NamedType); ok {
				if named.Name.Value == "byte" {
					pTypeStr = "i8*"
				} else if st, ok := g.semaCtx.Structs[named.Name.Value]; ok {
					pTypeStr = fmt.Sprintf("%%struct.%s*", st.Name)
				}
			}
		}
		params = append(params, fmt.Sprintf("%s %%%s_arg", pTypeStr, p.Name.Value))
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
		} else if pt, ok := p.Type.(*ast.PointerType); ok {
			if named, ok := pt.Base.(*ast.NamedType); ok {
				if named.Name.Value == "byte" {
					pType = &sema.PointerType{Base: sema.TypeByte}
				} else if st, ok := g.semaCtx.Structs[named.Name.Value]; ok {
					pType = &sema.PointerType{Base: st}
				}
			}
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
			if memExpr, ok := s.Call.Function.(*ast.MemberExpr); ok && memExpr.Field.Value == "FreeAll" {
				if objIdent, ok := memExpr.Object.(*ast.Identifier); ok {
					cleanCode := fmt.Sprintf("call void @hike_arena_free(%%struct.Arena* %%%s)", objIdent.Value)
					g.deferStack = append([]string{cleanCode}, g.deferStack...)
				}
			}
		}
	case *ast.AssignStmt:
		g.emitAssignStmt(b, s)
	case *ast.IfStmt:
		g.emitIfStmt(b, s, currentFn)
	case *ast.ForStmt:
		g.emitForStmt(b, s, currentFn)
	case *ast.SwitchStmt:
		g.emitSwitchStmt(b, s, currentFn)
	case *ast.ReturnStmt:
		g.emitReturnStmt(b, s, currentFn)
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

func (g *CodeGenerator) emitSwitchStmt(b *strings.Builder, s *ast.SwitchStmt, currentFn string) {
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
	if len(s.Left) == 1 && len(s.Right) == 1 {
		if lhsIndex, isLhsIndex := s.Left[0].(*ast.IndexExpr); isLhsIndex {
			baseReg, _ := g.resolveValue(b, lhsIndex.Left)
			idxReg, _ := g.resolveValue(b, lhsIndex.Index)
			valReg, _ := g.resolveValue(b, s.Right[0])

			gepReg := g.nextReg()
			truncReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", gepReg, baseReg, idxReg))
			b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", truncReg, valReg))
			b.WriteString(fmt.Sprintf("  store i8 %s, i8* %s\n", truncReg, gepReg))
			return
		}

		if lhsIdent, isLhsIdent := s.Left[0].(*ast.Identifier); isLhsIdent {
			if call, ok := s.Right[0].(*ast.CallExpr); ok {
				if memExpr, ok := call.Function.(*ast.MemberExpr); ok && memExpr.Field.Value == "NewArena" {
					g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %%struct.Arena\n", lhsIdent.Value))
					b.WriteString(fmt.Sprintf("  call void @hike_arena_init(%%struct.Arena* %%%s, i64 65536)\n", lhsIdent.Value))
					g.symbols[lhsIdent.Value] = Symbol{Name: lhsIdent.Value, LLVMName: "%" + lhsIdent.Value, Type: &sema.BasicType{Name: "Arena", LLVM: "%struct.Arena"}}
					return
				}
			}

			valReg, valType := g.resolveValue(b, s.Right[0])
			if _, exists := g.symbols[lhsIdent.Value]; !exists {
				g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", lhsIdent.Value, valType.LLVMType()))
				g.symbols[lhsIdent.Value] = Symbol{Name: lhsIdent.Value, LLVMName: "%" + lhsIdent.Value, Type: valType}
			}
			sym := g.symbols[lhsIdent.Value]
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", sym.Type.LLVMType(), valReg, sym.Type.LLVMType(), lhsIdent.Value))
			return
		}

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

			valToStore := valReg
			if strings.HasSuffix(fieldType.LLVMType(), "*") {
				if valReg == "0" || valReg == "null" || valReg == "" {
					valToStore = "null"
				} else if valType != nil && !strings.HasSuffix(valType.LLVMType(), "*") {
					convReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = inttoptr %s %s to %s\n", convReg, valType.LLVMType(), valReg, fieldType.LLVMType()))
					valToStore = convReg
				} else if valType != nil && valType.LLVMType() != fieldType.LLVMType() {
					convReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, fieldType.LLVMType()))
					valToStore = convReg
				}
			}

			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
				gepReg, structName, structName, objReg, fieldIdx))
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n",
				fieldType.LLVMType(), valToStore, fieldType.LLVMType(), gepReg))
			return
		}
	}

	if len(s.Left) >= 2 && len(s.Right) == 1 {
		if call, ok := s.Right[0].(*ast.CallExpr); ok {
			var targetTypeName string
			if genIdx, isGen := call.Function.(*ast.GenericIndexExpr); isGen {
				targetTypeName = genIdx.Index.(*ast.NamedType).Name.Value
			} else if idxExpr, isIdx := call.Function.(*ast.IndexExpr); isIdx {
				if id, ok := idxExpr.Index.(*ast.Identifier); ok {
					targetTypeName = id.Value
				}
			}

			if targetTypeName != "" {
				st := g.semaCtx.Structs[targetTypeName]
				v1 := s.Left[0].(*ast.Identifier).Value
				v2 := s.Left[1].(*ast.Identifier).Value

				rawPtr := g.nextReg()
				ptrReg := g.nextReg()

				allocatedWithArena := false
				if len(call.Args) > 0 {
					if argIdent, ok := call.Args[0].(*ast.Identifier); ok {
						if sym, exists := g.symbols[argIdent.Value]; exists {
							if sym.Type.TypeName() == "Arena" {
								b.WriteString(fmt.Sprintf("  %s = call i8* @hike_arena_alloc(%%struct.Arena* %s, i64 %d)\n",
									rawPtr, sym.LLVMName, st.TotalSize))
								allocatedWithArena = true
							} else if sym.Type.TypeName() == "*Arena" {
								arenaReg := g.nextReg()
								b.WriteString(fmt.Sprintf("  %s = load %%struct.Arena*, %%struct.Arena** %s\n", arenaReg, sym.LLVMName))
								b.WriteString(fmt.Sprintf("  %s = call i8* @hike_arena_alloc(%%struct.Arena* %s, i64 %d)\n",
									rawPtr, arenaReg, st.TotalSize))
								allocatedWithArena = true
							}
						}
					}
				}

				if !allocatedWithArena {
					b.WriteString(fmt.Sprintf("  %s = call i8* @malloc(i64 %d)\n", rawPtr, st.TotalSize))
				}

				b.WriteString(fmt.Sprintf("  %s = bitcast i8* %s to %%struct.%s*\n", ptrReg, rawPtr, st.Name))

				if v1 != "_" {
					b.WriteString(fmt.Sprintf("  %%%s = alloca %%struct.%s*\n", v1, st.Name))
					b.WriteString(fmt.Sprintf("  store %%struct.%s* %s, %%struct.%s** %%%s\n", st.Name, ptrReg, st.Name, v1))
					g.symbols[v1] = Symbol{Name: v1, LLVMName: "%" + v1, Type: &sema.PointerType{Base: st}}
				}

				if v2 != "_" {
					b.WriteString(fmt.Sprintf("  %%%s = alloca i64\n", v2))
					b.WriteString(fmt.Sprintf("  store i64 0, i64* %%%s\n", v2))
					g.symbols[v2] = Symbol{Name: v2, LLVMName: "%" + v2, Type: sema.TypeInt}
				}
				return
			}

			if fnIdent, isIdent := call.Function.(*ast.Identifier); isIdent {
				targetFn := g.semaCtx.Functions[fnIdent.Value]

				args := []string{}
				sigTypes := []string{}
				for i, arg := range call.Args {
					valReg, valType := g.resolveValue(b, arg)
					targetType := valType
					if i < len(targetFn.ParamTypes) {
						targetType = targetFn.ParamTypes[i]
					}
					args = append(args, fmt.Sprintf("%s %s", targetType.LLVMType(), valReg))
					sigTypes = append(sigTypes, targetType.LLVMType())
				}

				types := make([]string, len(targetFn.ReturnTypes))
				for i, t := range targetFn.ReturnTypes {
					types[i] = t.LLVMType()
				}
				retTupleType := fmt.Sprintf("{ %s }", strings.Join(types, ", "))

				tupleReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = call %s (%s) @%s(%s)\n", tupleReg, retTupleType, strings.Join(sigTypes, ", "), targetFn.Name, strings.Join(args, ", ")))

				for i, lhs := range s.Left {
					if id, ok := lhs.(*ast.Identifier); ok {
						valReg := g.nextReg()
						retT := targetFn.ReturnTypes[i]
						b.WriteString(fmt.Sprintf("  %s = extractvalue %s %s, %d\n", valReg, retTupleType, tupleReg, i))

						if id.Value != "_" {
							b.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", id.Value, retT.LLVMType()))
							b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", retT.LLVMType(), valReg, retT.LLVMType(), id.Value))
							g.symbols[id.Value] = Symbol{Name: id.Value, LLVMName: "%" + id.Value, Type: retT}
						}
					}
				}
			}
		}
	}
}

func (g *CodeGenerator) emitIfStmt(b *strings.Builder, s *ast.IfStmt, currentFn string) {
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

func (g *CodeGenerator) emitReturnStmt(b *strings.Builder, s *ast.ReturnStmt, currentFn string) {
	for _, deferCall := range g.deferStack {
		b.WriteString(fmt.Sprintf("  %s\n", deferCall))
	}

	if currentFn == "main" {
		if len(s.Values) == 1 {
			valReg, valType := g.resolveValue(b, s.Values[0])
			retReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = trunc %s %s to i32\n", retReg, valType.LLVMType(), valReg))
			b.WriteString(fmt.Sprintf("  ret i32 %s\n", retReg))
			return
		}
		b.WriteString("  ret i32 0\n")
		return
	}

	fnType := g.lookupFunction(currentFn)

	if len(s.Values) == 0 {
		b.WriteString("  ret void\n")
		return
	}

	if len(s.Values) == 1 {
		valReg, valType := g.resolveValue(b, s.Values[0])

		targetTypeStr := valType.LLVMType()
		if fnType != nil && len(fnType.ReturnTypes) == 1 {
			targetTypeStr = fnType.ReturnTypes[0].LLVMType()
		}

		if _, isNil := s.Values[0].(*ast.NilLiteral); isNil {
			b.WriteString(fmt.Sprintf("  ret %s null\n", targetTypeStr))
			return
		}

		if strings.HasSuffix(targetTypeStr, "*") {
			if valReg == "0" || valReg == "null" || valReg == "" {
				valReg = "null"
			}
		}

		if valType != nil && valType.LLVMType() != targetTypeStr {
			if targetTypeStr == "i64" && valType.LLVMType() == "i1" {
				zextReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", zextReg, valReg))
				valReg = zextReg
			} else if targetTypeStr == "i1" && valType.LLVMType() == "i64" {
				cmpReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = icmp ne i64 %s, 0\n", cmpReg, valReg))
				valReg = cmpReg
			} else if strings.HasSuffix(targetTypeStr, "*") && strings.HasSuffix(valType.LLVMType(), "*") {
				convReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %s\n", convReg, valType.LLVMType(), valReg, targetTypeStr))
				valReg = convReg
			}
		}

		b.WriteString(fmt.Sprintf("  ret %s %s\n", targetTypeStr, valReg))
		return
	}

	var types []string
	if fnType != nil && len(fnType.ReturnTypes) == len(s.Values) {
		for _, t := range fnType.ReturnTypes {
			types = append(types, t.LLVMType())
		}
	} else {
		for _, v := range s.Values {
			_, t := g.resolveValue(b, v)
			types = append(types, t.LLVMType())
		}
	}
	retTupleType := fmt.Sprintf("{ %s }", strings.Join(types, ", "))

	currentTuple := "undef"
	for i, valExpr := range s.Values {
		targetTypeStr := types[i]
		valReg, _ := g.resolveValue(b, valExpr)
		if _, isNil := valExpr.(*ast.NilLiteral); isNil {
			valReg = "null"
		}
		if strings.HasSuffix(targetTypeStr, "*") && (valReg == "0" || valReg == "null") {
			valReg = "null"
		}
		nextTuple := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, %d\n", nextTuple, retTupleType, currentTuple, targetTypeStr, valReg, i))
		currentTuple = nextTuple
	}
	b.WriteString(fmt.Sprintf("  ret %s %s\n", retTupleType, currentTuple))
}

func (g *CodeGenerator) emitCallExpr(b *strings.Builder, call *ast.CallExpr) (string, sema.Type) {
	return g.emitCallInternal(b, call)
}

func (g *CodeGenerator) resolveValue(b *strings.Builder, expr ast.Expression) (string, sema.Type) {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value), sema.TypeInt

	case *ast.NilLiteral:
		return "null", &sema.PointerType{Base: sema.TypeByte}

	case *ast.StringLiteral:
		label, length := g.addStringLiteral(e.Value)
		ptrReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0\n", ptrReg, length, length, label))
		return ptrReg, &sema.PointerType{Base: sema.TypeByte}

	case *ast.PrefixExpr:
		if e.Operator == "&" {
			if ident, ok := e.Right.(*ast.Identifier); ok {
				if sym, exists := g.symbols[ident.Value]; exists {
					return sym.LLVMName, &sema.PointerType{Base: sym.Type}
				}
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

		sym, exists := g.symbols[e.Value]
		if !exists {
			return "0", sema.TypeInt
		}
		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, sym.Type.LLVMType(), sym.Type.LLVMType(), sym.LLVMName))
		return loadReg, sym.Type

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

		gepReg := g.nextReg()
		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
			gepReg, structName, structName, objReg, fieldIdx))
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n",
			loadReg, fieldType.LLVMType(), fieldType.LLVMType(), gepReg))
		return loadReg, fieldType

	case *ast.IndexExpr:
		baseReg, _ := g.resolveValue(b, e.Left)
		idxReg, _ := g.resolveValue(b, e.Index)
		gepReg := g.nextReg()
		loadReg := g.nextReg()
		extReg := g.nextReg()

		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %s\n", gepReg, baseReg, idxReg))
		b.WriteString(fmt.Sprintf("  %s = load i8, i8* %s\n", loadReg, gepReg))
		b.WriteString(fmt.Sprintf("  %s = zext i8 %s to i64\n", extReg, loadReg))
		return extReg, sema.TypeInt

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
	// 1. 型キャスト: (*Type)(expr) または (*pkg.Type)(expr)
	if pref, ok := call.Function.(*ast.PrefixExpr); ok && pref.Operator == "*" {
		var typeName string
		if ident, ok := pref.Right.(*ast.Identifier); ok {
			typeName = ident.Value
		} else if mem, ok := pref.Right.(*ast.MemberExpr); ok {
			typeName = mem.Field.Value
		}

		if typeName != "" {
			argReg, argType := g.resolveValue(b, call.Args[0])
			srcTypeStr := "i8*"
			if argType != nil && argType.LLVMType() != "" {
				srcTypeStr = argType.LLVMType()
			}

			var targetType sema.Type
			var targetTypeStr string
			if typeName == "byte" {
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
				g.log(fmt.Sprintf("Applied pointer cast to *%s (src: %s, target: %s)", typeName, srcTypeStr, targetTypeStr))
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

	// 2. 型キャスト: int(val) / int(ptr)
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

	// 3. メンバ経由の呼び出し (パッケージ関数 または オブジェクトメソッド)
	if memExpr, ok := call.Function.(*ast.MemberExpr); ok {
		isVariable := false
		if objIdent, ok := memExpr.Object.(*ast.Identifier); ok {
			if _, exists := g.symbols[objIdent.Value]; exists {
				isVariable = true
			}
		}

		// パターンA: パッケージ関数呼び出し
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
					}

					emitFnName := targetFn.Name
					g.log(fmt.Sprintf("Resolved package function call: %s.%s -> @%s", pkgIdent.Value, methodName, emitFnName))

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

		// パターンB: レシーバオブジェクトに対するメソッド呼び出し
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
					g.log(fmt.Sprintf("Resolved method call: %s.%s -> @%s", structName, methodName, emitTargetName))

					args := []string{}

					// 第0引数: レシーバ (%struct.StructName*)
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

					// 第1引数以降: メソッドの実引数
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
							} else if valType.LLVMType() == "i1" && t.LLVMType() == "i64" {
								b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i64\n", convReg, valReg))
								valReg = convReg
							} else if valType.LLVMType() == "i64" && t.LLVMType() == "i1" {
								b.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i1\n", convReg, valReg))
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

	// 4. 通常の識別子関数呼び出し
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
				types := make([]string, len(targetFn.ReturnTypes))
				for i, t := range targetFn.ReturnTypes {
					types[i] = t.LLVMType()
				}
				retTupleType := fmt.Sprintf("{ %s }", strings.Join(types, ", "))
				retType = retTupleType
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

			g.log(fmt.Sprintf("Resolved identifier function call: %s -> @%s", fnIdent.Value, emitFnName))

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
