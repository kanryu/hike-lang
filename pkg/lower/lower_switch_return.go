package lower

import (
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

func (s *StmtLowerer) LowerSwitchStmt(ss *ast.SwitchStmt) {
	if ss.Init != nil {
		s.LowerStmt(ss.Init)
	}

	switchVal := s.root.Expr.LowerExpr(ss.Value)
	endBB := s.root.newBlock("switch.end")
	numericValues, tableMin, tableMax, numericSwitch := numericSwitchCases(ss)
	var structuredBlock *hir.BlockNode
	var structuredTable *hir.TableNode
	var structuredContinuation *hir.BlockNode
	var structuredCaseBody *hir.ControlBody
	structuredParentDepth := len(s.root.structuredFrames)
	var structuredParentBody *hir.ControlBody
	if len(s.root.structuredStack) > 0 {
		structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
	}
	if len(s.root.structuredStack) > 0 {
		structuredBlock = &hir.BlockNode{Label: endBB.Label}
		var tableIndex hir.Value
		if numericSwitch {
			tableIndex = switchVal
			if tableMin != 0 {
				reg := s.root.nextReg(switchVal.Type())
				tableIndex = reg
				// The table index is an ordinary instruction and must be
				// emitted before the TableNode. Otherwise structuredDestination
				// places it in the table continuation, after dispatch.
				s.root.emit(&hir.InstrBinary{Dst: reg, Op: hir.OpSub, L: switchVal, R: &hir.ConstInt{Val: tableMin, Typ: sema.TypeInt}})
			}
		}
		s.root.appendStructuredNode(structuredBlock)
		if numericSwitch {
			tableTargets := make([]int, int(tableMax-tableMin+1))
			for i := range tableTargets {
				tableTargets[i] = -1
			}
			structuredTable = &hir.TableNode{Label: endBB.Label + ".table", IndexValue: tableIndex, Min: tableMin, Targets: tableTargets, DefaultTarget: -1}
			for caseIndex, values := range numericValues {
				structuredTable.Cases = append(structuredTable.Cases, hir.TableCase{Values: values})
				for _, value := range values {
					structuredTable.Targets[value-tableMin] = caseIndex
				}
			}
			s.root.appendStructuredNodeTo(&structuredBlock.Body, structuredTable)
		}
		structuredContinuation = s.root.appendContinuationAfter(structuredParentBody, structuredBlock, structuredParentDepth)
		structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
		s.root.pushStructuredFrame(structuredBlock.Label)
		s.root.pushStructuredBody(&structuredBlock.Body)
		structuredCaseBody = &structuredBlock.Body
	}

	s.root.loopStack = append(s.root.loopStack, loopContext{
		breakBlock: endBB, continueBlock: endBB,
		structured:     structuredBlock != nil,
		breakLabel:     structuredBlockLabel(structuredBlock),
		breakControlID: controlID(structuredContinuation),
		breakTarget:    controlID(structuredContinuation),
	})
	defer func() {
		s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		if structuredBlock != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
		}
	}()

	var defaultCase *ast.CaseClause = nil
	caseOrdinal := 0

	for _, cc := range ss.Cases {
		if len(cc.Values) == 0 {
			defaultCase = cc
			continue
		}

		caseBodyBB := s.root.newBlock("switch.case.body")
		nextCaseBB := s.root.newBlock("switch.case.next")

		var matchedCond hir.Value = nil
		structuredStack := s.root.structuredStack
		if structuredTable != nil {
			// Keep case comparisons in the legacy CFG only; the structured
			// table dispatches directly by the integer index.
			s.root.structuredStack = nil
		} else if structuredBlock != nil && len(s.root.structuredStack) > 0 {
			// Build the next case condition in the current else body. If the
			// condition instructions are emitted before this switch, they can
			// land after the whole if-chain in structured HIR.
			s.root.structuredStack[len(s.root.structuredStack)-1] = structuredCaseBody
		}
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
		s.root.structuredStack = structuredStack

		var structuredCase *hir.IfNode
		if structuredBlock != nil && structuredTable == nil {
			// Each case is represented as an if/else chain inside the
			// switch block. This preserves structured control without
			// introducing a backend-specific switch node yet.
			structuredCase = &hir.IfNode{Label: caseBodyBB.Label, Cond: matchedCond}
			if len(s.root.structuredStack) > 0 {
				s.root.structuredStack[len(s.root.structuredStack)-1] = structuredCaseBody
			}
			s.root.appendStructuredNode(structuredCase)
			s.root.pushStructuredFrame(structuredCase.Label)
			s.root.pushStructuredBody(&structuredCase.Then)
		} else if structuredTable != nil {
			s.root.structuredStack[len(s.root.structuredStack)-1] = &structuredTable.Cases[caseOrdinal].Body
		}
		caseOrdinal++

		s.root.terminate(&hir.InstrBranch{Cond: matchedCond, ThenTarget: caseBodyBB.Label, ElseTarget: nextCaseBB.Label})

		s.root.setBlock(caseBodyBB)
		for _, stmt := range cc.Body {
			s.LowerStmt(stmt)
		}
		if structuredCase != nil {
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
			structuredCaseBody = &structuredCase.Else
		} else if structuredTable != nil {
			if len(s.root.structuredStack) > 0 {
				s.root.structuredStack[len(s.root.structuredStack)-1] = &structuredBlock.Body
			}
		}
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}

		s.root.setBlock(nextCaseBB)
	}

	if defaultCase != nil {
		if structuredBlock != nil && len(s.root.structuredStack) > 0 {
			if structuredTable != nil {
				structuredCaseBody = &structuredTable.Default
			}
			s.root.structuredStack[len(s.root.structuredStack)-1] = structuredCaseBody
		}
		for _, stmt := range defaultCase.Body {
			s.LowerStmt(stmt)
		}
		if structuredBlock != nil && structuredTable == nil {
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
		}
	}
	if structuredBlock != nil {
		if defaultCase == nil {
			if len(s.root.structuredStack) > 0 {
				s.root.structuredStack[len(s.root.structuredStack)-1] = structuredCaseBody
			}
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
	var structuredBlock *hir.BlockNode
	var structuredContinuation *hir.BlockNode
	var structuredCaseBody *hir.ControlBody
	structuredParentDepth := len(s.root.structuredFrames)
	var structuredParentBody *hir.ControlBody
	if len(s.root.structuredStack) > 0 {
		structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
	}
	if len(s.root.structuredStack) > 0 {
		structuredBlock = &hir.BlockNode{Label: endBB.Label}
		s.root.appendStructuredNode(structuredBlock)
		structuredContinuation = s.root.appendContinuationAfter(structuredParentBody, structuredBlock, structuredParentDepth)
		structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
		s.root.pushStructuredFrame(structuredBlock.Label)
		s.root.pushStructuredBody(&structuredBlock.Body)
		structuredCaseBody = &structuredBlock.Body
	}

	s.root.loopStack = append(s.root.loopStack, loopContext{
		breakBlock: endBB, continueBlock: endBB,
		structured:     structuredBlock != nil,
		breakLabel:     structuredBlockLabel(structuredBlock),
		breakControlID: controlID(structuredContinuation),
		breakTarget:    controlID(structuredContinuation),
	})
	defer func() {
		s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		if structuredBlock != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
		}
	}()

	dataPtrReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	actualTypeIDReg := s.root.nextReg(sema.TypeInt32)

	if it, ok := exprType.(*sema.InterfaceType); ok && !it.IsAny() {
		itabRawReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e := s.root
		e.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 0})
		e.emit(&hir.InstrExtractValue{Dst: itabRawReg, Agg: exprVal, Index: 1})
		typeIDPtr := s.root.nextReg(&sema.PointerType{Base: sema.TypeInt32})
		e.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabRawReg, ToType: &sema.PointerType{Base: sema.TypeInt32}})
		e.emit(&hir.InstrLoad{Dst: actualTypeIDReg, Ptr: typeIDPtr})
	} else {
		s.root.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 1})
		s.root.emit(&hir.InstrExtractValue{Dst: actualTypeIDReg, Agg: exprVal, Index: 0})
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
		if structuredBlock != nil && len(s.root.structuredStack) > 0 {
			s.root.structuredStack[len(s.root.structuredStack)-1] = structuredCaseBody
		}
		if c.IsNil {
			cmpReg := s.root.nextReg(sema.TypeBool)
			s.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpEq, L: actualTypeIDReg, R: &hir.ConstInt{Val: 0, Typ: sema.TypeInt32}})
			matchedCond = cmpReg
		}
		for _, tExpr := range c.Types {
			targetType := s.root.semaCtx.ResolveType(tExpr)
			targetTypeID := targetType.TypeID(s.root.semaCtx)

			cmpReg := s.root.nextReg(sema.TypeBool)
			s.root.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpEq, L: actualTypeIDReg, R: &hir.ConstInt{Val: targetTypeID, Typ: sema.TypeInt32}})

			if matchedCond == nil {
				matchedCond = cmpReg
			} else {
				orReg := s.root.nextReg(sema.TypeBool)
				s.root.emit(&hir.InstrBinary{Dst: orReg, Op: hir.OpOr, L: matchedCond, R: cmpReg})
				matchedCond = orReg
			}
		}

		var structuredCase *hir.IfNode
		if structuredBlock != nil {
			structuredCase = &hir.IfNode{Label: caseBodyBB.Label, Cond: matchedCond}
			if len(s.root.structuredStack) > 0 {
				s.root.structuredStack[len(s.root.structuredStack)-1] = structuredCaseBody
			}
			s.root.appendStructuredNode(structuredCase)
			s.root.pushStructuredFrame(structuredCase.Label)
			s.root.pushStructuredBody(&structuredCase.Then)
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
		if structuredCase != nil {
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
			structuredCaseBody = &structuredCase.Else
		}
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}

		s.root.setBlock(nextCaseBB)
	}

	if defaultCase != nil {
		if structuredBlock != nil && len(s.root.structuredStack) > 0 {
			s.root.structuredStack[len(s.root.structuredStack)-1] = structuredCaseBody
		}
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
		if structuredBlock != nil {
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
		}
	}
	if structuredBlock != nil && defaultCase == nil && len(s.root.structuredStack) > 0 {
		s.root.structuredStack[len(s.root.structuredStack)-1] = structuredCaseBody
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

	for i := len(s.root.deferStack) - 1; i >= 0; i-- {
		s.root.Call.LowerCall(s.root.deferStack[i])
	}

	vals := make([]hir.Value, len(rs.Values))
	for i, v := range rs.Values {
		val := s.root.Expr.LowerExpr(v)
		if val == nil || (func() bool { r, ok := val.(*hir.Reg); return ok && r == nil })() {
			if s.root.curFunc != nil && i < len(s.root.curFunc.ReturnTypes) {
				val = s.root.defaultConstValue(s.root.curFunc.ReturnTypes[i])
			} else {
				val = s.root.defaultConstValue(sema.TypeInt)
			}
		}
		// A slice produced by an intermediate expression may still refer to
		// an area-backed or otherwise temporary buffer.  Return an owned heap
		// copy; a direct variable return preserves that variable's ownership.
		if _, isSlice := val.Type().(*sema.SliceType); isSlice && !isDirectSliceIdentifier(v) {
			val = s.root.Call.lowerDeepCopyValue(val, val.Type(), make(map[sema.Type]bool))
		}
		if s.root.curFunc != nil && i < len(s.root.curFunc.ReturnTypes) {
			val = s.root.emitValueCoerce(val, s.root.curFunc.ReturnTypes[i])
		}
		vals[i] = val
	}

	// String concatenation creates an independent heap buffer.  The operands
	// are no longer needed after the result has been produced, so release
	// local string references consumed by a returned `A + B` expression.  A
	// plain `return A` is deliberately excluded: the returned string owns the
	// reference and must remain alive after the function's region ends.
	for _, expr := range rs.Values {
		if be, ok := expr.(*ast.BinaryExpr); ok && be.Operator == "+" {
			s.releaseReturnedStringOperands(be)
		}
	}

	s.root.terminate(&hir.InstrReturn{Vals: vals})
}

