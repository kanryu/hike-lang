package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

// StmtLowererは文（Statement）の走査、制御フロー（CFG）のブロック分岐、代入・変数初期化を担当する
type StmtLowerer struct {
	root *Lowerer
}

func NewStmtLowerer(root *Lowerer) *StmtLowerer {
	return &StmtLowerer{root: root}
}

func (s *StmtLowerer) isStringAliasExpr(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.Identifier:
		if s.root.isStringType(s.root.symbolTypes[astIDValue(e)]) {
			return true
		}
		// Global variables are resolved through the semantic context rather
		// than the function-local symbol table.  They still participate in
		// string aliasing and therefore need the same retain treatment.
		return s.root.isStringType(s.root.semaCtx.Globals[astIDValue(e)])
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// 文 (Statement) のディスパッチ
// -----------------------------------------------------------------------------

func (s *StmtLowerer) LowerStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}
	restoreLocation := s.root.setTokenLocation(s.root.sourceFile, statementToken(stmt))
	defer restoreLocation()

	switch node := stmt.(type) {
	case *ast.TypeDecl:
		// Type declarations affect semantic resolution only. They emit no
		// runtime instructions when lowering a function body.
	case *ast.VarDecl:
		s.LowerVarDecl(node)
	case *ast.AssignStmt:
		s.LowerAssignStmt(node)
	case *ast.SendStmt:
		s.LowerSendStmt(node)
	case *ast.ExprStmt:
		s.root.Expr.LowerExpr(node.Expr)
	case *ast.BlockStmt:
		for _, inner := range node.Statements {
			s.LowerStmt(inner)
		}
	case *ast.IfStmt:
		s.LowerIfStmt(node)
	case *ast.ForStmt:
		s.LowerForStmt(node)
	case *ast.ForRangeStmt:
		s.LowerForRangeStmt(node)
	case *ast.SwitchStmt:
		s.LowerSwitchStmt(node)
	case *ast.TypeSwitchStmt:
		s.LowerTypeSwitchStmt(node)
	case *ast.ReturnStmt:
		s.LowerReturnStmt(node)
	case *ast.DeferStmt:
		if node.Call != nil {
			if s.root.curFunc != nil {
				s.root.curFunc.Defers = append(s.root.curFunc.Defers, hir.DeferEntry{
					ID:       len(s.root.curFunc.Defers),
					Location: s.root.sourceLoc,
				})
				if fn, ok := node.Call.Function.(*ast.FuncLit); ok && funcLitContainsRecover(fn) {
					s.root.curFunc.HasLocalRecover = true
				}
			}
			s.root.deferStack = append(s.root.deferStack, node.Call)
		}
	case *ast.LockStmt:
		s.root.emit(&hir.InstrLock{})
		if node.Body != nil {
			for _, inner := range node.Body.Statements {
				s.LowerStmt(inner)
			}
		}
		s.root.emit(&hir.InstrUnlock{})
	case *ast.AreaStmt:
		s.LowerAreaStmt(node)
	case *ast.BreakStmt:
		if len(s.root.loopStack) > 0 {
			ctx := s.root.loopStack[len(s.root.loopStack)-1]
			if ctx.structured {
				targetID := ctx.breakControlID
				targetLabel := ctx.breakLabel
				if ctx.breakTarget >= 0 {
					targetID = ctx.breakTarget
				}
				s.root.appendStructuredNode(&hir.BrNode{Target: targetLabel, TargetID: targetID})
			}
			s.root.terminate(&hir.InstrJump{Target: ctx.breakBlock.Label})
		}
	case *ast.ContinueStmt:
		if len(s.root.loopStack) > 0 {
			ctx := s.root.loopStack[len(s.root.loopStack)-1]
			if ctx.structured {
				targetID := ctx.continueControlID
				targetLabel := ctx.continueLabel
				if ctx.continueTarget >= 0 {
					targetID = ctx.continueTarget
				}
				s.root.appendStructuredNode(&hir.BrNode{Target: targetLabel, TargetID: targetID})
			}
			s.root.terminate(&hir.InstrJump{Target: ctx.continueBlock.Label})
		}
	}
}

