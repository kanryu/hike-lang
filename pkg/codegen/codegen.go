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
	entryAllocas   *strings.Builder // entry ブロック用のアロケーションバッファ
}

func New(prog *ast.Program, semaCtx *sema.Context) *CodeGenerator {
	return &CodeGenerator{
		prog:           prog,
		semaCtx:        semaCtx,
		symbols:        make(map[string]Symbol),
		stringLiterals: []StringConst{},
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

func (g *CodeGenerator) addStringLiteral(str string) (string, int) {
	unescaped := strings.ReplaceAll(str, `\n`, "\n")
	unescaped = strings.ReplaceAll(unescaped, `\t`, "\t")
	unescaped = strings.ReplaceAll(unescaped, `\0`, "\x00")
	length := len(unescaped) + 1

	label := fmt.Sprintf("@.str.%d", len(g.stringLiterals)+1)
	g.stringLiterals = append(g.stringLiterals, StringConst{
		Label:  label,
		Value:  str,
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
		llvmEscaped := strings.ReplaceAll(sc.Value, `\n`, "\\0A")
		llvmEscaped = strings.ReplaceAll(llvmEscaped, `\t`, "\\09")
		g.output.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\", align 1\n", sc.Label, sc.Length, llvmEscaped))
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

	// 1. ソースコード内で定義された全構造体を最上段に出力
	for _, st := range g.semaCtx.Structs {
		fields := make([]string, len(st.Fields))
		for i, f := range st.Fields {
			fields[i] = f.Type.LLVMType()
		}
		g.output.WriteString(fmt.Sprintf("%%struct.%s = type { %s }\n", st.Name, strings.Join(fields, ", ")))
	}

	// 2. ユーザーコードで未定義の場合のみビルトイン構造体をフォールバック出力
	if _, ok := g.semaCtx.Structs["Arena"]; !ok {
		g.output.WriteString("%struct.Arena = type { i8*, i64, i64 }\n")
	}
	if _, ok := g.semaCtx.Structs["Allocator"]; !ok {
		g.output.WriteString("%struct.Allocator = type { i8*, i8* }\n")
	}
	g.output.WriteString("\n")

	// 3. ビルトイン関数（calloc で 0 クリアして確保）
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

	fnMeta, exists := g.semaCtx.Functions[fn.Name.Value]
	retType := "void"
	if exists && len(fnMeta.ReturnTypes) == 1 {
		retType = fnMeta.ReturnTypes[0].LLVMType()
	} else if exists && len(fnMeta.ReturnTypes) > 1 {
		types := []string{}
		for _, rt := range fnMeta.ReturnTypes {
			types = append(types, rt.LLVMType())
		}
		retType = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
	}
	if fn.Name.Value == "main" {
		retType = "i32"
	}

	params := []string{}
	for i, p := range fn.Params {
		pTypeStr := "i64"
		if exists && i < len(fnMeta.ParamTypes) {
			pTypeStr = fnMeta.ParamTypes[i].LLVMType()
		}
		params = append(params, fmt.Sprintf("%s %%%s_arg", pTypeStr, p.Name.Value))
	}

	var entryAllocas strings.Builder
	var bodyBuilder strings.Builder
	g.entryAllocas = &entryAllocas

	for i, p := range fn.Params {
		var pType sema.Type = sema.TypeInt
		if exists && i < len(fnMeta.ParamTypes) {
			pType = fnMeta.ParamTypes[i]
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
		g.emitStatement(&bodyBuilder, s, fn.Name.Value)
	}

	if retType == "void" {
		bodyBuilder.WriteString("  ret void\n")
	} else if retType == "i32" {
		bodyBuilder.WriteString("  ret i32 0\n")
	} else if retType == "i64" {
		bodyBuilder.WriteString("  ret i64 0\n")
	} else if strings.HasSuffix(retType, "*") {
		bodyBuilder.WriteString(fmt.Sprintf("  ret %s null\n", retType))
	} else {
		bodyBuilder.WriteString(fmt.Sprintf("  ret %s zeroinitializer\n", retType))
	}

	b.WriteString(fmt.Sprintf("define %s @%s(%s) {\nentry:\n", retType, fn.Name.Value, strings.Join(params, ", ")))
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
		condReg, _ := g.resolveValue(b, s.Cond)
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n\n", condReg, lblBody, lblEnd))
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
	lblDefault := lblEnd // default節がない場合はそのまま end へジャンプ

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
			// default
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

	// 1. switch 分岐テーブルを出力
	b.WriteString(fmt.Sprintf("  switch i64 %s, label %%%s [\n", valReg, lblDefault))
	for _, t := range targets {
		b.WriteString(fmt.Sprintf("    i64 %s, label %%%s\n", t.valReg, t.label))
	}
	b.WriteString("  ]\n\n")

	// 2. 各 case のブロック本体を出力（重複ラベルの出力抑止）
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

	// 3. default ブロックの出力
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

func (g *CodeGenerator) emitCallExpr(b *strings.Builder, call *ast.CallExpr) (string, sema.Type) {
	// パッケージ関数呼び出し (fmt.PrintLn(...) など)
	if memExpr, ok := call.Function.(*ast.MemberExpr); ok {
		if pkgIdent, isIdent := memExpr.Object.(*ast.Identifier); isIdent {
			targetFnName := pkgIdent.Value + "_" + memExpr.Field.Value
			if fnType, exists := g.semaCtx.Functions[targetFnName]; exists {
				args := []string{}
				for i, arg := range call.Args {
					valReg, valType := g.resolveValue(b, arg)
					t := valType
					if i < len(fnType.ParamTypes) {
						t = fnType.ParamTypes[i]
					}
					args = append(args, fmt.Sprintf("%s %s", t.LLVMType(), valReg))
				}

				retType := "void"
				var actualRetType sema.Type = sema.TypeVoid
				if len(fnType.ReturnTypes) == 1 {
					retType = fnType.ReturnTypes[0].LLVMType()
					actualRetType = fnType.ReturnTypes[0]
				}

				if retType == "void" {
					b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", targetFnName, strings.Join(args, ", ")))
					return "", sema.TypeVoid
				}

				retReg := g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", retReg, retType, targetFnName, strings.Join(args, ", ")))
				return retReg, actualRetType
			}
		}
	}

	if fnIdent, ok := call.Function.(*ast.Identifier); ok {
		fnType := g.semaCtx.Functions[fnIdent.Value]
		args := []string{}

		for i, arg := range call.Args {
			valReg, valType := g.resolveValue(b, arg)
			targetType := valType

			// 仮引数の定義範囲内であればその型に合わせ、可変長部分（i >= len）は実引数の型を維持する
			if fnType != nil && i < len(fnType.ParamTypes) {
				targetType = fnType.ParamTypes[i]
			}

			args = append(args, fmt.Sprintf("%s %s", targetType.LLVMType(), valReg))
		}

		retType := "void"
		var actualRetType sema.Type = sema.TypeVoid
		sigParamTypes := []string{}

		if fnType != nil {
			if len(fnType.ReturnTypes) == 1 {
				retType = fnType.ReturnTypes[0].LLVMType()
				actualRetType = fnType.ReturnTypes[0]
			} else if len(fnType.ReturnTypes) > 1 {
				types := make([]string, len(fnType.ReturnTypes))
				for i, t := range fnType.ReturnTypes {
					types[i] = t.LLVMType()
				}
				retType = fmt.Sprintf("{ %s }", strings.Join(types, ", "))
			}

			// シグネチャには宣言時の仮引数型のみを格納
			for _, p := range fnType.ParamTypes {
				sigParamTypes = append(sigParamTypes, p.LLVMType())
			}
			if fnType.IsVariadic {
				sigParamTypes = append(sigParamTypes, "...")
			}
		}

		retReg := ""
		if retType != "void" {
			retReg = g.nextReg()
			if len(sigParamTypes) > 0 {
				b.WriteString(fmt.Sprintf("  %s = call %s (%s) @%s(%s)\n", retReg, retType, strings.Join(sigParamTypes, ", "), fnIdent.Value, strings.Join(args, ", ")))
			} else {
				b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", retReg, retType, fnIdent.Value, strings.Join(args, ", ")))
			}
		} else {
			if len(sigParamTypes) > 0 {
				b.WriteString(fmt.Sprintf("  call void (%s) @%s(%s)\n", strings.Join(sigParamTypes, ", "), fnIdent.Value, strings.Join(args, ", ")))
			} else {
				b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", fnIdent.Value, strings.Join(args, ", ")))
			}
		}
		return retReg, actualRetType
	}
	return "", sema.TypeVoid
}

// 構造体定義を安全に特定するヘルパー
func (g *CodeGenerator) findStruct(t sema.Type, fieldName string) (*sema.StructType, string) {
	if t != nil {
		// 1. StructType 直接の場合
		if st, ok := t.(*sema.StructType); ok {
			return st, st.Name
		}
		// 2. ポインタ型を剥がしてチェック
		if ptr, ok := t.(*sema.PointerType); ok {
			if st, ok := ptr.Base.(*sema.StructType); ok {
				return st, st.Name
			}
		}
		// 3. LLVMType 文字列から構造体名を逆引き (%struct.AstNode* -> AstNode)
		llvmStr := t.LLVMType()
		cleanName := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(llvmStr, "*", ""), "%struct.", ""))
		if st, ok := g.semaCtx.Structs[cleanName]; ok {
			return st, st.Name
		}
	}

	// 4. フォールバック: 対象フィールドを持つ構造体を semaCtx から探索
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
		// インデックス代入: buf[i] = val
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

		// 変数宣言または代入
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
				// alloca を entry ブロックへ集約
				g.entryAllocas.WriteString(fmt.Sprintf("  %%%s = alloca %s\n", lhsIdent.Value, valType.LLVMType()))
				g.symbols[lhsIdent.Value] = Symbol{Name: lhsIdent.Value, LLVMName: "%" + lhsIdent.Value, Type: valType}
			}
			sym := g.symbols[lhsIdent.Value]
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %%%s\n", sym.Type.LLVMType(), valReg, sym.Type.LLVMType(), lhsIdent.Value))
			return
		}

		// 構造体フィールド代入: u.ID = id
		if lhsMember, isLhsMember := s.Left[0].(*ast.MemberExpr); isLhsMember {
			objReg, objType := g.resolveValue(b, lhsMember.Object)
			st, structName := g.findStruct(objType, lhsMember.Field.Value)
			if st == nil {
				panic(fmt.Sprintf("cannot assign to field %s on non-struct type %v", lhsMember.Field.Value, objType))
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
				panic(fmt.Sprintf("unknown field %s in struct %s", lhsMember.Field.Value, structName))
			}

			valReg, _ := g.resolveValue(b, s.Right[0])

			gepReg := g.nextReg()
			b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
				gepReg, structName, structName, objReg, fieldIdx))
			b.WriteString(fmt.Sprintf("  store %s %s, %s* %s\n",
				fieldType.LLVMType(), valReg, fieldType.LLVMType(), gepReg))
			return
		}
	}

	// 多値代入: u, err := ...
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

			// mem.Alloc[T](alloc) の場合
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

				// v1 が "_" でない場合のみメモリ確保・代入
				if v1 != "_" {
					b.WriteString(fmt.Sprintf("  %%%s = alloca %%struct.%s*\n", v1, st.Name))
					b.WriteString(fmt.Sprintf("  store %%struct.%s* %s, %%struct.%s** %%%s\n", st.Name, ptrReg, st.Name, v1))
					g.symbols[v1] = Symbol{Name: v1, LLVMName: "%" + v1, Type: &sema.PointerType{Base: st}}
				}

				// v2 が "_" でない場合のみメモリ確保・代入
				if v2 != "_" {
					b.WriteString(fmt.Sprintf("  %%%s = alloca i64\n", v2))
					b.WriteString(fmt.Sprintf("  store i64 0, i64* %%%s\n", v2))
					g.symbols[v2] = Symbol{Name: v2, LLVMName: "%" + v2, Type: sema.TypeInt}
				}
				return
			}

			// 汎用多値返却関数の呼び出し
			if fnIdent, isIdent := call.Function.(*ast.Identifier); isIdent {
				targetFn := g.semaCtx.Functions[fnIdent.Value]

				// 引数の評価
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
		boolReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0\n", boolReg, condType.LLVMType(), condReg))
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

	// then ブロック
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

	// else / else if ブロック
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
			// else if の場合は外側の lblEnd を引き継ぐ
			g.emitIfStmtWithEnd(b, alt, currentFn, lblEnd)
		}
	}

	// 最も外側の if のみ終了ラベルを出力
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

	fnType := g.semaCtx.Functions[currentFn]
	if len(fnType.ReturnTypes) == 0 {
		b.WriteString("  ret void\n")
		return
	}

	if len(fnType.ReturnTypes) == 1 {
		targetType := fnType.ReturnTypes[0]
		valReg, _ := g.resolveValue(b, s.Values[0])
		if _, isNil := s.Values[0].(*ast.NilLiteral); isNil {
			b.WriteString(fmt.Sprintf("  ret %s null\n", targetType.LLVMType()))
			return
		}
		b.WriteString(fmt.Sprintf("  ret %s %s\n", targetType.LLVMType(), valReg))
		return
	}

	// 多値返却
	types := make([]string, len(fnType.ReturnTypes))
	for i, t := range fnType.ReturnTypes {
		types[i] = t.LLVMType()
	}
	retTupleType := fmt.Sprintf("{ %s }", strings.Join(types, ", "))

	currentTuple := "undef"
	for i, valExpr := range s.Values {
		targetType := fnType.ReturnTypes[i]
		valReg, _ := g.resolveValue(b, valExpr)
		if _, isNil := valExpr.(*ast.NilLiteral); isNil {
			valReg = "null"
		}
		nextTuple := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = insertvalue %s %s, %s %s, %d\n", nextTuple, retTupleType, currentTuple, targetType.LLVMType(), valReg, i))
		currentTuple = nextTuple
	}
	b.WriteString(fmt.Sprintf("  ret %s %s\n", retTupleType, currentTuple))
}

