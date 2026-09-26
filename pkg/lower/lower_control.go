package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

func (s *StmtLowerer) LowerIfStmt(is *ast.IfStmt) {
	initScope := s.snapshotDefinedSymbols(is.Init)
	if is.Init != nil {
		s.LowerStmt(is.Init)
	}

	condVal := s.root.Expr.LowerExpr(is.Condition)
	thenBB := s.root.newBlock("if.then")
	elseBB := s.root.newBlock("if.else")
	endBB := s.root.newBlock("if.end")
	var structuredIf *hir.IfNode
	var structuredContinuation *hir.BlockNode
	structuredParentDepth := len(s.root.structuredFrames)
	var structuredParentBody *hir.ControlBody
	if len(s.root.structuredStack) > 0 {
		structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
	}
	if len(s.root.structuredStack) > 0 {
		structuredIf = &hir.IfNode{Label: thenBB.Label, Cond: condVal}
		s.root.appendStructuredNode(structuredIf)
		// Keep a continuation immediately after the if.  It is replaced by
		// the next control node when one is emitted, or remains as the
		// ordinary-instruction container when the next item is not control.
		structuredContinuation = s.root.appendContinuationAfter(structuredParentBody, structuredIf, structuredParentDepth)
		structuredIf.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
	}
	if is.Alternative != nil {
		s.root.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: thenBB.Label, ElseTarget: elseBB.Label})
	} else {
		s.root.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: thenBB.Label, ElseTarget: endBB.Label})
	}

	s.root.setBlock(thenBB)
	thenScope := s.snapshotDefinedSymbols(is.Consequence)
	if structuredIf != nil {
		s.root.pushStructuredFrame(structuredIf.Label)
		s.root.pushStructuredBody(&structuredIf.Then)
	}
	s.LowerStmt(is.Consequence)
	s.restoreDefinedSymbols(thenScope)
	if structuredIf != nil {
		s.root.popStructuredBody()
		s.root.popStructuredFrame()
	}
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: endBB.Label})
	}

	if is.Alternative != nil {
		s.root.setBlock(elseBB)
		elseScope := s.snapshotDefinedSymbols(is.Alternative)
		if structuredIf != nil {
			s.root.pushStructuredFrame(structuredIf.Label)
			s.root.pushStructuredBody(&structuredIf.Else)
		}
		s.LowerStmt(is.Alternative)
		s.restoreDefinedSymbols(elseScope)
		if structuredIf != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
		}
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}
	}

	s.root.setBlock(endBB)
	s.restoreDefinedSymbols(initScope)
}

type symbolSnapshot struct {
	name     string
	value    hir.Value
	typ      sema.Type
	hasValue bool
	hasType  bool
}

func (s *StmtLowerer) snapshotDefinedSymbols(stmt ast.Statement) []symbolSnapshot {
	names := make(map[string]bool)
	var walk func(ast.Statement)
	walk = func(node ast.Statement) {
		switch n := node.(type) {
		case *ast.AssignStmt:
			if n.Token.Type == token.DEFINE {
				for _, left := range n.Left {
					if id, ok := left.(*ast.Identifier); ok && id.Value != "_" {
						names[id.Value] = true
					}
				}
			}
		case *ast.BlockStmt:
			for _, child := range n.Statements {
				walk(child)
			}
		case *ast.IfStmt:
			walk(n.Init)
			walk(n.Consequence)
			walk(n.Alternative)
		case *ast.ForStmt:
			walk(n.Init)
			walk(n.Body)
			walk(n.Post)
		case *ast.ForRangeStmt:
			walk(n.Body)
		case *ast.SwitchStmt:
			walk(n.Init)
			for _, clause := range n.Cases {
				for _, child := range clause.Body {
					walk(child)
				}
			}
		}
	}
	walk(stmt)
	result := make([]symbolSnapshot, 0, len(names))
	for name := range names {
		value, hasValue := s.root.symbols[name]
		typ, hasType := s.root.symbolTypes[name]
		result = append(result, symbolSnapshot{name: name, value: value, typ: typ, hasValue: hasValue, hasType: hasType})
	}
	return result
}

func (s *StmtLowerer) restoreDefinedSymbols(snapshot []symbolSnapshot) {
	for _, old := range snapshot {
		if old.hasValue {
			s.root.symbols[old.name] = old.value
		} else {
			delete(s.root.symbols, old.name)
		}
		if old.hasType {
			s.root.symbolTypes[old.name] = old.typ
		} else {
			delete(s.root.symbolTypes, old.name)
		}
	}
}