// LowerAreaStmt lowers the lexical area scope. The optional source argument
// is expressed in KiB; the backend runtime applies its default when Size is
// nil. Allocations made while areaStack is non-empty are redirected by the
// common emitter path to the current area arena.
func (s *StmtLowerer) LowerAreaStmt(node *ast.AreaStmt) {
	if node == nil || node.Body == nil {
		return
	}
	var size hir.Value
	if node.Size != nil {
		size = s.root.Expr.LowerExpr(node.Size)
		if size != nil && size.Type() != sema.TypeInt {
			size = s.root.emitValueCoerce(size, sema.TypeInt)
		}
		bytes := s.root.nextReg(sema.TypeInt, "area_bytes")
		s.root.emit(&hir.InstrBinary{
			Dst: bytes,
			Op:  hir.OpMul,
			L:   size,
			R:   &hir.ConstInt{Val: 1024, Typ: sema.TypeInt},
		})
		size = bytes
	}
	areaPtr := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte}, "area")
	var parent hir.Value
	if len(s.root.areaStack) > 0 {
		parent = s.root.areaStack[len(s.root.areaStack)-1]
	}
	s.root.emit(&hir.InstrAreaBegin{Dst: areaPtr, Size: size, Parent: parent})
	s.root.areaStack = append(s.root.areaStack, areaPtr)
	for _, inner := range node.Body.Statements {
		s.LowerStmt(inner)
	}
	s.root.emit(&hir.InstrAreaEnd{Area: areaPtr})
	s.root.areaStack = s.root.areaStack[:len(s.root.areaStack)-1]
}

// funcLitContainsRecover recognizes only a recover written in the deferred
// function literal itself.  A call to another helper that eventually calls
// recover cannot safely identify the panic frame at lowering time and is
// intentionally treated as unsupported by the backends.
func funcLitContainsRecover(fn *ast.FuncLit) bool {
	if fn == nil {
		return false
	}
	return astStatementContainsRecover(fn.Body)
}

// astStatementContainsRecover walks the AST nodes that can occur inside a
// deferred function literal.  This is deliberately explicit instead of using
// reflection so the compiler remains usable without the reflect package.
func astStatementContainsRecover(stmt ast.Statement) bool {
	switch n := stmt.(type) {
	case *ast.BlockStmt:
		for _, child := range n.Statements {
			if astStatementContainsRecover(child) {
				return true
			}
		}
	case *ast.ExprStmt:
		return astExpressionContainsRecover(n.Expr)
	case *ast.AssignStmt:
		for _, expr := range append(n.Left, n.Right...) {
			if astExpressionContainsRecover(expr) {
				return true
			}
		}
	case *ast.VarDecl:
		return astExpressionContainsRecover(n.Value)
	case *ast.ReturnStmt:
		for _, expr := range n.Values {
			if astExpressionContainsRecover(expr) {
				return true
			}
		}
	case *ast.DeferStmt:
		return astExpressionContainsRecover(n.Call)
	case *ast.SendStmt:
		return astExpressionContainsRecover(n.Chan) || astExpressionContainsRecover(n.Value)
	case *ast.LockStmt:
		return astStatementContainsRecover(n.Body)
	case *ast.AreaStmt:
		return astExpressionContainsRecover(n.Size) || astStatementContainsRecover(n.Body)
	case *ast.IfStmt:
		return astStatementContainsRecover(n.Init) || astExpressionContainsRecover(n.Condition) ||
			astStatementContainsRecover(n.Consequence) || astStatementContainsRecover(n.Alternative)
	case *ast.ForStmt:
		return astStatementContainsRecover(n.Init) || astExpressionContainsRecover(n.Cond) ||
			astStatementContainsRecover(n.Post) || astStatementContainsRecover(n.Body)
	case *ast.ForRangeStmt:
		return astExpressionContainsRecover(n.Key) || astExpressionContainsRecover(n.Value) ||
			astExpressionContainsRecover(n.X) || astStatementContainsRecover(n.Body)
	case *ast.CaseClause:
		for _, expr := range n.Values {
			if astExpressionContainsRecover(expr) {
				return true
			}
		}
		for _, child := range n.Body {
			if astStatementContainsRecover(child) {
				return true
			}
		}
	case *ast.SwitchStmt:
		if astStatementContainsRecover(n.Init) || astExpressionContainsRecover(n.Value) {
			return true
		}
		for _, clause := range n.Cases {
			if astStatementContainsRecover(clause) {
				return true
			}
		}
	case *ast.TypeCaseClause:
		for _, child := range n.Body {
			if astStatementContainsRecover(child) {
				return true
			}
		}
	case *ast.TypeSwitchStmt:
		if astStatementContainsRecover(n.Init) || astExpressionContainsRecover(n.Expr) {
			return true
		}
		for _, clause := range n.Cases {
			if astStatementContainsRecover(clause) {
				return true
			}
		}
	}
	return false
}

