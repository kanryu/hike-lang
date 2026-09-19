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
			s.root.deferStack = append(s.root.deferStack, node.Call)
		}
	case *ast.BreakStmt:
		if len(s.root.loopStack) > 0 {
			ctx := s.root.loopStack[len(s.root.loopStack)-1]
			s.root.terminate(&hir.InstrJump{Target: ctx.breakBlock.Label})
		}
	case *ast.ContinueStmt:
		if len(s.root.loopStack) > 0 {
			ctx := s.root.loopStack[len(s.root.loopStack)-1]
			s.root.terminate(&hir.InstrJump{Target: ctx.continueBlock.Label})
		}
	}
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
	if vd.IsEscaped || s.root.escapedVars[vd.Name.Value] {
		sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(targetType)), Typ: sema.TypeInt}
		s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: targetType})
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

func (s *StmtLowerer) LowerAssignStmt(stmt *ast.AssignStmt) {
	isDefine := (stmt.Token.Type == token.DEFINE) || (stmt.Token.Literal == ":=") ||
		(stmt.Token.Type == token.VAR) || (stmt.Token.Literal == "var") || (stmt.Type != nil)

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

			ptrReg := s.root.nextReg(&sema.PointerType{Base: targetType}, astIDValue(ident))
			if s.root.escapedVars[astIDValue(ident)] {
				sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(targetType)), Typ: sema.TypeInt}
				s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: targetType})
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
			if s.root.isStringType(elemType) {
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
	if s.root.escapedVars[name] {
		sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(elemType)), Typ: sema.TypeInt}
		s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: elemType})
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

func (s *StmtLowerer) LowerIfStmt(is *ast.IfStmt) {
	if is.Init != nil {
		s.LowerStmt(is.Init)
	}

	condVal := s.root.Expr.LowerExpr(is.Condition)
	thenBB := s.root.newBlock("if.then")
	elseBB := s.root.newBlock("if.else")
	endBB := s.root.newBlock("if.end")

	if is.Alternative != nil {
		s.root.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: thenBB.Label, ElseTarget: elseBB.Label})
	} else {
		s.root.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: thenBB.Label, ElseTarget: endBB.Label})
	}

	s.root.setBlock(thenBB)
	s.LowerStmt(is.Consequence)
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: endBB.Label})
	}

	if is.Alternative != nil {
		s.root.setBlock(elseBB)
		s.LowerStmt(is.Alternative)
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}
	}

	s.root.setBlock(endBB)
}

func (s *StmtLowerer) LowerForStmt(fs *ast.ForStmt) {
	if fs.Init != nil {
		s.LowerStmt(fs.Init)
	}

	condBB := s.root.newBlock("for.cond")
	bodyBB := s.root.newBlock("for.body")
	postBB := s.root.newBlock("for.post")
	endBB := s.root.newBlock("for.end")

	s.root.loopStack = append(s.root.loopStack, loopContext{breakBlock: endBB, continueBlock: postBB})
	defer func() {
		s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
	}()

	s.root.terminate(&hir.InstrJump{Target: condBB.Label})

	s.root.setBlock(condBB)
	if fs.Cond != nil {
		condVal := s.root.Expr.LowerExpr(fs.Cond)
		s.root.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})
	} else {
		s.root.terminate(&hir.InstrJump{Target: bodyBB.Label})
	}

	s.root.setBlock(bodyBB)
	s.LowerStmt(fs.Body)
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: postBB.Label})
	}

	s.root.setBlock(postBB)
	if fs.Post != nil {
		s.LowerStmt(fs.Post)
	}
	s.root.terminate(&hir.InstrJump{Target: condBB.Label})

	s.root.setBlock(endBB)
}