func (s *StmtLowerer) LowerForStmt(fs *ast.ForStmt) {
	var structuredBlock *hir.BlockNode
	var structuredLoop *hir.LoopNode
	structuredParentDepth := len(s.root.structuredFrames)
	var structuredParentBody *hir.ControlBody
	if len(s.root.structuredStack) > 0 {
		structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
	}
	condBB := s.root.newBlock("for.cond")
	bodyBB := s.root.newBlock("for.body")
	postBB := s.root.newBlock("for.post")
	endBB := s.root.newBlock("for.end")
	if len(s.root.structuredStack) > 0 {
		structuredBlock = &hir.BlockNode{Label: endBB.Label}
		structuredLoop = &hir.LoopNode{Label: condBB.Label}
		structuredLoop.Exit = structuredBlock
		s.root.appendStructuredNode(structuredBlock)
		s.root.pushStructuredFrame(structuredBlock.Label)
		s.root.appendStructuredNodeTo(&structuredBlock.Body, structuredLoop)
		s.root.pushStructuredFrame(structuredLoop.Label)
		s.root.pushStructuredBody(&structuredLoop.Body)
	}
	var structuredContinuation *hir.BlockNode
	if structuredBlock != nil {
		// The continuation is a sibling of the outer loop block, so it uses
		// the depth that was active before the loop's wrapper was opened.
		structuredContinuation = s.root.appendContinuationAfter(structuredParentBody, structuredBlock, structuredParentDepth)
		structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
		structuredLoop.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), structuredLoop.Index())
		structuredLoop.Continuation = structuredContinuation
	}

	s.root.loopStack = append(s.root.loopStack, loopContext{
		breakBlock: endBB, continueBlock: postBB,
		structured:        structuredLoop != nil,
		breakLabel:        structuredBlockLabel(structuredBlock),
		continueLabel:     structuredLoopLabel(structuredLoop),
		breakControlID:    controlID(structuredContinuation),
		continueControlID: controlID(structuredLoop),
		breakTarget:       controlID(structuredContinuation),
		continueTarget:    controlID(structuredLoop),
	})
	defer func() {
		// Nested Go-shaped range lowering can transfer control through a
		// function literal before the normal loop cleanup runs. Keep cleanup
		// idempotent so an already-restored loop stack cannot panic here.
		if len(s.root.loopStack) > 0 {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		}
		if structuredLoop != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
			s.root.popStructuredFrame()
		}
	}()

	// Keep the source-level initializer in the common structured loop shape.
	// The legacy CFG still sees the same instructions before the first
	// condition jump, while structured consumers can treat Init uniformly for
	// all for/range variants.
	if fs.Init != nil {
		if structuredLoop != nil {
			s.root.pushStructuredBody(&structuredLoop.Init)
			s.LowerStmt(fs.Init)
			s.root.popStructuredBody()
		} else {
			s.LowerStmt(fs.Init)
		}
	}

	s.root.terminate(&hir.InstrJump{Target: condBB.Label})

	s.root.setBlock(condBB)
	if fs.Cond != nil {
		condVal := s.root.Expr.LowerExpr(fs.Cond)
		if structuredLoop != nil {
			structuredLoop.Condition = condVal
		}
		var structuredCond *hir.IfNode
		if structuredLoop != nil {
			structuredCond = &hir.IfNode{Label: bodyBB.Label, Cond: condVal}
			s.root.appendStructuredNode(structuredCond)
			s.root.appendStructuredNodeTo(&structuredCond.Else, &hir.BrNode{Target: structuredBlockLabel(structuredBlock), TargetID: controlID(structuredBlock)})
			s.root.pushStructuredFrame(structuredCond.Label)
			s.root.pushStructuredBody(&structuredCond.Then)
		}
		s.root.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})
		if structuredCond != nil {
			// The loop body is lowered below while this structured scope remains
			// active; pop it immediately after lowering the source body.
		}
	} else {
		s.root.terminate(&hir.InstrJump{Target: bodyBB.Label})
	}

	s.root.setBlock(bodyBB)
	s.LowerStmt(fs.Body)
	if structuredLoop != nil && fs.Cond != nil {
		s.root.popStructuredBody()
		s.root.popStructuredFrame()
	}
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: postBB.Label})
	}

	s.root.setBlock(postBB)
	if structuredLoop != nil {
		// Post is a distinct structured region. Continue targets the loop ID,
		// and the CFG/WAT lowering resolves that target to this region.
		s.root.pushStructuredBody(&structuredLoop.Post)
	}
	if fs.Post != nil {
		s.LowerStmt(fs.Post)
	}
	if structuredLoop != nil {
		s.root.appendStructuredNode(&hir.BrNode{Target: structuredLoopLabel(structuredLoop), TargetID: controlID(structuredLoop)})
		s.root.popStructuredBody()
	}
	s.root.terminate(&hir.InstrJump{Target: condBB.Label})

	s.root.setBlock(endBB)
}