func astExpressionContainsRecover(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	switch n := expr.(type) {
	case *ast.Identifier:
		return n.Value == "recover" || n.Value == "recover_cause" || n.Value == "recover_site"
	case *ast.CallExpr:
		if id, ok := n.Function.(*ast.Identifier); ok && (id.Value == "recover" || id.Value == "recover_cause" || id.Value == "recover_site") {
			return true
		}
		if astExpressionContainsRecover(n.Function) {
			return true
		}
		for _, arg := range n.Args {
			if astExpressionContainsRecover(arg) {
				return true
			}
		}
	case *ast.PrefixExpr:
		return astExpressionContainsRecover(n.Right)
	case *ast.ReceiveExpr:
		return astExpressionContainsRecover(n.Expr)
	case *ast.AsyncExpr:
		return astExpressionContainsRecover(n.Fn)
	case *ast.BinaryExpr:
		return astExpressionContainsRecover(n.Left) || astExpressionContainsRecover(n.Right)
	case *ast.IndexExpr:
		return astExpressionContainsRecover(n.Left) || astExpressionContainsRecover(n.Index)
	case *ast.GenericInstExpr:
		return astExpressionContainsRecover(n.Left)
	case *ast.MemberExpr:
		return astExpressionContainsRecover(n.Object)
	case *ast.InlineAsmExpr:
		for _, operand := range n.Operands {
			if astExpressionContainsRecover(operand) {
				return true
			}
		}
	case *ast.SliceExpr:
		return astExpressionContainsRecover(n.Left) || astExpressionContainsRecover(n.Low) || astExpressionContainsRecover(n.High)
	case *ast.SliceLiteral:
		for _, element := range n.Elements {
			if astExpressionContainsRecover(element) {
				return true
			}
		}
	case *ast.StructLiteral:
		for _, field := range n.Fields {
			if field != nil && astExpressionContainsRecover(field.Value) {
				return true
			}
		}
	case *ast.ArrayLiteral:
		for _, element := range n.Elements {
			if astExpressionContainsRecover(element) {
				return true
			}
		}
	case *ast.MapLiteral:
		for _, entry := range n.Entries {
			if entry != nil && (astExpressionContainsRecover(entry.Key) || astExpressionContainsRecover(entry.Value)) {
				return true
			}
		}
	case *ast.TypeAssertExpr:
		return astExpressionContainsRecover(n.Expr)
	case *ast.FuncLit:
		return astStatementContainsRecover(n.Body)
	}
	return false
}

func statementToken(stmt ast.Statement) token.Token {
	switch node := stmt.(type) {
	case *ast.VarDecl:
		return node.Token
	case *ast.AssignStmt:
		return node.Token
	case *ast.SendStmt:
		return node.Token
	case *ast.ExprStmt:
		return node.Token
	case *ast.BlockStmt:
		return node.Token
	case *ast.IfStmt:
		return node.Token
	case *ast.ForStmt:
		return node.Token
	case *ast.ForRangeStmt:
		return node.Token
	case *ast.SwitchStmt:
		return node.Token
	case *ast.TypeSwitchStmt:
		return node.Token
	case *ast.ReturnStmt:
		return node.Token
	case *ast.DeferStmt:
		return node.Token
	case *ast.AreaStmt:
		return node.Token
	case *ast.BreakStmt:
		return node.Token
	case *ast.ContinueStmt:
		return node.Token
	default:
		return token.Token{}
	}
}

// -----------------------------------------------------------------------------
// チャネル送信 (SendStmt)
// -----------------------------------------------------------------------------

func (s *StmtLowerer) LowerSendStmt(ss *ast.SendStmt) {
	chVal := s.root.Expr.LowerExpr(ss.Chan)
	val := s.root.Expr.LowerExpr(ss.Value)

	if ct, ok := chVal.Type().(*sema.ChanType); ok {
		val = s.root.emitValueCoerce(val, ct.Elem)
	}

	s.root.emit(&hir.InstrChanSend{
		Chan: chVal,
		Val:  val,
	})
}

// -------------------------------------------------------------
// 変数宣言 (VarDecl)
// -------------------------------------------------------------

