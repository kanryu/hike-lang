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
	stringBuckets, stringHashSwitch := stringSwitchBuckets(ss, s.root.semaCtx, s.root.is32Bit, switchVal.Type())
	if len(s.root.structuredStack) > 0 && (stringHashSwitch || numericSwitch) {
		if stringHashSwitch {
			s.lowerStructuredBucketSwitch(ss, switchVal, endBB, stringBuckets, true)
		} else {
			s.lowerStructuredBucketSwitch(ss, switchVal, endBB, nil, false)
		}
		s.root.setBlock(endBB)
		return
	}
	var structuredBlock *hir.BlockNode
	var structuredTable *hir.TableNode
	var structuredContinuation *hir.BlockNode
	var structuredCaseBody *hir.ControlBody
	structuredParentDepth := len(s.root.structuredFrames)
	var structuredParentBody *hir.ControlBody
	if len(s.root.structuredStack) > 0 {
		structuredParentBody = s.root.structuredStack[len(s.root.structuredStack)-1]
	}
	structuredBlock, structuredTable, structuredContinuation, structuredCaseBody = s.setupStructuredSwitch(endBB, switchVal, numericValues, tableMin, tableMax, numericSwitch, structuredParentBody, structuredParentDepth)

	s.root.loopStack = append(s.root.loopStack, loopContext{
		breakBlock: endBB, continueBlock: endBB,
		structured:     structuredBlock != nil,
		breakLabel:     structuredBlockLabel(structuredBlock),
		breakControlID: controlID(structuredContinuation),
		breakTarget:    controlID(structuredContinuation),
	})
	defer func() {
		if len(s.root.loopStack) > 0 {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		}
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

			if vVal.Type() == sema.TypeString && switchVal.Type() == sema.TypeString {
				left := switchVal
				right := vVal
				leftPtr, leftLen := s.root.stringParts(left)
				rightPtr, rightLen := s.root.stringParts(right)
				cmpReg = s.root.nextReg(sema.TypeBool)
				s.root.emit(&hir.InstrCallStatic{Dst: cmpReg, CalleeName: s.root.BuiltinName("hike_streq_len"), Args: []hir.Value{leftPtr, leftLen, rightPtr, rightLen}})
			} else if vVal.Type() == sema.TypeCString || switchVal.Type() == sema.TypeCString {
				cmpReg = s.root.nextReg(sema.TypeBool)
				s.root.emit(&hir.InstrCallStatic{Dst: cmpReg, CalleeName: s.root.BuiltinName("hike_streq"), Args: []hir.Value{switchVal, vVal}})
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
			// A switch without a default still needs an explicit exit from
			// the final failed case condition.  Without this edge, the
			// structured representation leaves the final else body attached
			// to the preceding case chain, which can create a back-edge and
			// loop forever when no case matches.
			if structuredTable == nil {
				s.root.appendStructuredNode(&hir.BrNode{
					Target:   structuredBlock.Label,
					TargetID: structuredBlock.Index(),
				})
			}
		}
	}
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: endBB.Label})
	}

	s.root.setBlock(endBB)
}