func structuredBlockLabel(node *hir.BlockNode) string {
	if node == nil {
		return ""
	}
	return node.Label
}

func structuredLoopLabel(node *hir.LoopNode) string {
	if node == nil {
		return ""
	}
	return node.Label
}

func controlID(node hir.ControlElement) int {
	if node == nil {
		return -1
	}
	// A typed nil pointer can be stored in the interface while a structured
	// construct is disabled (for example after an unsupported switch). Treat
	// it as absent before invoking the node method.
	switch n := node.(type) {
	case *hir.BlockNode:
		if n == nil {
			return -1
		}
	case *hir.LoopNode:
		if n == nil {
			return -1
		}
	case *hir.IfNode:
		if n == nil {
			return -1
		}
	case *hir.BrNode:
		if n == nil {
			return -1
		}
	case *hir.BrIfNode:
		if n == nil {
			return -1
		}
	case *hir.BrTableNode:
		if n == nil {
			return -1
		}
	case *hir.TableNode:
		if n == nil {
			return -1
		}
	}
	return node.Index()
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

	if isAsyncRecv && s.lowerAsyncRange(fr, targetExpr, xType) {
		return
	}

	if s.lowerIterableRange(fr, targetExpr, xVal, xType) {
		return
	}

	if s.lowerMapRange(fr, xVal, xType) {
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

	var structuredBlock *hir.BlockNode
	var structuredLoop *hir.LoopNode
	var structuredContinuation *hir.BlockNode
	structuredParentDepth := len(s.root.structuredFrames)
	var structuredParentBody *hir.ControlBody
	if len(s.root.structuredStack) > 0 {
		structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
	}
	if len(s.root.structuredStack) > 0 {
		structuredBlock = &hir.BlockNode{Label: "forrange.end"}
		structuredLoop = &hir.LoopNode{Label: "forrange.cond"}
		structuredLoop.Exit = structuredBlock
		s.root.appendStructuredNode(structuredBlock)
		s.root.pushStructuredFrame(structuredBlock.Label)
		s.root.appendStructuredNodeTo(&structuredBlock.Body, structuredLoop)
		s.root.pushStructuredFrame(structuredLoop.Label)
		s.root.pushStructuredBody(&structuredLoop.Body)
		structuredContinuation = s.root.appendContinuationAfter(structuredParentBody, structuredBlock, structuredParentDepth)
		structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
		structuredLoop.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), structuredLoop.Index())
		structuredLoop.Continuation = structuredContinuation
	}

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

	s.root.loopStack = append(s.root.loopStack, loopContext{
		breakBlock: endBB, continueBlock: postBB,
		structured:        structuredLoop != nil,
		breakLabel:        structuredBlockLabel(structuredBlock),
		continueLabel:     structuredLoopLabel(structuredLoop),
		breakControlID:    controlID(structuredContinuation),
		continueControlID: controlID(structuredLoop),
		breakTarget:       controlID(structuredContinuation),
		continueTarget:    controlID(structuredLoop),
	})
	defer func() {
		if len(s.root.loopStack) > 0 {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		}
		if structuredLoop != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
			s.root.popStructuredFrame()
		}
	}()

	s.root.terminate(&hir.InstrJump{Target: condBB.Label})

	s.root.setBlock(condBB)
	curIdx := s.root.nextReg(sema.TypeInt)
	s.root.emit(&hir.InstrLoad{Dst: curIdx, Ptr: idxAlloca})
	cmpReg := s.root.nextReg(sema.TypeBool)
	s.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpLt, L: curIdx, R: lenVal})
	if structuredLoop != nil {
		structuredLoop.Condition = cmpReg
	}
	var structuredCond *hir.IfNode
	if structuredLoop != nil {
		structuredCond = &hir.IfNode{Label: bodyBB.Label, Cond: cmpReg}
		s.root.appendStructuredNode(structuredCond)
		s.root.appendStructuredNodeTo(&structuredCond.Else, &hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
		s.root.pushStructuredFrame(structuredCond.Label)
		s.root.pushStructuredBody(&structuredCond.Then)
	}
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
	if structuredCond != nil {
		s.root.popStructuredBody()
		s.root.popStructuredFrame()
	}
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: postBB.Label})
	}

	s.root.setBlock(postBB)
	if structuredLoop != nil {
		s.root.pushStructuredBody(&structuredLoop.Post)
	}
	incIdx := s.root.nextReg(sema.TypeInt)
	s.root.emit(&hir.InstrBinary{Dst: incIdx, Op: hir.OpAdd, L: curIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
	s.root.emit(&hir.InstrStore{Val: incIdx, Ptr: idxAlloca})
	if structuredLoop != nil {
		s.root.appendStructuredNode(&hir.BrNode{Target: structuredLoop.Label, TargetID: structuredLoop.Index()})
		s.root.popStructuredBody()
	}
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

func (s *StmtLowerer) lowerMapRange(fr *ast.ForRangeStmt, xVal hir.Value, xType sema.Type) bool {
	// 3. 組み込み map[K]V の走査
	if mp, isMap := xType.(*sema.MapType); isMap {
		entryStructType := &sema.StructType{Name: "__hike_map_entry"}
		entryPtrType := &sema.PointerType{Base: entryStructType}
		// Keep this phantom type in sync with the runtime map ABI.  The map
		// lowering uses field pointers only to calculate byte offsets; leaving
		// Fields empty makes every field address use offset zero in backends
		// that do not otherwise materialize the runtime struct.
		entryStructType.Fields = []sema.Field{
			sema.Field{Name: "hash", Type: sema.TypeInt},
			sema.Field{Name: "key", Type: mp.Key},
			sema.Field{Name: "val", Type: mp.Value},
			sema.Field{Name: "next", Type: entryPtrType},
		}
		mapStructType := &sema.StructType{Name: "__hike_map", Fields: []sema.Field{
			sema.Field{Name: "buckets", Type: &sema.PointerType{Base: entryPtrType}},
			sema.Field{Name: "numBuckets", Type: sema.TypeInt},
			sema.Field{Name: "length", Type: sema.TypeInt},
			sema.Field{Name: "isString", Type: sema.TypeInt},
		}}
		mapPtrType := &sema.PointerType{Base: mapStructType}

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

		var structuredBlock *hir.BlockNode
		var structuredLoop *hir.LoopNode
		var structuredContinuation *hir.BlockNode
		var structuredInnerBlock *hir.BlockNode
		var structuredInnerLoop *hir.LoopNode
		structuredParentDepth := len(s.root.structuredFrames)
		var structuredParentBody *hir.ControlBody
		if len(s.root.structuredStack) > 0 {
			structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
		}
		if len(s.root.structuredStack) > 0 {
			structuredBlock = &hir.BlockNode{Label: endBB.Label}
			structuredLoop = &hir.LoopNode{Label: bCondBB.Label}
			structuredLoop.Exit = structuredBlock
			s.root.appendStructuredNode(structuredBlock)
			s.root.pushStructuredFrame(structuredBlock.Label)
			s.root.appendStructuredNodeTo(&structuredBlock.Body, structuredLoop)
			s.root.pushStructuredFrame(structuredLoop.Label)
			s.root.pushStructuredBody(&structuredLoop.Body)
			structuredContinuation = s.root.appendContinuationAfter(structuredParentBody, structuredBlock, structuredParentDepth)
			structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
			structuredLoop.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), structuredLoop.Index())
			structuredLoop.Continuation = structuredContinuation
		}

		s.root.loopStack = append(s.root.loopStack, loopContext{
			breakBlock: endBB, continueBlock: ePostBB,
			structured:        structuredLoop != nil,
			breakLabel:        structuredBlockLabel(structuredBlock),
			continueLabel:     structuredLoopLabel(structuredLoop),
			breakControlID:    controlID(structuredContinuation),
			continueControlID: controlID(structuredLoop),
			breakTarget:       controlID(structuredContinuation),
			continueTarget:    controlID(structuredLoop),
		})
		defer func() {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
			if structuredLoop != nil {
				s.root.popStructuredBody()
				s.root.popStructuredFrame()
				s.root.popStructuredFrame()
			}
		}()

		s.root.terminate(&hir.InstrJump{Target: bCondBB.Label})

		s.root.setBlock(bCondBB)
		curBIdx := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrLoad{Dst: curBIdx, Ptr: bIdxAlloca})
		cmpB := s.root.nextReg(sema.TypeBool)
		s.root.emit(&hir.InstrBinary{Dst: cmpB, Op: hir.OpLt, L: curBIdx, R: numBuckets})
		if structuredLoop != nil {
			structuredLoop.Condition = cmpB
		}
		var structuredOuterCond *hir.IfNode
		if structuredLoop != nil {
			structuredOuterCond = &hir.IfNode{Label: bBodyBB.Label, Cond: cmpB}
			s.root.appendStructuredNode(structuredOuterCond)
			s.root.appendStructuredNodeTo(&structuredOuterCond.Else, &hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
			s.root.pushStructuredFrame(structuredOuterCond.Label)
			s.root.pushStructuredBody(&structuredOuterCond.Then)
		}
		s.root.terminate(&hir.InstrBranch{Cond: cmpB, ThenTarget: bBodyBB.Label, ElseTarget: endBB.Label})

		s.root.setBlock(bBodyBB)
		pHead := s.root.nextReg(&sema.PointerType{Base: entryPtrType})
		s.root.emit(&hir.InstrGetElemPtr{Dst: pHead, BasePtr: buckets, Index: curBIdx})
		head := s.root.nextReg(entryPtrType)
		s.root.emit(&hir.InstrLoad{Dst: head, Ptr: pHead})
		s.root.emit(&hir.InstrStore{Val: head, Ptr: entryAlloca})
		if structuredLoop != nil {
			structuredInnerBlock = &hir.BlockNode{Label: bPostBB.Label}
			structuredInnerLoop = &hir.LoopNode{Label: eCondBB.Label}
			structuredInnerLoop.Exit = structuredInnerBlock
			s.root.appendStructuredNode(structuredInnerBlock)
			s.root.pushStructuredFrame(structuredInnerBlock.Label)
			s.root.appendStructuredNodeTo(&structuredInnerBlock.Body, structuredInnerLoop)
			s.root.pushStructuredFrame(structuredInnerLoop.Label)
			s.root.pushStructuredBody(&structuredInnerLoop.Body)
			innerNext := s.root.appendContinuationAfter(&structuredOuterCond.Then, structuredInnerBlock, len(s.root.structuredFrames)-2)
			structuredInnerBlock.SetControlLinks(innerNext.Index(), innerNext.Index(), -1)
			structuredInnerLoop.SetControlLinks(innerNext.Index(), innerNext.Index(), structuredInnerLoop.Index())
			structuredInnerLoop.Continuation = innerNext
		}
		s.root.terminate(&hir.InstrJump{Target: eCondBB.Label})

		s.root.setBlock(eCondBB)
		curE := s.root.nextReg(entryPtrType)
		s.root.emit(&hir.InstrLoad{Dst: curE, Ptr: entryAlloca})
		hasE := s.root.nextReg(sema.TypeBool)
		s.root.emit(&hir.InstrBinary{Dst: hasE, Op: hir.OpNeq, L: curE, R: &hir.ConstNil{Typ: entryPtrType}})
		if structuredInnerLoop != nil {
			structuredInnerLoop.Condition = hasE
		}
		var structuredInnerCond *hir.IfNode
		if structuredInnerLoop != nil {
			structuredInnerCond = &hir.IfNode{Label: eBodyBB.Label, Cond: hasE}
			s.root.appendStructuredNode(structuredInnerCond)
			s.root.appendStructuredNodeTo(&structuredInnerCond.Else, &hir.BrNode{Target: structuredInnerBlock.Label, TargetID: structuredInnerBlock.Index()})
			s.root.pushStructuredFrame(structuredInnerCond.Label)
			s.root.pushStructuredBody(&structuredInnerCond.Then)
		}
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
		if structuredInnerCond != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
		}
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
		if structuredInnerLoop != nil {
			s.root.pushStructuredBody(&structuredInnerLoop.Post)
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredInnerLoop.Label, TargetID: structuredInnerLoop.Index()})
			s.root.popStructuredBody()
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
			s.root.popStructuredFrame()
		}
		s.root.terminate(&hir.InstrJump{Target: eCondBB.Label})

		s.root.setBlock(bPostBB)
		if structuredLoop != nil {
			s.root.pushStructuredBody(&structuredLoop.Post)
		}
		nextB := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrBinary{Dst: nextB, Op: hir.OpAdd, L: curBIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
		s.root.emit(&hir.InstrStore{Val: nextB, Ptr: bIdxAlloca})
		if structuredLoop != nil {
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredLoop.Label, TargetID: structuredLoop.Index()})
			s.root.popStructuredBody()
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
		}
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
		return true
	}
	return false
}

