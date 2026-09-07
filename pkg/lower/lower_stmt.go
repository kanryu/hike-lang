package lower

import (
	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

// StmtLowerer は文（Statement）の走査、制御フロー（CFG）のブロック分岐、代入・変数初期化を担当する
type StmtLowerer struct {
	root *Lowerer
}

func NewStmtLowerer(root *Lowerer) *StmtLowerer {
	return &StmtLowerer{root: root}
}

// -----------------------------------------------------------------------------
// 文 (Statement) のディスパッチ
// -----------------------------------------------------------------------------

func (s *StmtLowerer) LowerStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}

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

// -----------------------------------------------------------------------------
// 変数宣言 (VarDecl)
// -----------------------------------------------------------------------------

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
		sizeVal := &hir.ConstInt{Val: int64(targetType.Size()), Typ: sema.TypeInt}
		s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: targetType})
	} else {
		s.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: targetType})
	}

	s.root.symbols[vd.Name.Value] = ptrReg
	s.root.symbolTypes[vd.Name.Value] = targetType
	s.root.emit(&hir.InstrStore{Val: val, Ptr: ptrReg})
}

// -----------------------------------------------------------------------------
// 代入文 (AssignStmt)
// -----------------------------------------------------------------------------

func (s *StmtLowerer) LowerAssignStmt(as *ast.AssignStmt) {
	isDefine := (as.Token.Type == token.DEFINE) || (as.Token.Literal == ":=") ||
		(as.Token.Type == token.VAR) || (as.Token.Literal == "var") || (as.Type != nil)

	// 1. 多値代入 (Tuple unpacking)
	if len(as.Left) > 1 && len(as.Right) == 1 {
		tupleVal := s.root.Expr.LowerExpr(as.Right[0])
		if tt, isTuple := tupleVal.Type().(*sema.TupleType); isTuple {
			for i, left := range as.Left {
				if i >= len(tt.Types) {
					break
				}
				elemType := tt.Types[i]
				elemReg := s.root.nextReg(elemType)
				s.root.emit(&hir.InstrExtractValue{Dst: elemReg, Agg: tupleVal, Index: i})

				if id, ok := left.(*ast.Identifier); ok {
					if id.Value == "_" {
						continue
					}
					if isDefine {
						ptrReg := s.root.nextReg(&sema.PointerType{Base: elemType}, id.Value)
						if s.root.escapedVars[id.Value] {
							sizeVal := &hir.ConstInt{Val: int64(elemType.Size()), Typ: sema.TypeInt}
							s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: elemType})
						} else {
							s.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: elemType})
						}
						s.root.symbols[id.Value] = ptrReg
						s.root.symbolTypes[id.Value] = elemType
						s.root.emit(&hir.InstrStore{Val: elemReg, Ptr: ptrReg})
					} else {
						ptr := s.root.Expr.LowerLValue(id)
						s.root.emit(&hir.InstrStore{Val: elemReg, Ptr: ptr})
					}
				} else {
					ptr := s.root.Expr.LowerLValue(left)
					targetType := ptr.Type().(*sema.PointerType).Base
					val := s.root.emitValueCoerce(elemReg, targetType)
					s.root.emit(&hir.InstrStore{Val: val, Ptr: ptr})
				}
			}
			return
		}
	}

	// 2. 通常代入 / 短縮定義
	rhsVals := make([]hir.Value, len(as.Right))
	for i, r := range as.Right {
		rhsVals[i] = s.root.Expr.LowerExpr(r)
	}

	for i, left := range as.Left {
		if id, ok := left.(*ast.Identifier); ok && id.Value == "_" {
			continue
		}

		var rhs ast.Expression = nil
		var val hir.Value = nil
		if i < len(as.Right) {
			rhs = as.Right[i]
		}
		if i < len(rhsVals) {
			val = rhsVals[i]
		}

		if id, ok := left.(*ast.Identifier); ok && isDefine {
			var actualType sema.Type = nil
			if as.Type != nil {
				actualType = s.root.semaCtx.ResolveType(as.Type)
			}

			isUninitVar := false
			if il, okIl := rhs.(*ast.IntegerLiteral); okIl && (as.Token.Type == token.VAR || as.Token.Literal == "var") && il.Token.Type == token.VAR {
				isUninitVar = true
			}

			if isUninitVar && actualType != nil {
				val = s.root.defaultConstValue(actualType)
			} else if val != nil {
				if actualType == nil {
					actualType = val.Type()
				} else {
					val = s.root.emitValueCoerce(val, actualType)
				}
			} else if actualType != nil {
				val = s.root.defaultConstValue(actualType)
			} else {
				actualType = sema.TypeInt
				val = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
			}

			ptrReg := s.root.nextReg(&sema.PointerType{Base: actualType}, id.Value)
			if s.root.escapedVars[id.Value] {
				sizeVal := &hir.ConstInt{Val: int64(actualType.Size()), Typ: sema.TypeInt}
				s.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: actualType})
			} else {
				s.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: actualType})
			}
			s.root.symbols[id.Value] = ptrReg
			s.root.symbolTypes[id.Value] = actualType
			s.root.emit(&hir.InstrStore{Val: val, Ptr: ptrReg})
			continue
		}

		ptr := s.root.Expr.LowerLValue(left)

		switch as.Token.Literal {
		case "++":
			targetType := ptr.Type().(*sema.PointerType).Base
			curValReg := s.root.nextReg(targetType)
			s.root.emit(&hir.InstrLoad{Dst: curValReg, Ptr: ptr})
			resReg := s.root.nextReg(targetType)
			s.root.emit(&hir.InstrBinary{Dst: resReg, Op: hir.OpAdd, L: curValReg, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
			val = resReg
		case "--":
			targetType := ptr.Type().(*sema.PointerType).Base
			curValReg := s.root.nextReg(targetType)
			s.root.emit(&hir.InstrLoad{Dst: curValReg, Ptr: ptr})
			resReg := s.root.nextReg(targetType)
			s.root.emit(&hir.InstrBinary{Dst: resReg, Op: hir.OpSub, L: curValReg, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
			val = resReg
		case "+=", "-=", "*=", "/=", "%=":
			curValReg := s.root.nextReg(val.Type())
			s.root.emit(&hir.InstrLoad{Dst: curValReg, Ptr: ptr})
			op := hir.OpAdd
			switch as.Token.Literal {
			case "-=":
				op = hir.OpSub
			case "*=":
				op = hir.OpMul
			case "/=":
				op = hir.OpDiv
			case "%=":
				op = hir.OpRem
			}
			resReg := s.root.nextReg(val.Type())
			s.root.emit(&hir.InstrBinary{Dst: resReg, Op: op, L: curValReg, R: val})
			val = resReg
		}

		targetType := ptr.Type().(*sema.PointerType).Base
		val = s.root.emitValueCoerce(val, targetType)
		s.root.emit(&hir.InstrStore{Val: val, Ptr: ptr})
	}
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
	xVal := s.root.Expr.LowerExpr(fr.X)
	xType := xVal.Type()

	// 1. MapBehavior (イテレータインターフェース)
	if _, _, isBeh := s.root.semaCtx.CheckMapBehavior(xType); isBeh {
		objPtr := s.root.Expr.LowerStructPtr(fr.X)
		initFnName, initFn, finalRecv, hasInit := s.root.Call.ResolveMethod(xType, "InitIterator", objPtr)
		nextFnName, nextFn, _, hasNext := s.root.Call.ResolveMethod(xType, "Next", objPtr)

		if hasInit && hasNext && initFn != nil && nextFn != nil {
			sizeReg := s.root.nextReg(sema.TypeInt)
			s.root.emit(&hir.InstrCallStatic{Dst: sizeReg, CalleeName: initFnName, Args: []hir.Value{finalRecv, &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}}})

			bufReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			s.root.emit(&hir.InstrAllocaDynamic{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte})
			s.root.emit(&hir.InstrCallStatic{CalleeName: initFnName, Args: []hir.Value{finalRecv, bufReg}})

			condBB := s.root.newBlock("mapbeh.cond")
			bodyBB := s.root.newBlock("mapbeh.body")
			endBB := s.root.newBlock("mapbeh.end")

			s.root.loopStack = append(s.root.loopStack, loopContext{breakBlock: endBB, continueBlock: condBB})
			defer func() {
				s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
			}()

			s.root.terminate(&hir.InstrJump{Target: condBB.Label})

			s.root.setBlock(condBB)
			retTupleType := nextFn.ReturnTypes[0]
			nextRes := s.root.nextReg(retTupleType)
			s.root.emit(&hir.InstrCallStatic{Dst: nextRes, CalleeName: nextFnName, Args: []hir.Value{finalRecv, bufReg}})
			okReg := s.root.nextReg(sema.TypeBool)
			s.root.emit(&hir.InstrExtractValue{Dst: okReg, Agg: nextRes, Index: 2})
			s.root.terminate(&hir.InstrBranch{Cond: okReg, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})

			s.root.setBlock(bodyBB)
			if fr.Key != nil {
				if kId, ok := fr.Key.(*ast.Identifier); ok && kId.Value != "_" {
					kPtrReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
					s.root.emit(&hir.InstrExtractValue{Dst: kPtrReg, Agg: nextRes, Index: 0})
					kValReg := s.root.nextReg(sema.TypeInt)
					s.root.emit(&hir.InstrLoad{Dst: kValReg, Ptr: kPtrReg})
					kPtr := s.root.symbols[kId.Value]
					if kPtr == nil {
						kPtr = s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, kId.Value)
						s.root.emit(&hir.InstrAlloca{Dst: kPtr.(*hir.Reg), AllocType: sema.TypeInt})
						s.root.symbols[kId.Value] = kPtr
						s.root.symbolTypes[kId.Value] = sema.TypeInt
					}
					s.root.emit(&hir.InstrStore{Val: kValReg, Ptr: kPtr})
				}
			}
			if fr.Value != nil {
				if vId, ok := fr.Value.(*ast.Identifier); ok && vId.Value != "_" {
					vPtrReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
					s.root.emit(&hir.InstrExtractValue{Dst: vPtrReg, Agg: nextRes, Index: 1})
					vValReg := s.root.nextReg(sema.TypeInt)
					s.root.emit(&hir.InstrLoad{Dst: vValReg, Ptr: vPtrReg})
					vPtr := s.root.symbols[vId.Value]
					if vPtr == nil {
						vPtr = s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, vId.Value)
						s.root.emit(&hir.InstrAlloca{Dst: vPtr.(*hir.Reg), AllocType: sema.TypeInt})
						s.root.symbols[vId.Value] = vPtr
						s.root.symbolTypes[vId.Value] = sema.TypeInt
					}
					s.root.emit(&hir.InstrStore{Val: vValReg, Ptr: vPtr})
				}
			}

			s.LowerStmt(fr.Body)
			if s.root.curBlock.Terminator == nil {
				s.root.terminate(&hir.InstrJump{Target: condBB.Label})
			}

			s.root.setBlock(endBB)
			return
		}
	}

	// 2. 言語標準マップ (MapType)
	if mp, isMap := xType.(*sema.MapType); isMap {
		bIdxAlloca := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "maprange.bidx")
		s.root.emit(&hir.InstrAlloca{Dst: bIdxAlloca, AllocType: sema.TypeInt})
		s.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: bIdxAlloca})

		entryAlloca := s.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}, "maprange.cur")
		s.root.emit(&hir.InstrAlloca{Dst: entryAlloca, AllocType: &sema.PointerType{Base: sema.TypeByte}})

		pBuckets := s.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		s.root.emit(&hir.InstrGetFieldPtr{Dst: pBuckets, BasePtr: xVal, FieldIndex: 0, FieldName: "buckets"})
		buckets := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrLoad{Dst: buckets, Ptr: pBuckets})

		pNumBuckets := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
		s.root.emit(&hir.InstrGetFieldPtr{Dst: pNumBuckets, BasePtr: xVal, FieldIndex: 1, FieldName: "numBuckets"})
		numBuckets := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrLoad{Dst: numBuckets, Ptr: pNumBuckets})

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
		pHead := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrGetElemPtr{Dst: pHead, BasePtr: buckets, Index: curBIdx})
		head := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrLoad{Dst: head, Ptr: pHead})
		s.root.emit(&hir.InstrStore{Val: head, Ptr: entryAlloca})
		s.root.terminate(&hir.InstrJump{Target: eCondBB.Label})

		s.root.setBlock(eCondBB)
		curE := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrLoad{Dst: curE, Ptr: entryAlloca})
		hasE := s.root.nextReg(sema.TypeBool)
		s.root.emit(&hir.InstrBinary{Dst: hasE, Op: hir.OpNeq, L: curE, R: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}})
		s.root.terminate(&hir.InstrBranch{Cond: hasE, ThenTarget: eBodyBB.Label, ElseTarget: bPostBB.Label})

		s.root.setBlock(eBodyBB)
		if fr.Key != nil {
			if kId, ok := fr.Key.(*ast.Identifier); ok && kId.Value != "_" {
				pKey := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
				s.root.emit(&hir.InstrGetFieldPtr{Dst: pKey, BasePtr: curE, FieldIndex: 1, FieldName: "key"})
				rawKey := s.root.nextReg(sema.TypeInt)
				s.root.emit(&hir.InstrLoad{Dst: rawKey, Ptr: pKey})
				realKey := s.root.coerceFromI64(rawKey, mp.Key)

				kPtr := s.root.symbols[kId.Value]
				if kPtr == nil {
					kPtr = s.root.nextReg(&sema.PointerType{Base: mp.Key}, kId.Value)
					s.root.emit(&hir.InstrAlloca{Dst: kPtr.(*hir.Reg), AllocType: mp.Key})
					s.root.symbols[kId.Value] = kPtr
					s.root.symbolTypes[kId.Value] = mp.Key
				}
				s.root.emit(&hir.InstrStore{Val: realKey, Ptr: kPtr})
			}
		}
		if fr.Value != nil {
			if vId, ok := fr.Value.(*ast.Identifier); ok && vId.Value != "_" {
				pVal := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
				s.root.emit(&hir.InstrGetFieldPtr{Dst: pVal, BasePtr: curE, FieldIndex: 2, FieldName: "val"})
				rawVal := s.root.nextReg(sema.TypeInt)
				s.root.emit(&hir.InstrLoad{Dst: rawVal, Ptr: pVal})
				realVal := s.root.coerceFromI64(rawVal, mp.Value)

				vPtr := s.root.symbols[vId.Value]
				if vPtr == nil {
					vPtr = s.root.nextReg(&sema.PointerType{Base: mp.Value}, vId.Value)
					s.root.emit(&hir.InstrAlloca{Dst: vPtr.(*hir.Reg), AllocType: mp.Value})
					s.root.symbols[vId.Value] = vPtr
					s.root.symbolTypes[vId.Value] = mp.Value
				}
				s.root.emit(&hir.InstrStore{Val: realVal, Ptr: vPtr})
			}
		}

		s.LowerStmt(fr.Body)
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: ePostBB.Label})
		}

		s.root.setBlock(ePostBB)
		curEPost := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrLoad{Dst: curEPost, Ptr: entryAlloca})
		pNextE := s.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		s.root.emit(&hir.InstrGetFieldPtr{Dst: pNextE, BasePtr: curEPost, FieldIndex: 3, FieldName: "next"})
		nextE := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrLoad{Dst: nextE, Ptr: pNextE})
		s.root.emit(&hir.InstrStore{Val: nextE, Ptr: entryAlloca})
		s.root.terminate(&hir.InstrJump{Target: eCondBB.Label})

		s.root.setBlock(bPostBB)
		nextB := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrBinary{Dst: nextB, Op: hir.OpAdd, L: curBIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
		s.root.emit(&hir.InstrStore{Val: nextB, Ptr: bIdxAlloca})
		s.root.terminate(&hir.InstrJump{Target: bCondBB.Label})

		s.root.setBlock(endBB)
		return
	}

	// 3. スライス / 配列 / 文字列
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
	} else {
		lenReg := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrCallStatic{Dst: lenReg, CalleeName: "strlen", Args: []hir.Value{xVal}})
		dataPtr = xVal
		lenVal = lenReg
		elemType = sema.TypeByte
	}

	idxAlloca := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "range.idx")
	s.root.emit(&hir.InstrAlloca{Dst: idxAlloca, AllocType: sema.TypeInt})
	s.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: idxAlloca})

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
	if fr.Key != nil {
		if kId, ok := fr.Key.(*ast.Identifier); ok && kId.Value != "_" {
			kPtr := s.root.symbols[kId.Value]
			if kPtr == nil {
				kPtr = s.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, kId.Value)
				s.root.emit(&hir.InstrAlloca{Dst: kPtr.(*hir.Reg), AllocType: sema.TypeInt})
				s.root.symbols[kId.Value] = kPtr
				s.root.symbolTypes[kId.Value] = sema.TypeInt
			}
			s.root.emit(&hir.InstrStore{Val: curIdx, Ptr: kPtr})
		}
	}
	if fr.Value != nil {
		if vId, ok := fr.Value.(*ast.Identifier); ok && vId.Value != "_" {
			elemPtrReg := s.root.nextReg(&sema.PointerType{Base: elemType})
			s.root.emit(&hir.InstrGetElemPtr{Dst: elemPtrReg, BasePtr: dataPtr, Index: curIdx})
			elemValReg := s.root.nextReg(elemType)
			s.root.emit(&hir.InstrLoad{Dst: elemValReg, Ptr: elemPtrReg})

			vPtr := s.root.symbols[vId.Value]
			if vPtr == nil {
				vPtr = s.root.nextReg(&sema.PointerType{Base: elemType}, vId.Value)
				s.root.emit(&hir.InstrAlloca{Dst: vPtr.(*hir.Reg), AllocType: elemType})
				s.root.symbols[vId.Value] = vPtr
				s.root.symbolTypes[vId.Value] = elemType
			}
			s.root.emit(&hir.InstrStore{Val: elemValReg, Ptr: vPtr})
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
			if vVal.Type() == sema.TypeString {
				cmpReg = s.root.nextReg(sema.TypeBool)
				s.root.emit(&hir.InstrCallStatic{Dst: cmpReg, CalleeName: "hike_streq", Args: []hir.Value{switchVal, vVal}})
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
		s.root.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 0})
		s.root.emit(&hir.InstrExtractValue{Dst: itabRawReg, Agg: exprVal, Index: 1})
		typeIDPtr := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt})
		s.root.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabRawReg, ToType: &sema.PointerType{Base: sema.TypeInt}})
		s.root.emit(&hir.InstrLoad{Dst: actualTypeIDReg, Ptr: typeIDPtr})
	} else {
		s.root.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 0})
		s.root.emit(&hir.InstrExtractValue{Dst: actualTypeIDReg, Agg: exprVal, Index: 1})
	}

	var defaultCase *ast.TypeCaseClause = nil

	for _, c := range tss.Cases {
		if len(c.Types) == 0 {
			defaultCase = c
			continue
		}

		caseBodyBB := s.root.newBlock("typeswitch.case.body")
		nextCaseBB := s.root.newBlock("typeswitch.case.next")

		var matchedCond hir.Value = nil
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
			if len(c.Types) == 1 {
				targetType := s.root.semaCtx.ResolveType(c.Types[0])
				castVal := s.root.nextReg(targetType)
				s.root.emit(&hir.InstrCast{Dst: castVal, Val: dataPtrReg, ToType: targetType})
				valToStore = castVal
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

	s.root.setBlock(endBB)
}

// -------------------------------------------------------------
// リターン文 (ReturnStmt)
// -------------------------------------------------------------

func (s *StmtLowerer) LowerReturnStmt(rs *ast.ReturnStmt) {
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