func (s *StmtLowerer) LowerForRangeStmt(fr *ast.ForRangeStmt) {
	targetExpr := fr.X
	var isAsyncRecv bool = false
	if recvExpr, okRecv := fr.X.(*ast.ReceiveExpr); okRecv {
		isAsyncRecv = true
		targetExpr = recvExpr.Expr
	}

	xVal := s.root.Expr.LowerExpr(targetExpr)
	xType := xVal.Type()

	// 1. AsyncIterable (for v := range <-stream)
	if isAsyncRecv {
		var hasInit, hasNextChan bool
		var initFnName, nextChanFnName string
		var nextChanFn *sema.FuncType
		var finalRecv hir.Value

		objPtr := s.root.Expr.LowerStructPtr(targetExpr)
		initFnName, _, finalRecv, hasInit = s.root.Call.ResolveMethod(xType, "InitIterator", objPtr)
		nextChanFnName, nextChanFn, _, hasNextChan = s.root.Call.ResolveMethod(xType, "NextChannel", objPtr)

		if strings.Contains(semaTypeName(xType), "__") {
			parts := strings.SplitN(strings.TrimPrefix(semaTypeName(xType), "*"), "__", 2)
			baseName := parts[0]
			typeSuffix := parts[1]
			specInit := fmt.Sprintf("%s_InitIterator_%s", baseName, typeSuffix)
			specNextChan := fmt.Sprintf("%s_NextChannel_%s", baseName, typeSuffix)
			if fn, ok := s.root.semaCtx.Functions[specInit]; ok {
				initFnName = specInit
				hasInit = true
				_ = fn
			}
			if fn, ok := s.root.semaCtx.Functions[specNextChan]; ok {
				nextChanFnName = specNextChan
				nextChanFn = fn
				hasNextChan = true
			}
		}

		if (!hasInit || !hasNextChan) && strings.Contains(semaTypeName(xType), "__") {
			baseName := strings.Split(strings.TrimPrefix(semaTypeName(xType), "*"), "__")[0]
			if st, _ := s.root.semaCtx.LookupStruct(baseName); st != nil {
				if !hasInit {
					initFnName, _, finalRecv, hasInit = s.root.Call.ResolveMethod(st, "InitIterator", objPtr)
				}
				if !hasNextChan {
					nextChanFnName, nextChanFn, _, hasNextChan = s.root.Call.ResolveMethod(st, "NextChannel", objPtr)
				}
			}
		}

		if hasInit && hasNextChan && nextChanFn != nil {
			if finalRecv == nil {
				finalRecv = objPtr
			}
			if initFnMeta := s.root.semaCtx.Functions[initFnName]; initFnMeta != nil && len(initFnMeta.ParamTypes) > 0 {
				_, isPtrExpected := initFnMeta.ParamTypes[0].(*sema.PointerType)
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

			sizeReg := s.root.nextReg(sema.TypeInt)
			s.root.emit(&hir.InstrCallStatic{Dst: sizeReg, CalleeName: initFnName, Args: []hir.Value{finalRecv, &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}}})

			bufReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			s.root.emit(&hir.InstrAllocaDynamic{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte})
			s.root.emit(&hir.InstrCallStatic{CalleeName: initFnName, Args: []hir.Value{finalRecv, bufReg}})

			var elemType sema.Type = sema.TypeInt
			if len(nextChanFn.ReturnTypes) >= 1 {
				ret0 := nextChanFn.ReturnTypes[0]
				if ch, ok := ret0.(*sema.ChanType); ok {
					elemType = ch.Elem
				} else if tup, ok := ret0.(*sema.TupleType); ok && len(tup.Types) > 0 {
					if ch, okCh := tup.Types[0].(*sema.ChanType); okCh {
						elemType = ch.Elem
					}
				}
			}

			var oldKeySym, oldValSym hir.Value
			var oldKeyTyp, oldValTyp sema.Type
			var hasOldKey, hasOldVal bool

			var kPtr, vPtr hir.Value
			if fr.Key != nil && fr.Value != nil {
				if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
					oldKeySym, hasOldKey = s.root.symbols[astIDValue(kId)]
					oldKeyTyp = s.root.symbolTypes[astIDValue(kId)]

					kReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, astIDValue(kId))
					s.root.emit(&hir.InstrAlloca{Dst: kReg, AllocType: sema.TypeInt})
					kPtr = kReg
					s.root.symbols[astIDValue(kId)] = kPtr
					s.root.symbolTypes[astIDValue(kId)] = sema.TypeInt
				}
				if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
					oldValSym, hasOldVal = s.root.symbols[astIDValue(vId)]
					oldValTyp = s.root.symbolTypes[astIDValue(vId)]

					vReg := s.root.nextReg(&sema.PointerType{Base: elemType}, astIDValue(vId))
					s.root.emit(&hir.InstrAlloca{Dst: vReg, AllocType: elemType})
					vPtr = vReg
					s.root.symbols[astIDValue(vId)] = vPtr
					s.root.symbolTypes[astIDValue(vId)] = elemType
				}
			} else if fr.Key != nil {
				if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
					oldKeySym, hasOldKey = s.root.symbols[astIDValue(kId)]
					oldKeyTyp = s.root.symbolTypes[astIDValue(kId)]

					kReg := s.root.nextReg(&sema.PointerType{Base: elemType}, astIDValue(kId))
					s.root.emit(&hir.InstrAlloca{Dst: kReg, AllocType: elemType})
					kPtr = kReg
					s.root.symbols[astIDValue(kId)] = kPtr
					s.root.symbolTypes[astIDValue(kId)] = elemType
				}
			}

			idxAlloca := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "asynciter.idx")
			s.root.emit(&hir.InstrAlloca{Dst: idxAlloca, AllocType: sema.TypeInt})
			s.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: idxAlloca})

			condBB := s.root.newBlock("asynciter.cond")
			bodyBB := s.root.newBlock("asynciter.body")
			postBB := s.root.newBlock("asynciter.post")
			endBB := s.root.newBlock("asynciter.end")

			s.root.loopStack = append(s.root.loopStack, loopContext{breakBlock: endBB, continueBlock: postBB})
			defer func() {
				s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
			}()

			s.root.terminate(&hir.InstrJump{Target: condBB.Label})

			s.root.setBlock(condBB)
			var retTupleType sema.Type
			if len(nextChanFn.ReturnTypes) == 1 {
				retTupleType = nextChanFn.ReturnTypes[0]
			} else {
				retTupleType = &sema.TupleType{Types: nextChanFn.ReturnTypes}
			}
			nextRes := s.root.nextReg(retTupleType)
			s.root.emit(&hir.InstrCallStatic{Dst: nextRes, CalleeName: nextChanFnName, Args: []hir.Value{finalRecv, bufReg}})
			okReg := s.root.nextReg(sema.TypeBool)
			s.root.emit(&hir.InstrExtractValue{Dst: okReg, Agg: nextRes, Index: 1})
			s.root.terminate(&hir.InstrBranch{Cond: okReg, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})

			s.root.setBlock(bodyBB)
			chReg := s.root.nextReg(&sema.ChanType{Elem: elemType})
			s.root.emit(&hir.InstrExtractValue{Dst: chReg, Agg: nextRes, Index: 0})

			// チャネルから要素を受信待機
			tmpAlloca := s.root.nextReg(&sema.PointerType{Base: elemType})
			s.root.emit(&hir.InstrAlloca{Dst: tmpAlloca, AllocType: elemType})
			chRaw := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			s.root.emit(&hir.InstrCast{Dst: chRaw, Val: chReg, ToType: &sema.PointerType{Base: sema.TypeByte}})
			tmpRaw := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			s.root.emit(&hir.InstrCast{Dst: tmpRaw, Val: tmpAlloca, ToType: &sema.PointerType{Base: sema.TypeByte}})
			s.root.emit(&hir.InstrCallStatic{CalleeName: "__hike_chan_recv", Args: []hir.Value{chRaw, tmpRaw}})
			elemVal := s.root.nextReg(elemType)
			s.root.emit(&hir.InstrLoad{Dst: elemVal, Ptr: tmpAlloca})

			curIdx := s.root.nextReg(sema.TypeInt)
			s.root.emit(&hir.InstrLoad{Dst: curIdx, Ptr: idxAlloca})

			if fr.Key != nil && fr.Value != nil {
				if kPtr != nil {
					s.root.emit(&hir.InstrStore{Val: curIdx, Ptr: kPtr})
				}
				if vPtr != nil {
					s.root.emit(&hir.InstrStore{Val: elemVal, Ptr: vPtr})
				}
			} else if fr.Key != nil {
				if kPtr != nil {
					s.root.emit(&hir.InstrStore{Val: elemVal, Ptr: kPtr})
				}
			}

			s.LowerStmt(fr.Body)
			if s.root.curBlock.Terminator == nil {
				s.root.terminate(&hir.InstrJump{Target: postBB.Label})
			}

			s.root.setBlock(postBB)
			incIdx := s.root.nextReg(sema.TypeInt)
			s.root.emit(&hir.InstrBinary{Dst: incIdx, Op: hir.OpAdd, L: curIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
			s.root.emit(&hir.InstrStore{Val: incIdx, Ptr: idxAlloca})
			s.root.terminate(&hir.InstrJump{Target: condBB.Label})

			s.root.setBlock(endBB)
			if fr.Key != nil {
				if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
					if hasOldKey {
						s.root.symbols[astIDValue(kId)] = oldKeySym
						s.root.symbolTypes[astIDValue(kId)] = oldKeyTyp
					} else {
						delete(s.root.symbols, astIDValue(kId))
						delete(s.root.symbolTypes, astIDValue(kId))
					}
				}
			}
			if fr.Value != nil {
				if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
					if hasOldVal {
						s.root.symbols[astIDValue(vId)] = oldValSym
						s.root.symbolTypes[astIDValue(vId)] = oldValTyp
					} else {
						delete(s.root.symbols, astIDValue(vId))
						delete(s.root.symbolTypes, astIDValue(vId))
					}
				}
			}
			return
		}
	}

	// 2. ユーザー定義コレクション (Iterable / MapBehavior: InitIterator + Next) の走査
	var hasInit, hasNext bool
	var initFnName, nextFnName string
	var nextFn *sema.FuncType
	var finalRecv hir.Value

	objPtr := s.root.Expr.LowerStructPtr(targetExpr)
	initFnName, _, finalRecv, hasInit = s.root.Call.ResolveMethod(xType, "InitIterator", objPtr)
	nextFnName, nextFn, _, hasNext = s.root.Call.ResolveMethod(xType, "Next", objPtr)

	if strings.Contains(semaTypeName(xType), "__") {
		parts := strings.SplitN(strings.TrimPrefix(semaTypeName(xType), "*"), "__", 2)
		baseName := parts[0]
		typeSuffix := parts[1]
		specInit := fmt.Sprintf("%s_InitIterator_%s", baseName, typeSuffix)
		specNext := fmt.Sprintf("%s_Next_%s", baseName, typeSuffix)
		if fn, ok := s.root.semaCtx.Functions[specInit]; ok {
			initFnName = specInit
			hasInit = true
			_ = fn
		}
		if fn, ok := s.root.semaCtx.Functions[specNext]; ok {
			nextFnName = specNext
			nextFn = fn
			hasNext = true
		}
	}

	if (!hasInit || !hasNext) && strings.Contains(semaTypeName(xType), "__") {
		baseName := strings.Split(strings.TrimPrefix(semaTypeName(xType), "*"), "__")[0]
		if st, _ := s.root.semaCtx.LookupStruct(baseName); st != nil {
			if !hasInit {
				initFnName, _, finalRecv, hasInit = s.root.Call.ResolveMethod(st, "InitIterator", objPtr)
			}
			if !hasNext {
				nextFnName, nextFn, _, hasNext = s.root.Call.ResolveMethod(st, "Next", objPtr)
			}
		}
	}

	if hasInit && hasNext && initFnName != "" && nextFnName != "" {
		if finalRecv == nil {
			finalRecv = objPtr
		}
		if initFnMeta := s.root.semaCtx.Functions[initFnName]; initFnMeta != nil && len(initFnMeta.ParamTypes) > 0 {
			_, isPtrExpected := initFnMeta.ParamTypes[0].(*sema.PointerType)
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

		sizeReg := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrCallStatic{Dst: sizeReg, CalleeName: initFnName, Args: []hir.Value{finalRecv, &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}}})

		bufReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrAllocaDynamic{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte})
		s.root.emit(&hir.InstrCallStatic{CalleeName: initFnName, Args: []hir.Value{finalRecv, bufReg}})

		var retTupleType sema.Type
		var retTypes []sema.Type
		if nextFn != nil && len(nextFn.ReturnTypes) > 0 {
			if len(nextFn.ReturnTypes) == 1 {
				retTupleType = nextFn.ReturnTypes[0]
				if tup, okTup := retTupleType.(*sema.TupleType); okTup {
					retTypes = tup.Types
				} else {
					retTypes = []sema.Type{retTupleType}
				}
			} else {
				retTupleType = &sema.TupleType{Types: nextFn.ReturnTypes}
				retTypes = nextFn.ReturnTypes
			}
		} else {
			// デフォルト推論: (val, ok)
			retTypes = []sema.Type{sema.TypeInt, sema.TypeBool}
			retTupleType = &sema.TupleType{Types: retTypes}
		}

		numReturns := len(retTypes)
		okIndex := numReturns - 1

		var elemType sema.Type = sema.TypeInt
		if numReturns == 2 {
			elemType = retTypes[0]
			if pt, isPtr := elemType.(*sema.PointerType); isPtr {
				elemType = pt.Base
			}
		} else if numReturns >= 3 {
			elemType = retTypes[1]
			if pt, isPtr := elemType.(*sema.PointerType); isPtr {
				elemType = pt.Base
			}
		}

		var oldKeySym, oldValSym hir.Value
		var oldKeyTyp, oldValTyp sema.Type
		var hasOldKey, hasOldVal bool

		var kPtr, vPtr hir.Value
		if numReturns >= 3 {
			// (key, val, ok) の Map スタイル
			keyType := retTypes[0]
			if pt, isPtr := keyType.(*sema.PointerType); isPtr {
				keyType = pt.Base
			}
			valType := retTypes[1]
			if pt, isPtr := valType.(*sema.PointerType); isPtr {
				valType = pt.Base
			}

			if fr.Key != nil && fr.Value != nil {
				if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
					oldKeySym, hasOldKey = s.root.symbols[astIDValue(kId)]
					oldKeyTyp = s.root.symbolTypes[astIDValue(kId)]

					kReg := s.root.nextReg(&sema.PointerType{Base: keyType}, astIDValue(kId))
					s.root.emit(&hir.InstrAlloca{Dst: kReg, AllocType: keyType})
					kPtr = kReg
					s.root.symbols[astIDValue(kId)] = kPtr
					s.root.symbolTypes[astIDValue(kId)] = keyType
				}
				if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
					oldValSym, hasOldVal = s.root.symbols[astIDValue(vId)]
					oldValTyp = s.root.symbolTypes[astIDValue(vId)]

					vReg := s.root.nextReg(&sema.PointerType{Base: valType}, astIDValue(vId))
					s.root.emit(&hir.InstrAlloca{Dst: vReg, AllocType: valType})
					vPtr = vReg
					s.root.symbols[astIDValue(vId)] = vPtr
					s.root.symbolTypes[astIDValue(vId)] = valType
				}
			} else if fr.Key != nil {
				if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
					oldKeySym, hasOldKey = s.root.symbols[astIDValue(kId)]
					oldKeyTyp = s.root.symbolTypes[astIDValue(kId)]

					kReg := s.root.nextReg(&sema.PointerType{Base: keyType}, astIDValue(kId))
					s.root.emit(&hir.InstrAlloca{Dst: kReg, AllocType: keyType})
					kPtr = kReg
					s.root.symbols[astIDValue(kId)] = kPtr
					s.root.symbolTypes[astIDValue(kId)] = keyType
				}
			}
		} else {
			// (elem, ok) のコレクション・リストスタイル
			if fr.Key != nil && fr.Value != nil {
				// 2変数の場合: 1つ目はインデックス (int), 2つ目は値 (elemType)
				if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
					oldKeySym, hasOldKey = s.root.symbols[astIDValue(kId)]
					oldKeyTyp = s.root.symbolTypes[astIDValue(kId)]

					kReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, astIDValue(kId))
					s.root.emit(&hir.InstrAlloca{Dst: kReg, AllocType: sema.TypeInt})
					kPtr = kReg
					s.root.symbols[astIDValue(kId)] = kPtr
					s.root.symbolTypes[astIDValue(kId)] = sema.TypeInt
				}
				if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
					oldValSym, hasOldVal = s.root.symbols[astIDValue(vId)]
					oldValTyp = s.root.symbolTypes[astIDValue(vId)]

					vReg := s.root.nextReg(&sema.PointerType{Base: elemType}, astIDValue(vId))
					s.root.emit(&hir.InstrAlloca{Dst: vReg, AllocType: elemType})
					vPtr = vReg
					s.root.symbols[astIDValue(vId)] = vPtr
					s.root.symbolTypes[astIDValue(vId)] = elemType
				}
			} else if fr.Key != nil {
				// 1変数の場合: コレクション走査では値 (elemType) を直接受け取る
				if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
					oldKeySym, hasOldKey = s.root.symbols[astIDValue(kId)]
					oldKeyTyp = s.root.symbolTypes[astIDValue(kId)]

					kReg := s.root.nextReg(&sema.PointerType{Base: elemType}, astIDValue(kId))
					s.root.emit(&hir.InstrAlloca{Dst: kReg, AllocType: elemType})
					kPtr = kReg
					s.root.symbols[astIDValue(kId)] = kPtr
					s.root.symbolTypes[astIDValue(kId)] = elemType
				}
			}
		}

		idxAlloca := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "iter.idx")
		s.root.emit(&hir.InstrAlloca{Dst: idxAlloca, AllocType: sema.TypeInt})
		s.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: idxAlloca})

		condBB := s.root.newBlock("iter.cond")
		bodyBB := s.root.newBlock("iter.body")
		postBB := s.root.newBlock("iter.post")
		endBB := s.root.newBlock("iter.end")

		s.root.loopStack = append(s.root.loopStack, loopContext{breakBlock: endBB, continueBlock: postBB})
		defer func() {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		}()

		s.root.terminate(&hir.InstrJump{Target: condBB.Label})

		s.root.setBlock(condBB)
		nextRes := s.root.nextReg(retTupleType)
		s.root.emit(&hir.InstrCallStatic{Dst: nextRes, CalleeName: nextFnName, Args: []hir.Value{finalRecv, bufReg}})
		okReg := s.root.nextReg(sema.TypeBool)
		s.root.emit(&hir.InstrExtractValue{Dst: okReg, Agg: nextRes, Index: okIndex})
		s.root.terminate(&hir.InstrBranch{Cond: okReg, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})

		s.root.setBlock(bodyBB)
		curIdx := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrLoad{Dst: curIdx, Ptr: idxAlloca})

		if numReturns >= 3 {
			// MapBehavior: Index 0=Key, Index 1=Val
			if kPtr != nil {
				kPtrReg := s.root.nextReg(retTypes[0])
				s.root.emit(&hir.InstrExtractValue{Dst: kPtrReg, Agg: nextRes, Index: 0})
				if pt, isPtr := retTypes[0].(*sema.PointerType); isPtr {
					kValReg := s.root.nextReg(pt.Base)
					s.root.emit(&hir.InstrLoad{Dst: kValReg, Ptr: kPtrReg})
					s.root.emit(&hir.InstrStore{Val: kValReg, Ptr: kPtr})
				} else {
					s.root.emit(&hir.InstrStore{Val: kPtrReg, Ptr: kPtr})
				}
			}
			if vPtr != nil {
				vPtrReg := s.root.nextReg(retTypes[1])
				s.root.emit(&hir.InstrExtractValue{Dst: vPtrReg, Agg: nextRes, Index: 1})
				if pt, isPtr := retTypes[1].(*sema.PointerType); isPtr {
					vValReg := s.root.nextReg(pt.Base)
					s.root.emit(&hir.InstrLoad{Dst: vValReg, Ptr: vPtrReg})
					s.root.emit(&hir.InstrStore{Val: vValReg, Ptr: vPtr})
				} else {
					s.root.emit(&hir.InstrStore{Val: vPtrReg, Ptr: vPtr})
				}
			}
		} else {
			// Collection/List: Index 0=Elem
			var elemVal hir.Value
			ret0Type := retTypes[0]
			if pt, isPtr := ret0Type.(*sema.PointerType); isPtr && elemType != nil && semaTypeName(pt.Base) == semaTypeName(elemType) {
				pElem := s.root.nextReg(ret0Type)
				s.root.emit(&hir.InstrExtractValue{Dst: pElem, Agg: nextRes, Index: 0})
				valReg := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrLoad{Dst: valReg, Ptr: pElem})
				elemVal = valReg
			} else {
				valReg := s.root.nextReg(ret0Type)
				s.root.emit(&hir.InstrExtractValue{Dst: valReg, Agg: nextRes, Index: 0})
				elemVal = valReg
			}

			if fr.Key != nil && fr.Value != nil {
				if kPtr != nil {
					s.root.emit(&hir.InstrStore{Val: curIdx, Ptr: kPtr})
				}
				if vPtr != nil {
					s.root.emit(&hir.InstrStore{Val: elemVal, Ptr: vPtr})
				}
			} else if fr.Key != nil {
				if kPtr != nil {
					s.root.emit(&hir.InstrStore{Val: elemVal, Ptr: kPtr})
				}
			}
		}

		s.LowerStmt(fr.Body)
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: postBB.Label})
		}

		s.root.setBlock(postBB)
		incIdx := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrBinary{Dst: incIdx, Op: hir.OpAdd, L: curIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
		s.root.emit(&hir.InstrStore{Val: incIdx, Ptr: idxAlloca})
		s.root.terminate(&hir.InstrJump{Target: condBB.Label})

		s.root.setBlock(endBB)
		if fr.Key != nil {
			if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
				if hasOldKey {
					s.root.symbols[astIDValue(kId)] = oldKeySym
					s.root.symbolTypes[astIDValue(kId)] = oldKeyTyp
				} else {
					delete(s.root.symbols, astIDValue(kId))
					delete(s.root.symbolTypes, astIDValue(kId))
				}
			}
		}
		if fr.Value != nil {
			if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
				if hasOldVal {
					s.root.symbols[astIDValue(vId)] = oldValSym
					s.root.symbolTypes[astIDValue(vId)] = oldValTyp
				} else {
					delete(s.root.symbols, astIDValue(vId))
					delete(s.root.symbolTypes, astIDValue(vId))
				}
			}
		}
		return
	}

	// 3. 組み込み map[K]V の走査
	if mp, isMap := xType.(*sema.MapType); isMap {
		mapStructType := &sema.StructType{Name: "__hike_map"}
		mapPtrType := &sema.PointerType{Base: mapStructType}
		entryStructType := &sema.StructType{Name: "__hike_map_entry"}
		entryPtrType := &sema.PointerType{Base: entryStructType}

		typedMap := s.root.nextReg(mapPtrType)
		s.root.emit(&hir.InstrCast{Dst: typedMap, Val: xVal, ToType: mapPtrType})

		bIdxAlloca := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "maprange.bidx")
		s.root.emit(&hir.InstrAlloca{Dst: bIdxAlloca, AllocType: sema.TypeInt})
		s.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: bIdxAlloca})

		entryAlloca := s.root.nextReg(&sema.PointerType{Base: entryPtrType}, "maprange.cur")
		s.root.emit(&hir.InstrAlloca{Dst: entryAlloca, AllocType: entryPtrType})

		pBuckets := s.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: entryPtrType}})
		s.root.emit(&hir.InstrGetFieldPtr{Dst: pBuckets, BasePtr: typedMap, FieldIndex: 0, FieldName: "buckets"})
		buckets := s.root.nextReg(&sema.PointerType{Base: entryPtrType})
		s.root.emit(&hir.InstrLoad{Dst: buckets, Ptr: pBuckets})

		pNumBuckets := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
		s.root.emit(&hir.InstrGetFieldPtr{Dst: pNumBuckets, BasePtr: typedMap, FieldIndex: 1, FieldName: "numBuckets"})
		numBuckets := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrLoad{Dst: numBuckets, Ptr: pNumBuckets})

		var oldKeySym, oldValSym hir.Value
		var oldKeyTyp, oldValTyp sema.Type
		var hasOldKey, hasOldVal bool

		var kPtr, vPtr hir.Value
		if fr.Key != nil {
			if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
				oldKeySym, hasOldKey = s.root.symbols[astIDValue(kId)]
				oldKeyTyp = s.root.symbolTypes[astIDValue(kId)]

				kReg := s.root.nextReg(&sema.PointerType{Base: mp.Key}, astIDValue(kId))
				s.root.emit(&hir.InstrAlloca{Dst: kReg, AllocType: mp.Key})
				kPtr = kReg
				s.root.symbols[astIDValue(kId)] = kPtr
				s.root.symbolTypes[astIDValue(kId)] = mp.Key
			}
		}
		if fr.Value != nil {
			if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
				oldValSym, hasOldVal = s.root.symbols[astIDValue(vId)]
				oldValTyp = s.root.symbolTypes[astIDValue(vId)]

				vReg := s.root.nextReg(&sema.PointerType{Base: mp.Value}, astIDValue(vId))
				s.root.emit(&hir.InstrAlloca{Dst: vReg, AllocType: mp.Value})
				vPtr = vReg
				s.root.symbols[astIDValue(vId)] = vPtr
				s.root.symbolTypes[astIDValue(vId)] = mp.Value
			}
		}

		bCondBB := s.root.newBlock("maprange.bcond")
		bBodyBB := s.root.newBlock("maprange.bbody")
		bPostBB := s.root.newBlock("maprange.bpost")
		eCondBB := s.root.newBlock("maprange.econd")
		eBodyBB := s.root.newBlock("maprange.ebody")
		ePostBB := s.root.newBlock("maprange.epost")
		endBB := s.root.newBlock("maprange.end")

		s.root.loopStack = append(s.root.loopStack, loopContext{breakBlock: endBB, continueBlock: ePostBB})
		defer func() {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		}()

		s.root.terminate(&hir.InstrJump{Target: bCondBB.Label})

		s.root.setBlock(bCondBB)
		curBIdx := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrLoad{Dst: curBIdx, Ptr: bIdxAlloca})
		cmpB := s.root.nextReg(sema.TypeBool)
		s.root.emit(&hir.InstrBinary{Dst: cmpB, Op: hir.OpLt, L: curBIdx, R: numBuckets})
		s.root.terminate(&hir.InstrBranch{Cond: cmpB, ThenTarget: bBodyBB.Label, ElseTarget: endBB.Label})

		s.root.setBlock(bBodyBB)
		pHead := s.root.nextReg(&sema.PointerType{Base: entryPtrType})
		s.root.emit(&hir.InstrGetElemPtr{Dst: pHead, BasePtr: buckets, Index: curBIdx})
		head := s.root.nextReg(entryPtrType)
		s.root.emit(&hir.InstrLoad{Dst: head, Ptr: pHead})
		s.root.emit(&hir.InstrStore{Val: head, Ptr: entryAlloca})
		s.root.terminate(&hir.InstrJump{Target: eCondBB.Label})

		s.root.setBlock(eCondBB)
		curE := s.root.nextReg(entryPtrType)
		s.root.emit(&hir.InstrLoad{Dst: curE, Ptr: entryAlloca})
		hasE := s.root.nextReg(sema.TypeBool)
		s.root.emit(&hir.InstrBinary{Dst: hasE, Op: hir.OpNeq, L: curE, R: &hir.ConstNil{Typ: entryPtrType}})
		s.root.terminate(&hir.InstrBranch{Cond: hasE, ThenTarget: eBodyBB.Label, ElseTarget: bPostBB.Label})

		s.root.setBlock(eBodyBB)
		if kPtr != nil {
			pKey := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
			s.root.emit(&hir.InstrGetFieldPtr{Dst: pKey, BasePtr: curE, FieldIndex: 1, FieldName: "key"})
			rawKey := s.root.nextReg(sema.TypeInt)
			s.root.emit(&hir.InstrLoad{Dst: rawKey, Ptr: pKey})
			realKey := s.root.coerceFromI64(rawKey, mp.Key)
			s.root.emit(&hir.InstrStore{Val: realKey, Ptr: kPtr})
		}
		if vPtr != nil {
			pVal := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
			s.root.emit(&hir.InstrGetFieldPtr{Dst: pVal, BasePtr: curE, FieldIndex: 2, FieldName: "val"})
			rawVal := s.root.nextReg(sema.TypeInt)
			s.root.emit(&hir.InstrLoad{Dst: rawVal, Ptr: pVal})
			realKeyVal := s.root.coerceFromI64(rawVal, mp.Value)
			s.root.emit(&hir.InstrStore{Val: realKeyVal, Ptr: vPtr})
		}

		s.LowerStmt(fr.Body)
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: ePostBB.Label})
		}

		s.root.setBlock(ePostBB)
		curEPost := s.root.nextReg(entryPtrType)
		s.root.emit(&hir.InstrLoad{Dst: curEPost, Ptr: entryAlloca})
		pNextE := s.root.nextReg(&sema.PointerType{Base: entryPtrType})
		s.root.emit(&hir.InstrGetFieldPtr{Dst: pNextE, BasePtr: curEPost, FieldIndex: 3, FieldName: "next"})
		nextE := s.root.nextReg(entryPtrType)
		s.root.emit(&hir.InstrLoad{Dst: nextE, Ptr: pNextE})
		s.root.emit(&hir.InstrStore{Val: nextE, Ptr: entryAlloca})
		s.root.terminate(&hir.InstrJump{Target: eCondBB.Label})

		s.root.setBlock(bPostBB)
		nextB := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrBinary{Dst: nextB, Op: hir.OpAdd, L: curBIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
		s.root.emit(&hir.InstrStore{Val: nextB, Ptr: bIdxAlloca})
		s.root.terminate(&hir.InstrJump{Target: bCondBB.Label})

		s.root.setBlock(endBB)
		if fr.Key != nil {
			if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
				if hasOldKey {
					s.root.symbols[astIDValue(kId)] = oldKeySym
					s.root.symbolTypes[astIDValue(kId)] = oldKeyTyp
				} else {
					delete(s.root.symbols, astIDValue(kId))
					delete(s.root.symbolTypes, astIDValue(kId))
				}
			}
		}
		if fr.Value != nil {
			if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
				if hasOldVal {
					s.root.symbols[astIDValue(vId)] = oldValSym
					s.root.symbolTypes[astIDValue(vId)] = oldValTyp
				} else {
					delete(s.root.symbols, astIDValue(vId))
					delete(s.root.symbolTypes, astIDValue(vId))
				}
			}
		}
		return
	}

	// 4. スライス、配列、文字列の走査
	var elemType sema.Type = sema.TypeByte
	var lenVal hir.Value = nil
	var dataPtr hir.Value = nil

	if sl, isSlice := xType.(*sema.SliceType); isSlice {
		elemType = sl.Elem
		rawBytePtr := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: xVal, Index: 0})
		typedPtr := s.root.nextReg(&sema.PointerType{Base: elemType})
		s.root.emit(&hir.InstrCast{Dst: typedPtr, Val: rawBytePtr, ToType: &sema.PointerType{Base: elemType}})
		lenReg := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrExtractValue{Dst: lenReg, Agg: xVal, Index: 1})
		dataPtr = typedPtr
		lenVal = lenReg
	} else if ar, isArr := xType.(*sema.ArrayType); isArr {
		elemType = ar.Elem
		lenVal = &hir.ConstInt{Val: int64(ar.Len), Typ: sema.TypeInt}
		dataPtr = s.root.Expr.LowerLValue(fr.X)
	} else if xType == sema.TypeString || semaTypeName(xType) == "string" {
		dataPtr, lenVal = s.root.stringParts(xVal)
		elemType = sema.TypeByte
	} else {
		lenReg := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrCallStatic{Dst: lenReg, CalleeName: s.root.BuiltinName("strlen"), Args: []hir.Value{xVal}})
		dataPtr = xVal
		lenVal = lenReg
		elemType = sema.TypeByte
	}

	idxAlloca := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "range.idx")
	s.root.emit(&hir.InstrAlloca{Dst: idxAlloca, AllocType: sema.TypeInt})
	s.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: idxAlloca})

	var oldKeySym, oldValSym hir.Value
	var oldKeyTyp, oldValTyp sema.Type
	var hasOldKey, hasOldVal bool

	var kPtr, vPtr hir.Value
	if fr.Key != nil {
		if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
			oldKeySym, hasOldKey = s.root.symbols[astIDValue(kId)]
			oldKeyTyp = s.root.symbolTypes[astIDValue(kId)]

			kReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, astIDValue(kId))
			s.root.emit(&hir.InstrAlloca{Dst: kReg, AllocType: sema.TypeInt})
			kPtr = kReg
			s.root.symbols[astIDValue(kId)] = kPtr
			s.root.symbolTypes[astIDValue(kId)] = sema.TypeInt
		}
	}
	if fr.Value != nil {
		if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
			oldValSym, hasOldVal = s.root.symbols[astIDValue(vId)]
			oldValTyp = s.root.symbolTypes[astIDValue(vId)]

			vReg := s.root.nextReg(&sema.PointerType{Base: elemType}, astIDValue(vId))
			s.root.emit(&hir.InstrAlloca{Dst: vReg, AllocType: elemType})
			vPtr = vReg
			s.root.symbols[astIDValue(vId)] = vPtr
			s.root.symbolTypes[astIDValue(vId)] = elemType
		}
	}

	condBB := s.root.newBlock("forrange.cond")
	bodyBB := s.root.newBlock("forrange.body")
	postBB := s.root.newBlock("forrange.post")
	endBB := s.root.newBlock("forrange.end")

	s.root.loopStack = append(s.root.loopStack, loopContext{breakBlock: endBB, continueBlock: postBB})
	defer func() {
		s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
	}()

	s.root.terminate(&hir.InstrJump{Target: condBB.Label})

	s.root.setBlock(condBB)
	curIdx := s.root.nextReg(sema.TypeInt)
	s.root.emit(&hir.InstrLoad{Dst: curIdx, Ptr: idxAlloca})
	cmpReg := s.root.nextReg(sema.TypeBool)
	s.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpLt, L: curIdx, R: lenVal})
	s.root.terminate(&hir.InstrBranch{Cond: cmpReg, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})

	s.root.setBlock(bodyBB)
	if kPtr != nil {
		s.root.emit(&hir.InstrStore{Val: curIdx, Ptr: kPtr})
	}
	if vPtr != nil {
		elemPtrReg := s.root.nextReg(&sema.PointerType{Base: elemType})
		s.root.emit(&hir.InstrGetElemPtr{Dst: elemPtrReg, BasePtr: dataPtr, Index: curIdx})
		elemValReg := s.root.nextReg(elemType)
		s.root.emit(&hir.InstrLoad{Dst: elemValReg, Ptr: elemPtrReg})
		s.root.emit(&hir.InstrStore{Val: elemValReg, Ptr: vPtr})
	}

	s.LowerStmt(fr.Body)
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: postBB.Label})
	}

	s.root.setBlock(postBB)
	incIdx := s.root.nextReg(sema.TypeInt)
	s.root.emit(&hir.InstrBinary{Dst: incIdx, Op: hir.OpAdd, L: curIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
	s.root.emit(&hir.InstrStore{Val: incIdx, Ptr: idxAlloca})
	s.root.terminate(&hir.InstrJump{Target: condBB.Label})

	s.root.setBlock(endBB)
	if fr.Key != nil {
		if kId, ok := fr.Key.(*ast.Identifier); ok && astIDValue(kId) != "_" {
			if hasOldKey {
				s.root.symbols[astIDValue(kId)] = oldKeySym
				s.root.symbolTypes[astIDValue(kId)] = oldKeyTyp
			} else {
				delete(s.root.symbols, astIDValue(kId))
				delete(s.root.symbolTypes, astIDValue(kId))
			}
		}
	}
	if fr.Value != nil {
		if vId, ok := fr.Value.(*ast.Identifier); ok && astIDValue(vId) != "_" {
			if hasOldVal {
				s.root.symbols[astIDValue(vId)] = oldValSym
				s.root.symbolTypes[astIDValue(vId)] = oldValTyp
			} else {
				delete(s.root.symbols, astIDValue(vId))
				delete(s.root.symbolTypes, astIDValue(vId))
			}
		}
	}
}

func (s *StmtLowerer) LowerSwitchStmt(ss *ast.SwitchStmt) {
	if ss.Init != nil {
		s.LowerStmt(ss.Init)
	}

	switchVal := s.root.Expr.LowerExpr(ss.Value)
	endBB := s.root.newBlock("switch.end")

	s.root.loopStack = append(s.root.loopStack, loopContext{breakBlock: endBB, continueBlock: endBB})
	defer func() {
		s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
	}()

	var defaultCase *ast.CaseClause = nil

	for _, cc := range ss.Cases {
		if len(cc.Values) == 0 {
			defaultCase = cc
			continue
		}

		caseBodyBB := s.root.newBlock("switch.case.body")
		nextCaseBB := s.root.newBlock("switch.case.next")

		var matchedCond hir.Value = nil
		for _, valExpr := range cc.Values {
			vVal := s.root.Expr.LowerExpr(valExpr)
			var cmpReg *hir.Reg

			if vVal.Type() == sema.TypeString || vVal.Type() == sema.TypeCString || switchVal.Type() == sema.TypeString || switchVal.Type() == sema.TypeCString {
				left := switchVal
				right := vVal
				if switchVal.Type() == sema.TypeString {
					left, _ = s.root.stringParts(switchVal)
				}
				if vVal.Type() == sema.TypeString {
					right, _ = s.root.stringParts(vVal)
				}
				cmpReg = s.root.nextReg(sema.TypeBool)
				s.root.emit(&hir.InstrCallStatic{Dst: cmpReg, CalleeName: s.root.BuiltinName("hike_streq"), Args: []hir.Value{left, right}})
			} else {
				cmpReg = s.root.nextReg(sema.TypeBool)
				s.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpEq, L: switchVal, R: vVal})
			}

			if matchedCond == nil {
				matchedCond = cmpReg
			} else {
				orReg := s.root.nextReg(sema.TypeBool)
				s.root.emit(&hir.InstrBinary{Dst: orReg, Op: hir.OpOr, L: matchedCond, R: cmpReg})
				matchedCond = orReg
			}
		}

		s.root.terminate(&hir.InstrBranch{Cond: matchedCond, ThenTarget: caseBodyBB.Label, ElseTarget: nextCaseBB.Label})

		s.root.setBlock(caseBodyBB)
		for _, stmt := range cc.Body {
			s.LowerStmt(stmt)
		}
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}

		s.root.setBlock(nextCaseBB)
	}

	if defaultCase != nil {
		for _, stmt := range defaultCase.Body {
			s.LowerStmt(stmt)
		}
	}
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: endBB.Label})
	}

	s.root.setBlock(endBB)
}