func (s *StmtLowerer) LowerVarDecl(vd *ast.VarDecl) {
	var targetType sema.Type = nil
	if vd.Type != nil {
		targetType = s.root.semaCtx.ResolveType(vd.Type)
	}

	var val hir.Value = nil
	if vd.Value != nil {
		val = s.root.Expr.LowerExpr(vd.Value)
		if targetType == nil {
			targetType = val.Type()
		} else {
			val = s.root.emitValueCoerce(val, targetType)
		}
	} else if targetType != nil {
		val = s.root.defaultConstValue(targetType)
	} else {
		targetType = sema.TypeInt
		val = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
	}

	ptrReg := s.root.nextReg(&sema.PointerType{Base: targetType}, vd.Name.Value)
	keepStringOrSliceOnHeap := len(s.root.areaStack) > 0 && s.root.isAreaHeapValue(targetType)
	if vd.IsEscaped || s.root.escapedVars[vd.Name.Value] || keepStringOrSliceOnHeap {
		sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(targetType)), Typ: sema.TypeInt}
		s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: targetType, KeepOnHeapInArea: keepStringOrSliceOnHeap})
	} else {
		s.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: targetType})
	}

	s.root.symbols[vd.Name.Value] = ptrReg
	s.root.symbolTypes[vd.Name.Value] = targetType
	s.root.emit(&hir.InstrStore{Val: val, Ptr: ptrReg})
	if s.root.isStringType(targetType) && vd.Value != nil && s.isStringAliasExpr(vd.Value) {
		s.root.retainString(val)
	}
}

// -------------------------------------------------------------
// 代入文 (AssignStmt)
// -------------------------------------------------------------

func (s *StmtLowerer) lowerDefineAssignment(stmt *ast.AssignStmt, rhsVals []hir.Value) {
	for i, left := range stmt.Left {
		ident, ok := left.(*ast.Identifier)
		if !ok || astIDValue(ident) == "_" {
			continue
		}

		var val hir.Value
		var targetType sema.Type

		if i < len(rhsVals) {
			val = rhsVals[i]
		}

		// 単一受け取り時、右辺がタプルであれば先頭要素（インデックス0）を自動抽出
		// Some Go-shaped type-switch paths can produce a typed-nil HIR
		// register while their value is intentionally discarded.  An
		// interface containing (*hir.Reg)(nil) is not itself nil, so check
		// that case before calling Value.Type().
		if reg, isReg := val.(*hir.Reg); isReg && reg == nil {
			val = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
		}
		if len(stmt.Left) == 1 && val != nil {
			if tup, isTup := val.Type().(*sema.TupleType); isTup && len(tup.Types) > 0 {
				elemVal := s.root.nextReg(tup.Types[0])
				s.root.emit(&hir.InstrExtractValue{
					Dst:   elemVal,
					Agg:   val,
					Index: 0,
				})
				val = elemVal
			}
		}

		if stmt.Type != nil {
			targetType = s.root.semaCtx.ResolveType(stmt.Type)
			if val != nil {
				if iface, isIface := targetType.(*sema.InterfaceType); isIface {
					isZero := false
					if ci, okCi := val.(*hir.ConstInt); okCi && ci.Val == 0 {
						isZero = true
					}
					if isNilValue(val) || isZero {
						val = s.root.defaultConstValue(iface)
					} else {
						val = s.root.emitValueCoerce(val, targetType)
					}
				} else {
					val = s.root.emitValueCoerce(val, targetType)
				}
			} else {
				val = s.root.defaultConstValue(targetType)
			}
		} else if val != nil {
			targetType = val.Type()
		} else {
			targetType = sema.TypeInt
			val = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
		}

		// Top-level initializers are prepended to main, but they still need
		// to write the actual HIR global.  Lowering them as ordinary defines
		// creates a local with the same name and leaves the global at zero.
		if s.root.loweringGlobalInit {
			if globalType, ok := s.root.semaCtx.Globals[astIDValue(ident)]; ok {
				if targetType == nil {
					targetType = globalType
				}
				if val != nil {
					val = s.root.emitValueCoerce(val, globalType)
				}
				s.root.emit(&hir.InstrStore{
					Val: val,
					Ptr: &hir.GlobalVar{
						Name: astIDValue(ident),
						Typ:  &sema.PointerType{Base: globalType},
					},
				})
				continue
			}
		}

		ptrReg := s.root.nextReg(&sema.PointerType{Base: targetType}, astIDValue(ident))
		keepStringOrSliceOnHeap := len(s.root.areaStack) > 0 && s.root.isAreaHeapValue(targetType)
		if s.root.escapedVars[astIDValue(ident)] || keepStringOrSliceOnHeap {
			sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(targetType)), Typ: sema.TypeInt}
			s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: targetType, KeepOnHeapInArea: keepStringOrSliceOnHeap})
		} else {
			s.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: targetType})
		}
		s.root.symbols[astIDValue(ident)] = ptrReg
		s.root.symbolTypes[astIDValue(ident)] = targetType

		if val != nil {
			s.root.emit(&hir.InstrStore{Val: val, Ptr: ptrReg})
			if s.root.isStringType(targetType) && i < len(stmt.Right) && s.isStringAliasExpr(stmt.Right[i]) {
				s.root.retainString(val)
			}
		}
	}
}