func isDirectSliceIdentifier(expr ast.Expression) bool {
	_, ok := expr.(*ast.Identifier)
	return ok
}

func (s *StmtLowerer) releaseReturnedStringOperands(expr *ast.BinaryExpr) {
	seen := make(map[string]bool)
	var visit func(ast.Expression)
	visit = func(node ast.Expression) {
		switch n := node.(type) {
		case *ast.Identifier:
			name := astIDValue(n)
			if name == "" || seen[name] || s.root.symbols[name] == nil {
				return
			}
			// Global variables are borrowed references and must not be released
			// as if they were local ownerships.
			if _, global := s.root.symbols[name].(*hir.GlobalVar); global {
				return
			}
			if !s.root.isStringType(s.root.symbolTypes[name]) {
				return
			}
			seen[name] = true
			// Local symbols name their storage slot, while releaseString
			// operates on the string value stored in that slot.
			value := s.root.nextReg(s.root.symbolTypes[name])
			s.root.emit(&hir.InstrLoad{Dst: value, Ptr: s.root.symbols[name]})
			s.root.releaseString(value)
		case *ast.BinaryExpr:
			visit(n.Left)
			visit(n.Right)
		}
	}
	visit(expr.Left)
	visit(expr.Right)
}

func (s *StmtLowerer) lowerTupleReturn(rs *ast.ReturnStmt) bool {
	if len(rs.Values) != 1 || s.root.curFunc == nil || len(s.root.curFunc.ReturnTypes) <= 1 {
		return false
	}
	for i := len(s.root.deferStack) - 1; i >= 0; i-- {
		s.root.Call.LowerCall(s.root.deferStack[i])
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
	s.root.terminate(&hir.InstrReturn{Vals: vals})
	return true
}