func (s *StmtLowerer) LowerTypeSwitchStmt(tss *ast.TypeSwitchStmt) {
	if tss.Init != nil {
		s.LowerStmt(tss.Init)
	}

	exprVal := s.root.Expr.LowerExpr(tss.Expr)
	exprType := exprVal.Type()
	endBB := s.root.newBlock("typeswitch.end")

	s.root.loopStack = append(s.root.loopStack, loopContext{breakBlock: endBB, continueBlock: endBB})
	defer func() {
		s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
	}()

	dataPtrReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	actualTypeIDReg := s.root.nextReg(sema.TypeInt)

	if it, ok := exprType.(*sema.InterfaceType); ok && !it.IsAny() {
		itabRawReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e := s.root
		e.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 0})
		e.emit(&hir.InstrExtractValue{Dst: itabRawReg, Agg: exprVal, Index: 1})
		typeIDPtr := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
		e.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabRawReg, ToType: &sema.PointerType{Base: sema.TypeInt}})
		e.emit(&hir.InstrLoad{Dst: actualTypeIDReg, Ptr: typeIDPtr})
	} else {
		s.root.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 0})
		s.root.emit(&hir.InstrExtractValue{Dst: actualTypeIDReg, Agg: exprVal, Index: 1})
	}

	var oldSym hir.Value
	var oldTyp sema.Type
	var hasOld bool
	if tss.Variable != nil {
		oldSym, hasOld = s.root.symbols[tss.Variable.Value]
		oldTyp = s.root.symbolTypes[tss.Variable.Value]
	}

	var defaultCase *ast.TypeCaseClause = nil

	for _, c := range tss.Cases {
		if len(c.Types) == 0 && !c.IsNil {
			defaultCase = c
			continue
		}

		caseBodyBB := s.root.newBlock("typeswitch.case.body")
		nextCaseBB := s.root.newBlock("typeswitch.case.next")

		var matchedCond hir.Value = nil
		if c.IsNil {
			cmpReg := s.root.nextReg(sema.TypeBool)
			s.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpEq, L: actualTypeIDReg, R: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}})
			matchedCond = cmpReg
		}
		for _, tExpr := range c.Types {
			targetType := s.root.semaCtx.ResolveType(tExpr)
			targetTypeID := s.root.semaCtx.GetTypeID(targetType)

			cmpReg := s.root.nextReg(sema.TypeBool)
			s.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpEq, L: actualTypeIDReg, R: &hir.ConstInt{Val: targetTypeID, Typ: sema.TypeInt}})

			if matchedCond == nil {
				matchedCond = cmpReg
			} else {
				orReg := s.root.nextReg(sema.TypeBool)
				s.root.emit(&hir.InstrBinary{Dst: orReg, Op: hir.OpOr, L: matchedCond, R: cmpReg})
				matchedCond = orReg
			}
		}

		s.root.terminate(&hir.InstrBranch{Cond: matchedCond, ThenTarget: caseBodyBB.Label, ElseTarget: nextCaseBB.Label})

		s.root.setBlock(caseBodyBB)
		if tss.Variable != nil {
			var valToStore hir.Value
			if len(c.Types) == 1 && !c.IsNil {
				targetType := s.root.semaCtx.ResolveType(c.Types[0])
				if strings.HasSuffix(sema.LLVMTypeOf(targetType), "*") {
					castVal := s.root.nextReg(targetType)
					s.root.emit(&hir.InstrCast{Dst: castVal, Val: dataPtrReg, ToType: targetType})
					valToStore = castVal
				} else {
					typedPtr := s.root.nextReg(&sema.PointerType{Base: targetType})
					s.root.emit(&hir.InstrCast{Dst: typedPtr, Val: dataPtrReg, ToType: &sema.PointerType{Base: targetType}})
					unpackedVal := s.root.nextReg(targetType)
					s.root.emit(&hir.InstrLoad{Dst: unpackedVal, Ptr: typedPtr})
					valToStore = unpackedVal
				}
			} else {
				valToStore = exprVal
			}
			vAlloca := s.root.nextReg(&sema.PointerType{Base: valToStore.Type()}, tss.Variable.Value)
			s.root.emit(&hir.InstrAlloca{Dst: vAlloca, AllocType: valToStore.Type()})
			s.root.emit(&hir.InstrStore{Val: valToStore, Ptr: vAlloca})
			s.root.symbols[tss.Variable.Value] = vAlloca
			s.root.symbolTypes[tss.Variable.Value] = valToStore.Type()
		}

		for _, innerStmt := range c.Body {
			s.LowerStmt(innerStmt)
		}
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}

		s.root.setBlock(nextCaseBB)
	}

	if defaultCase != nil {
		if tss.Variable != nil {
			vAlloca := s.root.nextReg(&sema.PointerType{Base: exprType}, tss.Variable.Value)
			s.root.emit(&hir.InstrAlloca{Dst: vAlloca, AllocType: exprType})
			s.root.emit(&hir.InstrStore{Val: exprVal, Ptr: vAlloca})
			s.root.symbols[tss.Variable.Value] = vAlloca
			s.root.symbolTypes[tss.Variable.Value] = exprType
		}
		for _, innerStmt := range defaultCase.Body {
			s.LowerStmt(innerStmt)
		}
	}
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: endBB.Label})
	}

	if tss.Variable != nil {
		if hasOld {
			s.root.symbols[tss.Variable.Value] = oldSym
			s.root.symbolTypes[tss.Variable.Value] = oldTyp
		} else {
			delete(s.root.symbols, tss.Variable.Value)
			delete(s.root.symbolTypes, tss.Variable.Value)
		}
	}

	s.root.setBlock(endBB)
}