func (s *StmtLowerer) LowerAssignStmt(stmt *ast.AssignStmt) {
	isDefine := (stmt.Token.Type == token.DEFINE) || (stmt.Token.Literal == ":=") ||
		(stmt.Token.Type == token.VAR) || (stmt.Token.Type == token.CONST) || (stmt.Token.Literal == "var") || (stmt.Type != nil)

	// 多値アンパック代入 (v, ok := expr)
	if len(stmt.Left) > 1 && len(stmt.Right) == 1 {
		if s.lowerTupleAssignment(stmt, isDefine) {
			return
		}
	}

	// 右辺の先行評価
	rhsVals := make([]hir.Value, len(stmt.Right))
	for i, r := range stmt.Right {
		s.annotateAssignmentLiteral(r, s.assignmentTargetType(stmt, i))
		rhsVals[i] = s.root.Expr.LowerExpr(r)
	}

	// 定義代入 (:=) または型付き変数宣言 (var)
	if isDefine {
		s.lowerDefineAssignment(stmt, rhsVals)
		return
	}

	// 通常代入 (=) および 演算子付き代入 (+=, -=, *=, ++, -- 等)
	op := stmt.TokenLiteral()

	for i, left := range stmt.Left {
		if ident, ok := left.(*ast.Identifier); ok && astIDValue(ident) == "_" {
			continue
		}

		// マップおよびコレクション構造体への添字代入 m[k] = v の判定
		if idxExpr, okIdx := left.(*ast.IndexExpr); okIdx {
			leftVal := s.root.Expr.LowerExpr(idxExpr.Left)
			leftType := leftVal.Type()

			// Strings are shared views.  Before writing through a string index,
			// detach the visible view so aliases (including substrings) remain
			// unchanged.  The replacement is stored in the original variable;
			// LowerLValue below then addresses the detached buffer.
			if s.root.isStringType(leftType) {
				if ident, isIdent := idxExpr.Left.(*ast.Identifier); isIdent {
					if basePtr, ok := s.root.symbols[astIDValue(ident)]; ok {
						backing, offset, length := s.root.stringViewParts(leftVal)
						writable := s.root.nextReg(sema.TypeString)
						s.root.emit(&hir.InstrCallStatic{Dst: writable, CalleeName: s.root.BuiltinName("__hike_string_writable"), Args: []hir.Value{backing, offset, length}})
						s.root.emit(&hir.InstrStore{Val: writable, Ptr: basePtr})
					}
				}
			}

			// 1. 組み込み map[K]V
			if mp, isMap := leftType.(*sema.MapType); isMap {
				keyVal := s.root.Expr.LowerExpr(idxExpr.Index)
				if i < len(rhsVals) {
					val := rhsVals[i]
					val = s.root.emitValueCoerce(val, mp.Value)
					keyI64 := s.root.coerceToI64(keyVal, mp.Key)
					valI64 := s.root.coerceToI64(val, mp.Value)
					s.root.emit(&hir.InstrCallStatic{
						CalleeName: "__hike_map_set",
						Args:       []hir.Value{leftVal, keyI64, valI64},
					})
				}
				continue
			}

			// 2. ユーザー定義コレクションへの添字代入 (Set(key, val) メソッド)
			objPtr := s.root.Expr.LowerStructPtr(idxExpr.Left)
			setFnName, setFn, finalRecv, found := s.root.Call.ResolveMethod(leftType, "Set", objPtr)
			if strings.Contains(semaTypeName(leftType), "__") {
				parts := strings.SplitN(strings.TrimPrefix(semaTypeName(leftType), "*"), "__", 2)
				baseName := parts[0]
				typeSuffix := parts[1]
				specSet := fmt.Sprintf("%s_Set_%s", baseName, typeSuffix)
				if fn, ok := s.root.semaCtx.Functions[specSet]; ok {
					setFnName = specSet
					setFn = fn
					found = true
				}
			}
			if !found && strings.Contains(semaTypeName(leftType), "__") {
				baseName := strings.Split(strings.TrimPrefix(semaTypeName(leftType), "*"), "__")[0]
				if st, _ := s.root.semaCtx.LookupStruct(baseName); st != nil {
					setFnName, setFn, finalRecv, found = s.root.Call.ResolveMethod(st, "Set", objPtr)
				}
			}

			if found && setFnName != "" && i < len(rhsVals) {
				if finalRecv == nil {
					finalRecv = objPtr
				}
				if setFn != nil && len(setFn.ParamTypes) > 0 {
					_, isPtrExpected := setFn.ParamTypes[0].(*sema.PointerType)
					_, isPtrActual := finalRecv.Type().(*sema.PointerType)
					if isPtrExpected && !isPtrActual {
						allocaTmp := s.root.nextReg(&sema.PointerType{Base: finalRecv.Type()})
						s.root.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: finalRecv.Type()})
						s.root.emit(&hir.InstrStore{Val: finalRecv, Ptr: allocaTmp})
						finalRecv = allocaTmp
					} else if !isPtrExpected && isPtrActual {
						ptrType := finalRecv.Type().(*sema.PointerType)
						loadReg := s.root.nextReg(ptrType.Base)
						s.root.emit(&hir.InstrLoad{Dst: loadReg, Ptr: finalRecv})
						finalRecv = loadReg
					}
				}

				keyVal := s.root.Expr.LowerExpr(idxExpr.Index)
				val := rhsVals[i]

				var keyArg, valArg hir.Value
				if setFn != nil && len(setFn.ParamTypes) >= 2 {
					kIdx := 1
					vIdx := 2
					if !setFn.IsMethod && len(setFn.ParamTypes) == 2 {
						kIdx = 0
						vIdx = 1
					}
					if len(setFn.ParamTypes) > vIdx {
						keyArg = s.root.emitValueCoerce(keyVal, setFn.ParamTypes[kIdx])
						valArg = s.root.emitValueCoerce(val, setFn.ParamTypes[vIdx])
					} else {
						keyArg = s.root.emitValueCoerce(keyVal, sema.TypeInt)
						valArg = val
					}
				} else {
					keyArg = s.root.emitValueCoerce(keyVal, sema.TypeInt)
					valArg = val
				}

				s.root.emit(&hir.InstrCallStatic{
					CalleeName: setFnName,
					Args:       []hir.Value{finalRecv, keyArg, valArg},
				})
				continue
			}

			// 3. 従来の MapBehavior (後方互換性)
			if _, _, isBeh := s.root.semaCtx.CheckMapBehavior(leftType); isBeh {
				setFnName, setFn, finalRecv, found := s.root.Call.ResolveMethod(leftType, "Set", objPtr)
				if found && setFn != nil && i < len(rhsVals) {
					if finalRecv == nil {
						finalRecv = objPtr
					}
					keyVal := s.root.Expr.LowerExpr(idxExpr.Index)
					val := rhsVals[i]
					keyArg := s.root.emitValueCoerce(keyVal, setFn.ParamTypes[1])
					valArg := s.root.emitValueCoerce(val, setFn.ParamTypes[2])
					s.root.emit(&hir.InstrCallStatic{
						CalleeName: setFnName,
						Args:       []hir.Value{finalRecv, keyArg, valArg},
					})
					continue
				}
			}
		}

		// 左辺値のアドレスを解決
		targetPtr := s.root.Expr.LowerLValue(left)

		if targetPtr != nil {
			var val hir.Value
			if i < len(rhsVals) {
				val = rhsVals[i]
			} else {
				val = &hir.ConstInt{Val: 1, Typ: sema.TypeInt}
			}
			if reg, isReg := val.(*hir.Reg); isReg && (reg == nil || reg.Typ == nil) {
				val = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
			}

			// 単一受け取り時、右辺がタプルであれば先頭要素（インデックス0）を自動抽出
			if len(stmt.Left) == 1 && val != nil {
				if tup, isTup := val.Type().(*sema.TupleType); isTup && len(tup.Types) > 0 {
					elemVal := s.root.nextReg(tup.Types[0])
					s.root.emit(&hir.InstrExtractValue{
						Dst:   elemVal,
						Agg:   val,
						Index: 0,
					})
					val = elemVal
				}
			}

			var elemType sema.Type = sema.TypeInt
			if pt, ok := targetPtr.Type().(*sema.PointerType); ok {
				elemType = pt.Base
			}
			val = s.root.emitValueCoerce(val, elemType)
			if s.root.isStringType(elemType) && op != "+=" {
				oldVal := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrLoad{Dst: oldVal, Ptr: targetPtr})
				s.root.releaseString(oldVal)
				if i < len(stmt.Right) && s.isStringAliasExpr(stmt.Right[i]) {
					s.root.retainString(val)
				}
			}

			switch op {
			case "+=", "++":
				curVal := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrLoad{Dst: curVal, Ptr: targetPtr})
				if op == "+=" && s.root.isStringType(elemType) {
					leftPtr, leftOffset, leftLen := s.root.stringViewParts(curVal)
					rightPtr, rightOffset, rightLen := s.root.stringViewParts(val)
					if s.shouldOptimizeStringAppend(left) {
						// Repeated local writes use the growth-buffer runtime.
						if s.root.is32Bit {
							raw := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
							s.root.emit(&hir.InstrCallStatic{Dst: raw, CalleeName: s.root.BuiltinName("__hike_string_append"), Args: []hir.Value{leftPtr, leftOffset, leftLen, rightPtr, rightOffset, rightLen}})
							length := s.root.nextReg(sema.TypeInt)
							s.root.emit(&hir.InstrCallStatic{Dst: length, CalleeName: s.root.BuiltinName("strlen"), Args: []hir.Value{raw}})
							val = s.root.makeString(raw, length)
						} else {
							raw := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
							s.root.emit(&hir.InstrCallStatic{Dst: raw, CalleeName: s.root.BuiltinName("__hike_string_append"), Args: []hir.Value{leftPtr, leftOffset, leftLen, rightPtr, rightOffset, rightLen}})
							length := s.root.nextReg(sema.TypeInt)
							s.root.emit(&hir.InstrCallStatic{Dst: length, CalleeName: s.root.BuiltinName("strlen"), Args: []hir.Value{raw}})
							val = s.root.makeString(raw, length)
						}
					} else {
						// Infrequent writes keep the exact-size concatenation path.
						raw := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
						s.root.emit(&hir.InstrCallStatic{Dst: raw, CalleeName: s.root.BuiltinName("hike_strcat_len"), Args: []hir.Value{leftPtr, leftLen, rightPtr, rightLen}})
						length := s.root.nextReg(sema.TypeInt)
						s.root.emit(&hir.InstrCallStatic{Dst: length, CalleeName: s.root.BuiltinName("strlen"), Args: []hir.Value{raw}})
						val = s.root.makeString(raw, length)
					}
					break
				}
				newVal := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrBinary{Dst: newVal, Op: hir.OpAdd, L: curVal, R: val})
				val = newVal
			case "-=", "--":
				curVal := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrLoad{Dst: curVal, Ptr: targetPtr})
				newVal := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrBinary{Dst: newVal, Op: hir.OpSub, L: curVal, R: val})
				val = newVal
			case "*=":
				curVal := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrLoad{Dst: curVal, Ptr: targetPtr})
				newVal := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrBinary{Dst: newVal, Op: hir.OpMul, L: curVal, R: val})
				val = newVal
			}

			s.root.emit(&hir.InstrStore{Val: val, Ptr: targetPtr})
		}
	}
}