func (s *StmtLowerer) lowerIterableRange(fr *ast.ForRangeStmt, targetExpr ast.Expression, xVal hir.Value, xType sema.Type) bool {
	// 2. ユーザー定義コレクション (Iterable / MapBehavior: InitIterator + Next) の走査
	objPtr := s.root.Expr.LowerStructPtr(targetExpr)
	initFnName, nextFnName, nextFn, finalRecv, hasInit, hasNext := s.resolveIterableMethods(xType, objPtr)

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

		var structuredBlock *hir.BlockNode
		var structuredLoop *hir.LoopNode
		var structuredContinuation *hir.BlockNode
		structuredParentDepth := len(s.root.structuredFrames)
		var structuredParentBody *hir.ControlBody
		if len(s.root.structuredStack) > 0 {
			structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
		}
		if len(s.root.structuredStack) > 0 {
			structuredBlock = &hir.BlockNode{Label: "iter.end"}
			structuredLoop = &hir.LoopNode{Label: "iter.cond"}
			structuredLoop.Exit = structuredBlock
			s.root.appendStructuredNode(structuredBlock)
			s.root.pushStructuredFrame(structuredBlock.Label)
			s.root.appendStructuredNodeTo(&structuredBlock.Body, structuredLoop)
			s.root.pushStructuredFrame(structuredLoop.Label)
			s.root.pushStructuredBody(&structuredLoop.Body)
			structuredContinuation = s.root.appendContinuationAfter(structuredParentBody, structuredBlock, structuredParentDepth)
			structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
			structuredLoop.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), structuredLoop.Index())
			structuredLoop.Continuation = structuredContinuation
		}

		condBB := s.root.newBlock("iter.cond")
		bodyBB := s.root.newBlock("iter.body")
		postBB := s.root.newBlock("iter.post")
		endBB := s.root.newBlock("iter.end")

		s.root.loopStack = append(s.root.loopStack, loopContext{
			breakBlock: endBB, continueBlock: postBB,
			structured:        structuredLoop != nil,
			breakLabel:        structuredBlockLabel(structuredBlock),
			continueLabel:     structuredLoopLabel(structuredLoop),
			breakControlID:    controlID(structuredContinuation),
			continueControlID: controlID(structuredLoop),
			breakTarget:       controlID(structuredContinuation),
			continueTarget:    controlID(structuredLoop),
		})
		defer func() {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
			if structuredLoop != nil {
				s.root.popStructuredBody()
				s.root.popStructuredFrame()
				s.root.popStructuredFrame()
			}
		}()

		s.root.terminate(&hir.InstrJump{Target: condBB.Label})

		s.root.setBlock(condBB)
		nextRes := s.root.nextReg(retTupleType)
		s.root.emit(&hir.InstrCallStatic{Dst: nextRes, CalleeName: nextFnName, Args: []hir.Value{finalRecv, bufReg}})
		okReg := s.root.nextReg(sema.TypeBool)
		s.root.emit(&hir.InstrExtractValue{Dst: okReg, Agg: nextRes, Index: okIndex})
		if structuredLoop != nil {
			structuredLoop.Condition = okReg
		}
		var structuredCond *hir.IfNode
		if structuredLoop != nil {
			structuredCond = &hir.IfNode{Label: bodyBB.Label, Cond: okReg}
			s.root.appendStructuredNode(structuredCond)
			s.root.appendStructuredNodeTo(&structuredCond.Else, &hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
			s.root.pushStructuredFrame(structuredCond.Label)
			s.root.pushStructuredBody(&structuredCond.Then)
		}
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
		if structuredCond != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
		}
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: postBB.Label})
		}

		s.root.setBlock(postBB)
		if structuredLoop != nil {
			s.root.pushStructuredBody(&structuredLoop.Post)
		}
		incIdx := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrBinary{Dst: incIdx, Op: hir.OpAdd, L: curIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
		s.root.emit(&hir.InstrStore{Val: incIdx, Ptr: idxAlloca})
		if structuredLoop != nil {
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredLoop.Label, TargetID: structuredLoop.Index()})
			s.root.popStructuredBody()
		}
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
		return true
	}
	return false
}