func (g *CodeGenerator) resolveValue(b *strings.Builder, expr ast.Expression) (string, sema.Type) {
	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return fmt.Sprintf("%d", e.Value), sema.TypeInt

	case *ast.NilLiteral:
		return "null", &sema.PointerType{Base: sema.TypeByte}

	case *ast.StringLiteral:
		label, length := g.addStringLiteral(e.Value)
		ptrReg := g.nextReg() // ← 末尾の "emitMemberPtr" を削除
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0\n", ptrReg, length, length, label))
		return ptrReg, &sema.PointerType{Base: sema.TypeByte}

	case *ast.Identifier:
		sym, exists := g.symbols[e.Value]
		if !exists {
			return "0", sema.TypeInt
		}
		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n", loadReg, sym.Type.LLVMType(), sym.Type.LLVMType(), sym.LLVMName))
		return loadReg, sym.Type

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

	case *ast.MemberExpr:
		objReg, objType := g.resolveValue(b, e.Object)
		st, structName := g.findStruct(objType, e.Field.Value)
		if st == nil {
			panic(fmt.Sprintf("cannot access field %s on non-struct type %v", e.Field.Value, objType))
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
			panic(fmt.Sprintf("unknown field %s in struct %s", e.Field.Value, structName))
		}

		gepReg := g.nextReg()
		loadReg := g.nextReg()
		b.WriteString(fmt.Sprintf("  %s = getelementptr inbounds %%struct.%s, %%struct.%s* %s, i32 0, i32 %d\n",
			gepReg, structName, structName, objReg, fieldIdx))
		b.WriteString(fmt.Sprintf("  %s = load %s, %s* %s\n",
			loadReg, fieldType.LLVMType(), fieldType.LLVMType(), gepReg))
		return loadReg, fieldType

	case *ast.CallExpr:
		// パッケージ関数呼び出し (os.Alloc(...) など)
		if memExpr, ok := e.Function.(*ast.MemberExpr); ok {
			if pkgIdent, isIdent := memExpr.Object.(*ast.Identifier); isIdent {
				targetFnName := pkgIdent.Value + "_" + memExpr.Field.Value
				if targetFn, exists := g.semaCtx.Functions[targetFnName]; exists {
					args := []string{}
					for i, arg := range e.Args {
						argReg, argType := g.resolveValue(b, arg)
						t := argType
						if i < len(targetFn.ParamTypes) {
							t = targetFn.ParamTypes[i]
						}
						args = append(args, fmt.Sprintf("%s %s", t.LLVMType(), argReg))
					}

					retType := "void"
					var semaRet sema.Type = sema.TypeVoid
					if len(targetFn.ReturnTypes) == 1 {
						retType = targetFn.ReturnTypes[0].LLVMType()
						semaRet = targetFn.ReturnTypes[0]
					}

					if retType == "void" {
						b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", targetFn.Name, strings.Join(args, ", ")))
						return "0", sema.TypeVoid
					}

					callReg := g.nextReg()
					b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", callReg, retType, targetFn.Name, strings.Join(args, ", ")))
					return callReg, semaRet
				}
			}
		}

		// 2. 型キャスト: (*byte)(ptr), (*Arena)(ptr) 等
		if pref, ok := e.Function.(*ast.PrefixExpr); ok && pref.Operator == "*" {
			if ident, ok := pref.Right.(*ast.Identifier); ok {
				argReg, argType := g.resolveValue(b, e.Args[0])
				castReg := g.nextReg()
				srcTypeStr := "i8*"
				if argType != nil && argType.LLVMType() != "" {
					srcTypeStr = argType.LLVMType()
				}
				if ident.Value == "byte" {
					targetType := &sema.PointerType{Base: sema.TypeByte}
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i8*\n", castReg, srcTypeStr, argReg))
					return castReg, targetType
				}
				if ident.Value == "int" {
					targetType := &sema.PointerType{Base: sema.TypeInt}
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to i64*\n", castReg, srcTypeStr, argReg))
					return castReg, targetType
				}
				if st, ok := g.semaCtx.Structs[ident.Value]; ok {
					targetType := &sema.PointerType{Base: st}
					b.WriteString(fmt.Sprintf("  %s = bitcast %s %s to %%struct.%s*\n", castReg, srcTypeStr, argReg, ident.Value))
					return castReg, targetType
				}
			}
		}

		// 通常の単一値返却関数の呼び出し
		if fnIdent, ok := e.Function.(*ast.Identifier); ok {
			targetFn, exists := g.semaCtx.Functions[fnIdent.Value]
			if exists {
				args := []string{}
				for i, arg := range e.Args {
					argReg, argType := g.resolveValue(b, arg)
					t := argType
					if i < len(targetFn.ParamTypes) {
						t = targetFn.ParamTypes[i]
					}
					args = append(args, fmt.Sprintf("%s %s", t.LLVMType(), argReg))
				}
				retType := "void"
				var semaRet sema.Type = sema.TypeVoid
				if len(targetFn.ReturnTypes) == 1 {
					retType = targetFn.ReturnTypes[0].LLVMType()
					semaRet = targetFn.ReturnTypes[0]
				}

				sigParamTypes := []string{}
				for _, p := range targetFn.ParamTypes {
					sigParamTypes = append(sigParamTypes, p.LLVMType())
				}
				if targetFn.IsVariadic {
					sigParamTypes = append(sigParamTypes, "...")
				}

				callReg := g.nextReg()
				if retType == "void" {
					if len(sigParamTypes) > 0 {
						b.WriteString(fmt.Sprintf("  call void (%s) @%s(%s)\n", strings.Join(sigParamTypes, ", "), targetFn.Name, strings.Join(args, ", ")))
					} else {
						b.WriteString(fmt.Sprintf("  call void @%s(%s)\n", targetFn.Name, strings.Join(args, ", ")))
					}
					return "0", sema.TypeVoid
				} else {
					if len(sigParamTypes) > 0 {
						b.WriteString(fmt.Sprintf("  %s = call %s (%s) @%s(%s)\n", callReg, retType, strings.Join(sigParamTypes, ", "), targetFn.Name, strings.Join(args, ", ")))
					} else {
						b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", callReg, retType, targetFn.Name, strings.Join(args, ", ")))
					}
					return callReg, semaRet
				}
			}
		}

	case *ast.PrefixExpr:
		rightReg, rightType := g.resolveValue(b, e.Right)
		resReg := g.nextReg()

		switch e.Operator {
		case "-":
			b.WriteString(fmt.Sprintf("  %s = sub i64 0, %s\n", resReg, rightReg))
			return resReg, sema.TypeInt
		case "!":
			if rightType.LLVMType() == "i1" {
				b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", resReg, rightReg))
			} else {
				b.WriteString(fmt.Sprintf("  %s = icmp eq %s %s, 0\n", resReg, rightType.LLVMType(), rightReg))
			}
			return resReg, &sema.BasicType{Name: "bool", LLVM: "i1"}
		}

	case *ast.BinaryExpr:
		lhs, lhsType := g.resolveValue(b, e.Left)
		rhs, rhsType := g.resolveValue(b, e.Right)
		resReg := g.nextReg()

		if e.Operator == "&&" || e.Operator == "||" {
			lBool := lhs
			if lhsType.LLVMType() != "i1" {
				lBool = g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0\n", lBool, lhsType.LLVMType(), lhs))
			}
			rBool := rhs
			if rhsType.LLVMType() != "i1" {
				rBool = g.nextReg()
				b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, 0\n", rBool, rhsType.LLVMType(), rhs))
			}

			if e.Operator == "&&" {
				b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", resReg, lBool, rBool))
			} else {
				b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", resReg, lBool, rBool))
			}
			return resReg, &sema.BasicType{Name: "bool", LLVM: "i1"}
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
		case "<":
			b.WriteString(fmt.Sprintf("  %s = icmp slt i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, &sema.BasicType{Name: "bool", LLVM: "i1"}
		case ">":
			b.WriteString(fmt.Sprintf("  %s = icmp sgt i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, &sema.BasicType{Name: "bool", LLVM: "i1"}
		case "<=":
			b.WriteString(fmt.Sprintf("  %s = icmp sle i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, &sema.BasicType{Name: "bool", LLVM: "i1"}
		case ">=":
			b.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, %s\n", resReg, lhs, rhs))
			return resReg, &sema.BasicType{Name: "bool", LLVM: "i1"}
		case "==":
			cmpType := "i64"
			if lhsType != nil && lhsType.LLVMType() != "" && lhs != "null" {
				cmpType = lhsType.LLVMType()
			} else if rhsType != nil && rhsType.LLVMType() != "" && rhs != "null" {
				cmpType = rhsType.LLVMType()
			}
			b.WriteString(fmt.Sprintf("  %s = icmp eq %s %s, %s\n", resReg, cmpType, lhs, rhs))
			return resReg, &sema.BasicType{Name: "bool", LLVM: "i1"}
		case "!=":
			cmpType := "i64"
			if lhsType != nil && lhsType.LLVMType() != "" && lhs != "null" {
				cmpType = lhsType.LLVMType()
			} else if rhsType != nil && rhsType.LLVMType() != "" && rhs != "null" {
				cmpType = rhsType.LLVMType()
			}
			b.WriteString(fmt.Sprintf("  %s = icmp ne %s %s, %s\n", resReg, cmpType, lhs, rhs))
			return resReg, &sema.BasicType{Name: "bool", LLVM: "i1"}
		}
	}
	return "0", sema.TypeInt
}