func (s *StmtLowerer) shouldOptimizeStringAppend(left ast.Expression) bool {
	ident, ok := left.(*ast.Identifier)
	if !ok {
		return false
	}
	name := astIDValue(ident)
	return s.root.stringMutationCounts[name] >= 3 || s.root.stringMutationInLoop[name]
}

// assignmentTargetType returns the type already known for an assignment
// target.  Go permits eliding the type in a composite literal when the
// surrounding assignment supplies it (for example, x = {Field: value}).
// Recovering that context here keeps the parser conservative while ensuring
// lowering never receives an untyped struct literal.
func (s *StmtLowerer) assignmentTargetType(stmt *ast.AssignStmt, index int) sema.Type {
	if stmt == nil || index >= len(stmt.Left) {
		return nil
	}
	if stmt.Type != nil {
		return s.root.semaCtx.ResolveType(stmt.Type)
	}
	ident, ok := stmt.Left[index].(*ast.Identifier)
	if !ok || ident == nil {
		return nil
	}
	if ptr, exists := s.root.symbols[astIDValue(ident)]; exists && ptr != nil {
		if typ, okType := ptr.Type().(*sema.PointerType); okType {
			return typ.Base
		}
	}
	return s.root.symbolTypes[astIDValue(ident)]
}

func (s *StmtLowerer) annotateAssignmentLiteral(expr ast.Expression, target sema.Type) {
	if expr == nil || target == nil {
		return
	}
	switch literal := expr.(type) {
	case *ast.StructLiteral:
		if literal.Type == nil {
			if _, ok := target.(*sema.StructType); ok {
				if named, isNamed := semaTypeToTypeExpr(target).(*ast.NamedType); isNamed {
					literal.Type = named
				}
			}
		}
		if structType, ok := target.(*sema.StructType); ok {
			for _, field := range literal.Fields {
				if field == nil || field.Name == nil {
					continue
				}
				for _, declared := range structType.Fields {
					if declared.Name == field.Name.Value {
						s.annotateAssignmentLiteral(field.Value, declared.Type)
						break
					}
				}
			}
		}
	case *ast.ArrayLiteral:
		if arrayType, ok := target.(*sema.ArrayType); ok {
			for _, element := range literal.Elements {
				s.annotateAssignmentLiteral(element, arrayType.Elem)
			}
		}
	case *ast.SliceLiteral:
		if sliceType, ok := target.(*sema.SliceType); ok {
			for _, element := range literal.Elements {
				s.annotateAssignmentLiteral(element, sliceType.Elem)
			}
		}
	case *ast.MapLiteral:
		if mapType, ok := target.(*sema.MapType); ok {
			for _, entry := range literal.Entries {
				if entry != nil {
					s.annotateAssignmentLiteral(entry.Value, mapType.Value)
				}
			}
		}
	}
}