// lowerStructuredBucketSwitch is shared by integer and string switches. The
// table receives an eight-bit dispatch index; exact case predicates remain in
// the selected table body as structured IfNodes.
func (s *StmtLowerer) lowerStructuredBucketSwitch(ss *ast.SwitchStmt, switchVal hir.Value, endBB *basicBlock, buckets map[uint64][]string, stringMode bool) {
	entriesByHash := make(map[uint64]map[int][]ast.Expression)
	var hashOrder []uint64
	seenHash := make(map[uint64]bool)
	var defaultCase *ast.CaseClause
	entryCount := 0

	for _, cc := range ss.Cases {
		if len(cc.Values) == 0 {
			defaultCase = cc
			continue
		}
		for _, expr := range cc.Values {
			entryCount++
			if stringMode {
				if _, ok := stringSwitchCaseValue(expr, s.root.semaCtx); !ok {
					return
				}
			} else if _, ok := expr.(*ast.IntegerLiteral); !ok {
				return
			}
		}
	}
	if entryCount == 0 || (stringMode && len(buckets) == 0) || len(s.root.structuredStack) == 0 {
		return
	}
	tableSize := switchTableSize(entryCount)
	entriesByHash = make(map[uint64]map[int][]ast.Expression)
	hashOrder = nil
	seenHash = make(map[uint64]bool)
	for caseIndex, cc := range ss.Cases {
		if len(cc.Values) == 0 {
			continue
		}
		for _, expr := range cc.Values {
			hash, ok := switchBucketForExpr(expr, s.root.semaCtx, s.root.is32Bit, stringMode, tableSize)
			if !ok {
				return
			}
			if !seenHash[hash] {
				seenHash[hash] = true
				hashOrder = append(hashOrder, hash)
			}
			if entriesByHash[hash] == nil {
				entriesByHash[hash] = make(map[int][]ast.Expression)
			}
			entriesByHash[hash][caseIndex] = append(entriesByHash[hash][caseIndex], expr)
		}
	}

	parentBody := s.root.structuredStack[len(s.root.structuredStack)-1]
	parentDepth := len(s.root.structuredFrames)
	structuredBlock := &hir.BlockNode{Label: endBB.Label}
	s.root.appendStructuredNode(structuredBlock)
	structuredContinuation := s.root.appendContinuationAfter(parentBody, structuredBlock, parentDepth)
	structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
	s.root.pushStructuredFrame(structuredBlock.Label)
	s.root.pushStructuredBody(&structuredBlock.Body)

	s.root.loopStack = append(s.root.loopStack, loopContext{
		breakBlock: endBB, continueBlock: endBB,
		structured:     true,
		breakLabel:     structuredBlock.Label,
		breakControlID: structuredContinuation.Index(),
		breakTarget:    controlID(structuredContinuation),
	})
	defer func() {
		s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		s.root.popStructuredBody()
		s.root.popStructuredFrame()
	}()

	var hashValue hir.Value
	if stringMode {
		hashValue = s.lowerStringSwitchHash(switchVal, tableSize)
	} else {
		unsigned := s.root.nextReg(sema.TypeUint)
		s.root.emit(&hir.InstrCast{Dst: unsigned, Val: switchVal, ToType: sema.TypeUint})
		dispatch := s.root.nextReg(sema.TypeUint)
		hashValue = dispatch
		s.root.emit(&hir.InstrBinary{Dst: dispatch, Op: hir.OpRem, L: unsigned, R: &hir.ConstInt{Val: int64(tableSize), Typ: sema.TypeUint}})
	}
	// Reuse the numeric structured table.  The hash has already been reduced
	// to the uint8 dispatch index by lowerStringSwitchHash.
	structuredTable := &hir.TableNode{
		Label:         structuredBlock.Label + ".hash.table",
		IndexValue:    hashValue,
		Min:           0,
		Targets:       make([]int, tableSize),
		DefaultTarget: -1,
	}
	for i := range structuredTable.Targets {
		structuredTable.Targets[i] = -1
	}
	s.root.appendStructuredNodeTo(&structuredBlock.Body, structuredTable)

	for _, hash := range hashOrder {
		caseIndex := len(structuredTable.Cases)
		structuredTable.Cases = append(structuredTable.Cases, hir.TableCase{Values: []int64{int64(hash)}})
		structuredTable.Targets[hash] = caseIndex
		bucketBody := s.root.newBlock("switch.hash.bucket")
		s.root.setBlock(bucketBody)
		s.root.structuredStack[len(s.root.structuredStack)-1] = &structuredTable.Cases[caseIndex].Body

		ordered := make([]int, 0, len(entriesByHash[hash]))
		for caseIndex := range ss.Cases {
			if _, ok := entriesByHash[hash][caseIndex]; ok {
				ordered = append(ordered, caseIndex)
			}
		}
		for pos, sourceCaseIndex := range ordered {
			var matchedCond hir.Value
			for _, expr := range entriesByHash[hash][sourceCaseIndex] {
				value := s.root.Expr.LowerExpr(expr)
				cmp := s.root.nextReg(sema.TypeBool)
				if stringMode {
					leftPtr, leftLen := s.root.stringParts(switchVal)
					rightPtr, rightLen := s.root.stringParts(value)
					s.root.emit(&hir.InstrCallStatic{Dst: cmp, CalleeName: s.root.BuiltinName("hike_streq_len"), Args: []hir.Value{leftPtr, leftLen, rightPtr, rightLen}})
				} else {
					s.root.emit(&hir.InstrBinary{Dst: cmp, Op: hir.OpEq, L: switchVal, R: value})
				}
				if matchedCond == nil {
					matchedCond = cmp
				} else {
					or := s.root.nextReg(sema.TypeBool)
					s.root.emit(&hir.InstrBinary{Dst: or, Op: hir.OpOr, L: matchedCond, R: cmp})
					matchedCond = or
				}
			}

			caseBody := s.root.newBlock("switch.hash.case.body")
			collisionNext := s.root.newBlock("switch.hash.collision.next")
			caseIf := &hir.IfNode{Label: caseBody.Label, Cond: matchedCond}
			s.root.appendStructuredNode(caseIf)
			s.root.terminate(&hir.InstrBranch{Cond: matchedCond, ThenTarget: caseBody.Label, ElseTarget: collisionNext.Label})

			s.root.setBlock(caseBody)
			s.root.pushStructuredFrame(caseIf.Label)
			s.root.pushStructuredBody(&caseIf.Then)
			for _, stmt := range ss.Cases[sourceCaseIndex].Body {
				s.LowerStmt(stmt)
			}
			s.root.appendStructuredNode(&hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
			if s.root.curBlock.Terminator == nil {
				s.root.terminate(&hir.InstrJump{Target: endBB.Label})
			}

			s.root.setBlock(collisionNext)
			s.root.structuredStack[len(s.root.structuredStack)-1] = &caseIf.Else
			if pos == len(ordered)-1 && defaultCase == nil {
				s.root.appendStructuredNode(&hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
			}
		}
		if defaultCase != nil {
			for _, stmt := range defaultCase.Body {
				s.LowerStmt(stmt)
			}
		}
		if s.root.curBlock.Terminator == nil {
			s.root.terminate(&hir.InstrJump{Target: endBB.Label})
		}
	}

	if defaultCase != nil {
		s.root.structuredStack[len(s.root.structuredStack)-1] = &structuredTable.Default
		for _, stmt := range defaultCase.Body {
			s.LowerStmt(stmt)
		}
	} else {
		s.root.structuredStack[len(s.root.structuredStack)-1] = &structuredTable.Default
		s.root.appendStructuredNode(&hir.BrNode{Target: structuredBlock.Label, TargetID: structuredBlock.Index()})
	}
	if s.root.curBlock.Terminator == nil {
		s.root.terminate(&hir.InstrJump{Target: endBB.Label})
	}
}

// stringSwitchBuckets returns the compile-time hash buckets for a string
// switch.  The bucket value is a slice because distinct case strings may have
// the same hash and must remain an equality chain inside that bucket.
func stringSwitchBuckets(ss *ast.SwitchStmt, ctx *sema.Context, is32 bool, switchType sema.Type) (map[uint64][]string, bool) {
	if ss == nil || ctx == nil || (switchType != sema.TypeString && semaTypeName(switchType) != "string") {
		return nil, false
	}
	buckets := make(map[uint64][]string)
	caseCount := 0
	for _, cc := range ss.Cases {
		if len(cc.Values) == 0 {
			continue
		}
		caseCount++
		for _, expr := range cc.Values {
			value, ok := stringSwitchCaseValue(expr, ctx)
			if !ok {
				return nil, false
			}
			hash := stringSwitchHash(value, is32)
			buckets[hash] = append(buckets[hash], value)
		}
	}
	if caseCount < 4 {
		return nil, false
	}
	return buckets, true
}

func stringSwitchCaseValue(expr ast.Expression, ctx *sema.Context) (string, bool) {
	if expr == nil || ctx == nil {
		return "", false
	}
	switch e := expr.(type) {
	case *ast.StringLiteral:
		return e.Value, true
	case *ast.Identifier:
		return ctx.LookupStringConstant(astIDValue(e))
	case *ast.MemberExpr:
		if pkg, ok := e.Object.(*ast.Identifier); ok {
			return ctx.LookupStringConstant(astIDValue(pkg) + "_" + astIDValue(e.Field))
		}
	case *ast.BinaryExpr:
		if e.Operator == "+" {
			left, leftOK := stringSwitchCaseValue(e.Left, ctx)
			right, rightOK := stringSwitchCaseValue(e.Right, ctx)
			if leftOK && rightOK {
				return left + right, true
			}
		}
	}
	return "", false
}

func switchBucketForExpr(expr ast.Expression, ctx *sema.Context, is32, stringMode bool, tableSize int) (uint64, bool) {
	if tableSize <= 0 {
		return 0, false
	}
	if stringMode {
		value, ok := stringSwitchCaseValue(expr, ctx)
		if !ok {
			return 0, false
		}
		return stringSwitchHash(value, is32) % uint64(tableSize), true
	}
	literal, ok := expr.(*ast.IntegerLiteral)
	if !ok || literal == nil {
		return 0, false
	}
	value := literal.Value % int64(tableSize)
	if value < 0 {
		value += int64(tableSize)
	}
	return uint64(value), true
}

func stringSwitchHash(value string, is32 bool) uint64 {
	if is32 {
		h := uint32(2166136261)
		for i := 0; i < len(value); i++ {
			h ^= uint32(value[i])
			h *= 16777619
		}
		return uint64(h)
	}
	h := uint64(14695981039346656037)
	for i := 0; i < len(value); i++ {
		h ^= uint64(value[i])
		h *= 1099511628211
	}
	return h
}

func stringSwitchBucket(value string, is32 bool, tableSize int) uint64 {
	return stringSwitchHash(value, is32) % uint64(tableSize)
}

func (s *StmtLowerer) lowerStringSwitchHash(value hir.Value, tableSize int) hir.Value {
	ptr, length := s.root.stringParts(value)
	key := s.root.nextReg(sema.TypeInt)
	s.root.emit(&hir.InstrCallStatic{Dst: key, CalleeName: s.root.BuiltinName("__hike_string_key"), Args: []hir.Value{ptr, length}})
	keyPtr := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	s.root.emit(&hir.InstrCast{Dst: keyPtr, Val: key, ToType: keyPtr.Type()})
	hash := s.root.nextReg(sema.TypeInt)
	s.root.emit(&hir.InstrCallStatic{Dst: hash, CalleeName: s.root.BuiltinName("__hike_hash_str"), Args: []hir.Value{keyPtr}})
	// __hike_string_key allocates only the temporary descriptor used by the
	// hash routine.  Free it immediately after hashing; the string bytes remain
	// owned by the switch expression.
	s.root.emit(&hir.InstrCallStatic{CalleeName: s.root.BuiltinName("free"), Args: []hir.Value{keyPtr}})
	// Structured switch tables use an 8-bit dispatch index.  The full hash is
	// still used by the compile-time bucket map; collisions are resolved by the
	// exact string predicates generated in the selected table case.
	unsigned := s.root.nextReg(sema.TypeUint)
	s.root.emit(&hir.InstrCast{Dst: unsigned, Val: hash, ToType: sema.TypeUint})
	dispatch := s.root.nextReg(sema.TypeUint)
	s.root.emit(&hir.InstrBinary{Dst: dispatch, Op: hir.OpRem, L: unsigned, R: &hir.ConstInt{Val: int64(tableSize), Typ: sema.TypeUint}})
	return dispatch
}

func switchTableSize(entryCount int) int {
	for _, size := range []int{17, 31, 61, 127, 251} {
		if entryCount*5 <= size*2 {
			return size
		}
	}
	return 251
}

func (s *StmtLowerer) setupStructuredSwitch(endBB *basicBlock, switchVal hir.Value, numericValues [][]int64, tableMin, tableMax int64, numericSwitch bool, parentBody *hir.ControlBody, parentDepth int) (*hir.BlockNode, *hir.TableNode, *hir.BlockNode, *hir.ControlBody) {
	if len(s.root.structuredStack) == 0 {
		return nil, nil, nil, nil
	}
	structuredBlock := &hir.BlockNode{Label: endBB.Label}
	var tableIndex hir.Value
	if numericSwitch {
		tableIndex = switchVal
		if tableMin != 0 {
			reg := s.root.nextReg(switchVal.Type())
			tableIndex = reg
			s.root.emit(&hir.InstrBinary{Dst: reg, Op: hir.OpSub, L: switchVal, R: &hir.ConstInt{Val: tableMin, Typ: sema.TypeInt}})
		}
	}
	s.root.appendStructuredNode(structuredBlock)
	var structuredTable *hir.TableNode
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
	structuredContinuation := s.root.appendContinuationAfter(parentBody, structuredBlock, parentDepth)
	structuredBlock.SetControlLinks(structuredContinuation.Index(), structuredContinuation.Index(), -1)
	s.root.pushStructuredFrame(structuredBlock.Label)
	s.root.pushStructuredBody(&structuredBlock.Body)
	return structuredBlock, structuredTable, structuredContinuation, &structuredBlock.Body
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
		if len(s.root.loopStack) > 0 {
			s.root.loopStack = s.root.loopStack[:len(s.root.loopStack)-1]
		}
		if structuredBlock != nil {
			s.root.popStructuredBody()
			s.root.popStructuredFrame()
		}
	}()

	dataPtrReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	actualTypeIDReg := s.root.nextReg(sema.TypeInt32)

	if it, ok := exprType.(*sema.InterfaceType); ok && !sema.InterfaceType_IsAny(it) {
		itabRawReg := s.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		e := s.root
		e.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 0})
		e.emit(&hir.InstrExtractValue{Dst: itabRawReg, Agg: exprVal, Index: 1})
		// A nil interface has no itab.  Use the null-safe runtime helper
		// instead of casting the itab to an integer pointer and loading it.
		e.emit(&hir.InstrCallStatic{Dst: actualTypeIDReg, CalleeName: "__hike_iface_typeid", Args: []hir.Value{itabRawReg}})
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
	tailCallCandidate := len(rs.Values) == 1 && isCallExpression(rs.Values[0])
	s.root.tailCallCleanupPending = tailCallCandidate
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
	// Mark only after all return-value copying, coercion, and explicit operand
	// cleanup. A future backend may replace this call with a machine tail call,
	// so the call must still be the final instruction in the block.
	if len(rs.Values) == 1 {
		if isCallExpression(rs.Values[0]) {
			s.root.markLastStaticCallTail(vals[0])
		}
	}
	s.root.tailCallCleanupPending = false

	s.root.terminate(&hir.InstrReturn{Vals: vals})
}

func isCallExpression(expr ast.Expression) bool {
	switch expr.(type) {
	case *ast.CallExpr:
		return true
	default:
		return false
	}
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
			// Parameters are borrowed by the callee.  Releasing one here would
			// decrement the caller's reference, and the caller may release the
			// same string again after the call.  Only locally owned symbols may
			// be consumed by this return-expression cleanup.
			if s.isCurrentFunctionParameter(name) {
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
			s.root.releaseStringNamed(value, name)
		case *ast.BinaryExpr:
			visit(n.Left)
			visit(n.Right)
		}
	}
	visit(expr.Left)
	visit(expr.Right)
}

func (s *StmtLowerer) isCurrentFunctionParameter(name string) bool {
	if s.root.curFunc == nil {
		return false
	}
	for _, param := range s.root.curFunc.Params {
		if param == nil {
			continue
		}
		if param.Name == name || strings.HasPrefix(param.Name, name+"_arg.") {
			return true
		}
	}
	return false
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