// -------------------------------------------------------------
// リターン文 (ReturnStmt)
// -------------------------------------------------------------

func (s *StmtLowerer) LowerReturnStmt(rs *ast.ReturnStmt) {
	if s.root.Call.currentVarArgs != nil {
		return
	}

	if s.lowerTupleReturn(rs) {
		return
	}

	vals := make([]hir.Value, len(rs.Values))
	for i, v := range rs.Values {
		val := s.root.Expr.LowerExpr(v)
		if s.root.curFunc != nil && i < len(s.root.curFunc.ReturnTypes) {
			val = s.root.emitValueCoerce(val, s.root.curFunc.ReturnTypes[i])
		}
		vals[i] = val
	}

	for i := len(s.root.deferStack) - 1; i >= 0; i-- {
		s.root.Call.LowerCall(s.root.deferStack[i])
	}

	s.root.terminate(&hir.InstrReturn{Vals: vals})
}

func (s *StmtLowerer) lowerTupleReturn(rs *ast.ReturnStmt) bool {
	if len(rs.Values) != 1 || s.root.curFunc == nil || len(s.root.curFunc.ReturnTypes) <= 1 {
		return false
	}
	rhsVal := s.root.Expr.LowerExpr(rs.Values[0])
	tup, ok := rhsVal.Type().(*sema.TupleType)
	if !ok {
		return false
	}

	vals := make([]hir.Value, len(s.root.curFunc.ReturnTypes))
	for i := range s.root.curFunc.ReturnTypes {
		if i >= len(tup.Types) {
			vals[i] = s.root.defaultConstValue(s.root.curFunc.ReturnTypes[i])
			continue
		}
		elemReg := s.root.nextReg(tup.Types[i])
		s.root.emit(&hir.InstrExtractValue{Dst: elemReg, Agg: rhsVal, Index: i})
		vals[i] = s.root.emitValueCoerce(elemReg, s.root.curFunc.ReturnTypes[i])
	}
	for i := len(s.root.deferStack) - 1; i >= 0; i-- {
		s.root.Call.LowerCall(s.root.deferStack[i])
	}
	s.root.terminate(&hir.InstrReturn{Vals: vals})
	return true
}