func (s *StmtLowerer) lowerTupleAssignment(stmt *ast.AssignStmt, isDefine bool) bool {
	var rhsVal hir.Value
	if tae, ok := stmt.Right[0].(*ast.TypeAssertExpr); ok {
		rhsVal = s.root.Expr.LowerTypeAssertExpr(tae)
	} else {
		rhsVal = s.root.Expr.LowerExpr(stmt.Right[0])
	}

	tup, ok := rhsVal.Type().(*sema.TupleType)
	if !ok {
		return false
	}
	for i, left := range stmt.Left {
		if i >= len(tup.Types) {
			break
		}
		elemType := tup.Types[i]
		elemVal := s.root.nextReg(elemType)
		s.root.emit(&hir.InstrExtractValue{Dst: elemVal, Agg: rhsVal, Index: i})

		if isDefine {
			s.defineTupleElement(left, elemType, elemVal)
			continue
		}
		if ident, okIdent := left.(*ast.Identifier); okIdent && astIDValue(ident) == "_" {
			continue
		}
		targetPtr := s.root.Expr.LowerLValue(left)
		if targetPtr == nil {
			continue
		}
		targetType := elemType
		if pt, okPt := targetPtr.Type().(*sema.PointerType); okPt {
			targetType = pt.Base
		}
		coerced := s.root.emitValueCoerce(elemVal, targetType)
		s.root.emit(&hir.InstrStore{Val: coerced, Ptr: targetPtr})
	}
	return true
}

func (s *StmtLowerer) defineTupleElement(left ast.Expression, elemType sema.Type, elemVal hir.Value) {
	ident, ok := left.(*ast.Identifier)
	if !ok || astIDValue(ident) == "_" {
		return
	}
	name := astIDValue(ident)
	ptrReg := s.root.nextReg(&sema.PointerType{Base: elemType}, name)
	keepStringOrSliceOnHeap := len(s.root.areaStack) > 0 && s.root.isAreaHeapValue(elemType)
	if s.root.escapedVars[name] || keepStringOrSliceOnHeap {
		sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(elemType)), Typ: sema.TypeInt}
		s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: elemType, KeepOnHeapInArea: keepStringOrSliceOnHeap})
	} else {
		s.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: elemType})
	}
	s.root.emit(&hir.InstrStore{Val: elemVal, Ptr: ptrReg})
	s.root.symbols[name] = ptrReg
	s.root.symbolTypes[name] = elemType
}

// -------------------------------------------------------------
// 制御構文 (If / For / ForRange / Switch / Return)
// -------------------------------------------------------------