func (s *StmtLowerer) resolveIterableMethods(xType sema.Type, objPtr hir.Value) (string, string, *sema.FuncType, hir.Value, bool, bool) {
	initFnName, _, finalRecv, hasInit := s.root.Call.ResolveMethod(xType, "InitIterator", objPtr)
	nextFnName, nextFn, _, hasNext := s.root.Call.ResolveMethod(xType, "Next", objPtr)
	if strings.Contains(semaTypeName(xType), "__") {
		parts := strings.SplitN(strings.TrimPrefix(semaTypeName(xType), "*"), "__", 2)
		baseName, typeSuffix := parts[0], parts[1]
		if _, ok := s.root.semaCtx.Functions[fmt.Sprintf("%s_InitIterator_%s", baseName, typeSuffix)]; ok {
			initFnName, hasInit = fmt.Sprintf("%s_InitIterator_%s", baseName, typeSuffix), true
		}
		if fn, ok := s.root.semaCtx.Functions[fmt.Sprintf("%s_Next_%s", baseName, typeSuffix)]; ok {
			nextFnName, nextFn, hasNext = fmt.Sprintf("%s_Next_%s", baseName, typeSuffix), fn, true
		}
		if !hasInit || !hasNext {
			if st, _ := s.root.semaCtx.LookupStruct(baseName); st != nil {
				if !hasInit {
					initFnName, _, finalRecv, hasInit = s.root.Call.ResolveMethod(st, "InitIterator", objPtr)
				}
				if !hasNext {
					nextFnName, nextFn, _, hasNext = s.root.Call.ResolveMethod(st, "Next", objPtr)
				}
			}
		}
	}
	return initFnName, nextFnName, nextFn, finalRecv, hasInit, hasNext
}

func (s *StmtLowerer) lowerAsyncRange(fr *ast.ForRangeStmt, targetExpr ast.Expression, xType sema.Type) bool {
	var hasInit, hasNextChan bool
	var initFnName, nextChanFnName string
	var nextChanFn *sema.FuncType
	var finalRecv hir.Value
	var ifaceVal *hir.Reg
	var initMethodIndex, nextChanMethodIndex int
	_, isInterface := xType.(*sema.InterfaceType)

	objPtr := s.root.Expr.LowerStructPtr(targetExpr)
	if iface, ok := xType.(*sema.InterfaceType); ok && !iface.IsAny() {
		initMethod, initIdx := iface.GetMethod("InitIterator")
		nextMethod, nextIdx := iface.GetMethod("NextChannel")
		if initMethod != nil && nextMethod != nil {
			initMethodIndex = initIdx
			nextChanMethodIndex = nextIdx
			nextChanFn = &sema.FuncType{
				Name:        nextMethod.Name,
				ParamTypes:  nextMethod.ParamTypes,
				ReturnTypes: nextMethod.ReturnTypes,
			}
			hasInit = true
			hasNextChan = true
			ifaceVal = s.root.nextReg(iface)
			s.root.emit(&hir.InstrLoad{Dst: ifaceVal, Ptr: objPtr})
		}
	} else {
		initFnName, _, finalRecv, hasInit = s.root.Call.ResolveMethod(xType, "InitIterator", objPtr)
		nextChanFnName, nextChanFn, _, hasNextChan = s.root.Call.ResolveMethod(xType, "NextChannel", objPtr)
	}

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
		if !isInterface {
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
		}

		sizeReg := s.root.nextReg(sema.TypeInt)
		if isInterface {
			s.root.emit(&hir.InstrCallIface{
				Dst:         sizeReg,
				IfaceVal:    ifaceVal,
				MethodIndex: initMethodIndex,
				MethodName:  "InitIterator",
				Args:        []hir.Value{&hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}},
			})
		} else {
			s.root.emit(&hir.InstrCallStatic{Dst: sizeReg, CalleeName: initFnName, Args: []hir.Value{finalRecv, &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}}})
		}

		bufReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		s.root.emit(&hir.InstrAllocaDynamic{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte})
		if isInterface {
			s.root.emit(&hir.InstrCallIface{IfaceVal: ifaceVal, MethodIndex: initMethodIndex, MethodName: "InitIterator", Args: []hir.Value{bufReg}})
		} else {
			s.root.emit(&hir.InstrCallStatic{CalleeName: initFnName, Args: []hir.Value{finalRecv, bufReg}})
		}

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

		var structuredBlock *hir.BlockNode
		var structuredLoop *hir.LoopNode
		var structuredContinuation *hir.BlockNode
		structuredParentDepth := len(s.root.structuredFrames)
		var structuredParentBody *hir.ControlBody
		if len(s.root.structuredStack) > 0 {
			structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
		}
		if len(s.root.structuredStack) > 0 {
			structuredBlock = &hir.BlockNode{Label: "asynciter.end"}
			structuredLoop = &hir.LoopNode{Label: "asynciter.cond"}
			structuredLoop.Exit = structuredBlock
			s.root.appendStructuredNode(structuredBlock)
			s.root.pushStructuredFrame(structuredBlock.Label)
			s.root.appendStructuredNodeTo(&structuredBlock.Body, structuredLoop)
			s.root.pushStructuredFrame(structuredLoop.Label)
			s.root.pushStructuredBody(&structuredLoop.Body)
			structuredContinuation = s.root.appendContinuationAfter(structuredParentBody, structuredBlock, structuredParentDepth)
			structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
			structuredLoop.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), structuredLoop.Index())
			structuredLoop.Continuation = structuredContinuation
		}

		condBB := s.root.newBlock("asynciter.cond")
		bodyBB := s.root.newBlock("asynciter.body")
		postBB := s.root.newBlock("asynciter.post")
		endBB := s.root.newBlock("asynciter.end")

		s.root.loopStack = append(s.root.loopStack, loopContext{
			breakBlock: endBB, continueBlock: postBB,
			structured:        structuredLoop != nil,
			breakLabel:        structuredBlockLabel(structuredBlock),
			continueLabel:     structuredLoopLabel(structuredLoop),
			breakControlID:    controlID(structuredContinuation),
			continueControlID: controlID(structuredLoop),
			breakTarget:       controlID(structuredContinuation),
			continueTarget:    controlID(structuredLoop),
		})
		defer func() {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
			if structuredLoop != nil {
				s.root.popStructuredBody()
				s.root.popStructuredFrame()
				s.root.popStructuredFrame()
			}
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
		if isInterface {
			s.root.emit(&hir.InstrCallIface{Dst: nextRes, IfaceVal: ifaceVal, MethodIndex: nextChanMethodIndex, MethodName: "NextChannel", Args: []hir.Value{bufReg}})
		} else {
			s.root.emit(&hir.InstrCallStatic{Dst: nextRes, CalleeName: nextChanFnName, Args: []hir.Value{finalRecv, bufReg}})
		}
		okReg := s.root.nextReg(sema.TypeBool)
		s.root.emit(&hir.InstrExtractValue{Dst: okReg, Agg: nextRes, Index: 1})
		if structuredLoop != nil {
			structuredLoop.Condition = okReg
		}
		var structuredCond *hir.IfNode
		if structuredLoop != nil {
			structuredCond = &hir.IfNode{Label: bodyBB.Label, Cond: okReg}
			s.root.appendStructuredNode(structuredCond)
			s.root.appendStructuredNodeTo(&structuredCond.Else, &hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
			s.root.pushStructuredFrame(structuredCond.Label)
			s.root.pushStructuredBody(&structuredCond.Then)
		}
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
		if structuredCond != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
		}
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: postBB.Label})
		}

		s.root.setBlock(postBB)
		if structuredLoop != nil {
			s.root.pushStructuredBody(&structuredLoop.Post)
		}
		incIdx := s.root.nextReg(sema.TypeInt)
		s.root.emit(&hir.InstrBinary{Dst: incIdx, Op: hir.OpAdd, L: curIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
		s.root.emit(&hir.InstrStore{Val: incIdx, Ptr: idxAlloca})
		if structuredLoop != nil {
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredLoop.Label, TargetID: structuredLoop.Index()})
			s.root.popStructuredBody()
		}
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
		return true
	}
	return false
}

func numericSwitchCases(ss *ast.SwitchStmt) ([][]int64, int64, int64, bool) {
	if ss == nil {
		return nil, 0, 0, false
	}
	var cases [][]int64
	seen := make(map[int64]bool)
	var min, max int64
	haveValue := false
	for _, cc := range ss.Cases {
		if len(cc.Values) == 0 {
			continue
		}
		values := make([]int64, 0, len(cc.Values))
		for _, expr := range cc.Values {
			literal, ok := expr.(*ast.IntegerLiteral)
			if !ok || seen[literal.Value] {
				return nil, 0, 0, false
			}
			seen[literal.Value] = true
			values = append(values, literal.Value)
			if !haveValue || literal.Value < min {
				min = literal.Value
			}
			if !haveValue || literal.Value > max {
				max = literal.Value
			}
			haveValue = true
		}
		cases = append(cases, values)
	}
	if !haveValue || max-min > 4096 {
		return nil, 0, 0, false
	}
	return cases, min, max, true
}
