package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

// -----------------------------------------------------------------------------
// 関数定義 (Function Declaration) の変換
// -----------------------------------------------------------------------------

func (c *CallLowerer) LowerFunc(fn *ast.FuncDecl) {
	restoreLocation := c.root.setTokenLocation(fn.Filename, fn.Token)
	defer restoreLocation()
	c.resetFunctionState(fn)
	fnName, recvType := c.resolveFunctionIdentity(fn)
	irName := c.resolveFuncIRName(fnName)

	isMain := (fn.Name.Value == "main" && fn.Receiver == nil)
	returnTypes := c.resolveFunctionReturnTypes(fn, fnName, recvType, isMain)

	// C言語スタイルの可変長引数を持ち、本体を持つ関数は呼び出し側でインライン展開
	if c.isUntypedVariadicBody(fn) {
		return
	}

	hirFn := c.createFunction(fn, irName, returnTypes)
	c.root.initFunctionControl(hirFn)

	if fn.Body == nil {
		c.lowerExternSignature(fn, hirFn)
		return
	}

	entryBB := &basicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(entryBB)

	if isMain {
		c.lowerMainArguments(hirFn)
	} else {
		c.lowerFunctionParameters(fn, hirFn, recvType)
		c.lowerNamedReturns(fn, returnTypes)
	}

	for _, stmt := range fn.Body.Statements {
		c.root.loweringGlobalInit = isMain && c.root.globalInitRemaining > 0
		if c.root.loweringGlobalInit {
			c.root.globalInitRemaining--
		}
		c.root.Stmt.LowerStmt(stmt)
	}
	c.root.loweringGlobalInit = false

	// フォールスルー時（明示的returnがない場合）のみdeferを呼んでデフォルトリターンを生成
	if c.root.curBlock.Terminator == nil {
		c.emitFallthroughReturn(isMain, returnTypes)
	}
	hirFn.StructuredBody = c.root.structuredRoot
	hir.FlattenTransparentBlocks(hirFn)
}

// lowerNamedReturns materializes Go-style named result variables as ordinary
// local slots. The parser keeps their names separately from ReturnTypes, so
// assignments such as `x = ...` and `return x` use the same symbol table as
// parameters and local variables.
func (c *CallLowerer) lowerNamedReturns(fn *ast.FuncDecl, returnTypes []sema.Type) {
	for i, name := range fn.ReturnNames {
		if name == "" || i >= len(returnTypes) {
			continue
		}
		resultType := returnTypes[i]
		ptr := c.root.nextReg(&sema.PointerType{Base: resultType}, name)
		c.root.emit(&hir.InstrAlloca{Dst: ptr, AllocType: resultType})
		c.root.emit(&hir.InstrStore{Val: c.root.defaultConstValue(resultType), Ptr: ptr})
		c.root.symbols[name] = ptr
		c.root.symbolTypes[name] = resultType
	}
}

func (c *CallLowerer) resetFunctionState(fn *ast.FuncDecl) {
	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	// Global identifiers are lowered through GlobalVar values, but their type
	// must also be visible to statement-level alias analysis.  In particular,
	// assigning a global string to a local variable creates another reference.
	for name, typ := range c.root.semaCtx.Globals {
		c.root.symbolTypes[name] = typ
	}
	c.root.deferStack = []*ast.CallExpr{}
	c.root.areaStack = []*hir.Reg{}
	c.root.resetStructuredState()
	c.root.legacyBlocks = nil
	c.root.regCount = 0
	c.root.escapedVars = make(map[string]bool)
	if fn.Body != nil {
		c.root.escapedVars = sema.CollectAllCapturesInBlock(fn.Body)
		c.root.stringMutationCounts, c.root.stringMutationInLoop = countStringMutations(fn.Body)
	} else {
		c.root.stringMutationCounts = make(map[string]int)
		c.root.stringMutationInLoop = make(map[string]bool)
	}
}

func (c *CallLowerer) resolveFuncIRName(fnName string) string {
	fnMeta, _ := c.root.semaCtx.LookupFunction(fnName)
	if fnMeta != nil && semaFuncIRName(fnMeta) != "" {
		return semaFuncIRName(fnMeta)
	}
	return fnName
}

func (c *CallLowerer) resolveFunctionIdentity(fn *ast.FuncDecl) (string, sema.Type) {
	fnName := fn.Name.Value
	var recvType sema.Type
	if fn.Receiver == nil {
		return fnName, recvType
	}
	recvType = c.root.semaCtx.ResolveType(fn.Receiver.Type)
	recvName := strings.TrimPrefix(semaTypeName(recvType), "*")
	return sema.CanonicalMethodName(recvName, fnName), recvType
}

func (c *CallLowerer) isUntypedVariadicBody(fn *ast.FuncDecl) bool {
	if fn.Body == nil || !fn.IsVariadic {
		return false
	}
	for _, param := range fn.Params {
		if param.IsVariadic {
			return false
		}
	}
	return true
}

func (c *CallLowerer) resolveFunctionReturnTypes(fn *ast.FuncDecl, fnName string, recvType sema.Type, isMain bool) []sema.Type {
	if isMain {
		return []sema.Type{sema.TypeInt}
	}
	if fnType := c.root.semaCtx.Functions[fnName]; fnType != nil {
		return fnType.ReturnTypes
	}
	if fn.Receiver != nil {
		if method, _ := c.root.semaCtx.LookupMethod(semaTypeName(recvType), fn.Name.Value); method != nil {
			return method.ReturnTypes
		}
	}
	return c.resolveReturnTypeExpressions(fn.ReturnTypes)
}

func (c *CallLowerer) resolveReturnTypeExpressions(exprs []ast.TypeExpr) []sema.Type {
	returnTypes := make([]sema.Type, 0, len(exprs))
	for _, expr := range exprs {
		returnTypes = append(returnTypes, c.root.semaCtx.ResolveType(expr))
	}
	return returnTypes
}

func (c *CallLowerer) createFunction(fn *ast.FuncDecl, irName string, returnTypes []sema.Type) *hir.Function {
	hirFn := &hir.Function{
		Name:        irName,
		Location:    hir.SourceLocation{Filename: fn.Filename, Line: fn.Token.Line, Column: fn.Token.Col},
		Params:      []*hir.Reg{},
		ReturnTypes: returnTypes,
		IsVariadic:  fn.Body == nil && fn.IsVariadic,
		IsExtern:    fn.Body == nil,
	}
	c.root.curFunc = hirFn
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)
	return hirFn
}

func (c *CallLowerer) lowerExternSignature(fn *ast.FuncDecl, hirFn *hir.Function) {
	if fn.Name.Value == "os_now_ns" || fn.Name.Value == "os_sleep_ms" {
		return
	}
	for i, param := range fn.Params {
		paramType := c.root.semaCtx.ResolveType(param.Type)
		if param.IsVariadic {
			if _, isSlice := paramType.(*sema.SliceType); !isSlice {
				paramType = &sema.SliceType{Elem: paramType}
			}
		}
		hirFn.Params = append(hirFn.Params, &hir.Reg{ID: i + 1, Typ: paramType, Name: param.Name.Value})
	}
}

func (c *CallLowerer) emitFallthroughReturn(isMain bool, returnTypes []sema.Type) {
	for i := len(c.root.deferStack) - 1; i >= 0; i-- {
		c.LowerCall(c.root.deferStack[i])
	}
	if isMain {
		c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{&hir.ConstInt{Val: 0, Typ: sema.TypeInt}}})
		return
	}
	if len(returnTypes) == 0 {
		c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		return
	}
	defaults := make([]hir.Value, len(returnTypes))
	for i, rt := range returnTypes {
		defaults[i] = c.root.defaultConstValue(rt)
	}
	c.root.terminate(&hir.InstrReturn{Vals: defaults})
}

func (c *CallLowerer) lowerMainArguments(hirFn *hir.Function) {
	argc32Reg := c.root.nextReg(&sema.BasicType{Name: "int32", ByteSize: 4, LLVM: "i32"}, "argc")
	argvReg := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}, "argv")
	hirFn.Params = append(hirFn.Params, argc32Reg, argvReg)

	argcReg := c.root.nextReg(sema.TypeInt, "argc.val")
	c.root.emit(&hir.InstrCast{Dst: argcReg, Val: argc32Reg, ToType: sema.TypeInt})
	if _, exists := c.root.semaCtx.Globals["os_Args"]; !exists {
		return
	}
	c.lowerOSArgs(argvReg, argcReg)
}

func (c *CallLowerer) lowerOSArgs(argvReg *hir.Reg, argcReg *hir.Reg) {
	callocRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
	c.root.emit(&hir.InstrCallStatic{
		Dst:        callocRaw,
		CalleeName: "calloc",
		Args:       []hir.Value{argcReg, &hir.ConstInt{Val: int64(sema.PointerSize), Typ: sema.TypeInt}},
	})
	callocRes := c.root.nextReg(&sema.PointerType{Base: sema.TypeString})
	c.root.emit(&hir.InstrCast{Dst: callocRes, Val: callocRaw, ToType: &sema.PointerType{Base: sema.TypeString}})

	loopCondBB := c.root.newBlock("argv.loop.cond")
	loopBodyBB := c.root.newBlock("argv.loop.body")
	loopEndBB := c.root.newBlock("argv.loop.end")
	idxAlloca := c.root.nextReg(&sema.PointerType{Base: sema.TypeInt}, "i.arg")
	c.root.emit(&hir.InstrAlloca{Dst: idxAlloca, AllocType: sema.TypeInt})
	c.root.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: idxAlloca})
	c.root.terminate(&hir.InstrJump{Target: loopCondBB.Label})

	c.root.setBlock(loopCondBB)
	curI := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrLoad{Dst: curI, Ptr: idxAlloca})
	cmp := c.root.nextReg(sema.TypeBool)
	c.root.emit(&hir.InstrBinary{Dst: cmp, Op: hir.OpLt, L: curI, R: argcReg})
	c.root.terminate(&hir.InstrBranch{Cond: cmp, ThenTarget: loopBodyBB.Label, ElseTarget: loopEndBB.Label})

	c.root.setBlock(loopBodyBB)
	srcElemPtr := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
	c.root.emit(&hir.InstrGetElemPtr{Dst: srcElemPtr, BasePtr: argvReg, Index: curI})
	argCStr := c.root.nextReg(sema.TypeCString)
	c.root.emit(&hir.InstrLoad{Dst: argCStr, Ptr: srcElemPtr})
	argStr := c.lowerCStringToString(argCStr)
	dstElemPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeString})
	c.root.emit(&hir.InstrGetElemPtr{Dst: dstElemPtr, BasePtr: callocRes, Index: curI})
	c.root.emit(&hir.InstrStore{Val: argStr, Ptr: dstElemPtr})
	nextI := c.root.nextReg(sema.TypeInt)
	c.root.emit(&hir.InstrBinary{Dst: nextI, Op: hir.OpAdd, L: curI, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
	c.root.emit(&hir.InstrStore{Val: nextI, Ptr: idxAlloca})
	c.root.terminate(&hir.InstrJump{Target: loopCondBB.Label})

	c.root.setBlock(loopEndBB)
	sliceType := &sema.SliceType{Elem: sema.TypeString}
	t1 := c.root.nextReg(sliceType)
	c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(sliceType), Val: callocRaw, Index: 0})
	t2 := c.root.nextReg(sliceType)
	c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: argcReg, Index: 1})
	t3 := c.root.nextReg(sliceType)
	c.root.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: argcReg, Index: 2})
	c.root.emit(&hir.InstrStore{Val: t3, Ptr: &hir.GlobalVar{Name: "os_Args", Typ: &sema.PointerType{Base: sliceType}}})
}

func (c *CallLowerer) lowerFunctionParameters(fn *ast.FuncDecl, hirFn *hir.Function, recvType sema.Type) {
	if fn.Receiver != nil {
		c.lowerReceiverParameter(fn.Receiver, hirFn, recvType)
	}

	for _, param := range fn.Params {
		c.lowerParameter(param, hirFn)
	}
}

func (c *CallLowerer) lowerReceiverParameter(receiver *ast.ParamDecl, hirFn *hir.Function, recvType sema.Type) {
	paramReg := c.root.nextReg(recvType, receiver.Name.Value+"_arg")
	hirFn.Params = append(hirFn.Params, paramReg)
	ptrReg := c.root.nextReg(&sema.PointerType{Base: recvType}, receiver.Name.Value)
	if receiver.IsEscaped {
		sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(recvType)), Typ: sema.TypeInt}
		c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: recvType, KeepOnHeap: true})
	} else {
		c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: recvType})
	}
	c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
	c.root.symbols[receiver.Name.Value] = ptrReg
	c.root.symbolTypes[receiver.Name.Value] = recvType
}

func (c *CallLowerer) lowerParameter(param *ast.ParamDecl, hirFn *hir.Function) {
	paramType := c.root.semaCtx.ResolveType(param.Type)
	if param.IsVariadic {
		if _, isSlice := paramType.(*sema.SliceType); !isSlice {
			paramType = &sema.SliceType{Elem: paramType}
		}
	}
	paramReg := c.root.nextReg(paramType, param.Name.Value+"_arg")
	hirFn.Params = append(hirFn.Params, paramReg)
	ptrReg := c.root.nextReg(&sema.PointerType{Base: paramType}, param.Name.Value)
	if param.IsEscaped || c.root.escapedVars[param.Name.Value] {
		sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(paramType)), Typ: sema.TypeInt}
		c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: paramType, KeepOnHeap: true})
	} else {
		c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: paramType})
	}
	c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
	c.root.symbols[param.Name.Value] = ptrReg
	c.root.symbolTypes[param.Name.Value] = paramType
}

// -------------------------------------------------------------
// 外部 C 関数宣言 (ExternFunc Declaration) の変換
// -------------------------------------------------------------

func (c *CallLowerer) LowerExternFunc(efn *ast.ExternFuncDecl) {
	cName := efn.Name.Value
	if efn.TargetCName != nil {
		cName = efn.TargetCName.Value
	}

	returnTypes := c.resolveExternReturnTypes(efn)

	params := []*hir.Reg{}
	for i, p := range efn.Params {
		pType := c.root.semaCtx.ResolveType(p.Type)
		if p.IsVariadic {
			if _, isSlice := pType.(*sema.SliceType); !isSlice {
				pType = &sema.SliceType{Elem: pType}
			}
		}
		params = append(params, &hir.Reg{ID: i + 1, Typ: pType, Name: p.Name.Value})
	}

	hirFn := &hir.Function{
		Name:        cName,
		Params:      params,
		ReturnTypes: returnTypes,
		IsVariadic:  efn.IsVariadic,
		IsExtern:    true,
		IsCFunc:     true,
	}
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)
}

func (c *CallLowerer) resolveExternReturnTypes(efn *ast.ExternFuncDecl) []sema.Type {
	if fnType := c.root.semaCtx.Functions[efn.Name.Value]; fnType != nil && len(fnType.ReturnTypes) > 0 {
		return fnType.ReturnTypes
	}
	return c.resolveReturnTypeExpressions(efn.ReturnTypes)
}

// -------------------------------------------------------------
// C 連携関数 (CFunc Declaration) の変換 (Dual ABI: 実体 + トランポリン)
// -------------------------------------------------------------

func (c *CallLowerer) LowerCFunc(cfn *ast.CFuncDecl) {
	targetCName := c.resolveCFuncTargetName(cfn)

	returnTypes := c.resolveCFuncReturnTypes(cfn)

	// 1. 外部シンボル参照エイリアス (= c_func_name) の場合
	if cfn.IsAlias() || cfn.Body == nil {
		c.lowerExternalCFunc(cfn, targetCName, returnTypes)
		return
	}

	// 2. 手書きブロックを持つ具象 cfunc の場合
	// ① Hike 内部用実体関数 (__hike_impl_<Name>) をコンパイル
	implName := "__hike_impl_" + cfn.Name.Value

	c.resetCFuncState(cfn)

	implFn := c.newCFuncImplementation(cfn, implName, returnTypes)
	c.root.resetStructuredState()
	c.root.initFunctionControl(implFn)

	entryBB := &basicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(entryBB)

	trampolineParamTypes := c.lowerCFuncParameters(cfn, implFn)

	for _, stmt := range cfn.Body.Statements {
		c.root.Stmt.LowerStmt(stmt)
	}

	if c.root.curBlock.Terminator == nil {
		c.emitCFuncFallthroughReturn(returnTypes)
	}
	implFn.StructuredBody = c.root.structuredRoot
	hir.FlattenTransparentBlocks(implFn)
	c.root.legacyBlocks = nil

	// ② 外部公開用トランポリン関数 (<TargetCName>) を生成
	trampolineFn := c.newCFuncTrampoline(cfn, targetCName, returnTypes)
	c.root.resetStructuredState()
	c.root.initFunctionControl(trampolineFn)

	tEntryBB := &basicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(tEntryBB)

	callArgs := c.lowerCFuncTrampolineParameters(trampolineParamTypes, trampolineFn)

	var callDst *hir.Reg = nil
	if len(returnTypes) == 1 {
		callDst = c.root.nextReg(returnTypes[0])
	} else if len(returnTypes) > 1 {
		callDst = c.root.nextReg(&sema.TupleType{Types: returnTypes})
	}

	c.root.emit(&hir.InstrCallStatic{
		Dst:        callDst,
		CalleeName: implName,
		Args:       callArgs,
	})

	c.emitCFuncTrampolineReturn(callDst, returnTypes)
	trampolineFn.StructuredBody = c.root.structuredRoot
	hir.FlattenTransparentBlocks(trampolineFn)
	c.root.legacyBlocks = nil
}

func (c *CallLowerer) emitCFuncTrampolineReturn(callDst *hir.Reg, returnTypes []sema.Type) {
	if len(returnTypes) == 1 {
		c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{callDst}})
		return
	}
	if len(returnTypes) == 0 {
		c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		return
	}

	retVals := make([]hir.Value, len(returnTypes))
	for i, rt := range returnTypes {
		elemReg := c.root.nextReg(rt)
		c.root.emit(&hir.InstrExtractValue{Dst: elemReg, Agg: callDst, Index: i})
		retVals[i] = elemReg
	}
	c.root.terminate(&hir.InstrReturn{Vals: retVals})
}

func (c *CallLowerer) lowerCFuncTrampolineParameters(paramTypes []sema.Type, fn *hir.Function) []hir.Value {
	callArgs := []hir.Value{}
	for i, pType := range paramTypes {
		pReg := c.root.nextReg(pType, fmt.Sprintf("arg_%d", i))
		fn.Params = append(fn.Params, pReg)
		callArgs = append(callArgs, pReg)
	}
	return callArgs
}

func (c *CallLowerer) emitCFuncFallthroughReturn(returnTypes []sema.Type) {
	for i := len(c.root.deferStack) - 1; i >= 0; i-- {
		c.LowerCall(c.root.deferStack[i])
	}

	if len(returnTypes) == 0 {
		c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		return
	}

	defaults := make([]hir.Value, len(returnTypes))
	for i, rt := range returnTypes {
		defaults[i] = c.root.defaultConstValue(rt)
	}
	c.root.terminate(&hir.InstrReturn{Vals: defaults})
}

func (c *CallLowerer) lowerCFuncParameters(cfn *ast.CFuncDecl, implFn *hir.Function) []sema.Type {
	paramTypes := []sema.Type{}
	for _, p := range cfn.Params {
		pType := c.root.semaCtx.ResolveType(p.Type)
		paramTypes = append(paramTypes, pType)

		paramReg := c.root.nextReg(pType, p.Name.Value+"_arg")
		implFn.Params = append(implFn.Params, paramReg)

		ptrReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
		if p.IsEscaped || c.root.escapedVars[p.Name.Value] {
			sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(pType)), Typ: sema.TypeInt}
			c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: pType})
		} else {
			c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: pType})
		}
		c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
		c.root.symbols[p.Name.Value] = ptrReg
		c.root.symbolTypes[p.Name.Value] = pType
	}
	return paramTypes
}

func (c *CallLowerer) newCFuncTrampoline(cfn *ast.CFuncDecl, name string, returnTypes []sema.Type) *hir.Function {
	c.root.regCount = 0
	trampolineFn := &hir.Function{
		Name: name,
		Location: hir.SourceLocation{
			Filename: c.root.sourceFile,
			Line:     cfn.Token.Line,
			Column:   cfn.Token.Col,
		},
		Params:        []*hir.Reg{},
		ReturnTypes:   returnTypes,
		IsVariadic:    cfn.IsVariadic,
		IsExtern:      false,
		IsCFunc:       true,
		IsPassThrough: cfn.IsPassThrough,
		CFuncTarget:   name,
	}
	c.root.curFunc = trampolineFn
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, trampolineFn)
	return trampolineFn
}

func (c *CallLowerer) newCFuncImplementation(cfn *ast.CFuncDecl, name string, returnTypes []sema.Type) *hir.Function {
	implFn := &hir.Function{
		Name: name,
		Location: hir.SourceLocation{
			Filename: c.root.sourceFile,
			Line:     cfn.Token.Line,
			Column:   cfn.Token.Col,
		},
		Params:        []*hir.Reg{},
		ReturnTypes:   returnTypes,
		IsVariadic:    cfn.IsVariadic,
		IsExtern:      false,
		IsCFunc:       false,
		IsPassThrough: cfn.IsPassThrough,
	}
	c.root.curFunc = implFn
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, implFn)
	return implFn
}

func (c *CallLowerer) resetCFuncState(cfn *ast.CFuncDecl) {
	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	c.root.deferStack = []*ast.CallExpr{}
	c.root.regCount = 0
	c.root.legacyBlocks = nil
	c.root.resetStructuredState()
	c.root.escapedVars = sema.CollectAllCapturesInBlock(cfn.Body)
}

func (c *CallLowerer) resolveCFuncTargetName(cfn *ast.CFuncDecl) string {
	if cfn.TargetCName != nil {
		return cfn.TargetCName.Value
	}
	return cfn.Name.Value
}

func (c *CallLowerer) lowerExternalCFunc(cfn *ast.CFuncDecl, targetCName string, returnTypes []sema.Type) {
	params := []*hir.Reg{}
	for i, p := range cfn.Params {
		pType := c.root.semaCtx.ResolveType(p.Type)
		if p.IsVariadic {
			if _, isSlice := pType.(*sema.SliceType); !isSlice {
				pType = &sema.SliceType{Elem: pType}
			}
		}
		params = append(params, &hir.Reg{ID: i + 1, Typ: pType, Name: p.Name.Value})
	}

	hirFn := &hir.Function{
		Name:          targetCName,
		Params:        params,
		ReturnTypes:   returnTypes,
		IsVariadic:    cfn.IsVariadic,
		IsExtern:      true,
		IsCFunc:       true,
		IsPassThrough: cfn.IsPassThrough,
		CFuncTarget:   targetCName,
	}
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)
}

func (c *CallLowerer) resolveCFuncReturnTypes(cfn *ast.CFuncDecl) []sema.Type {
	if fnType := c.root.semaCtx.Functions[cfn.Name.Value]; fnType != nil && len(fnType.ReturnTypes) > 0 {
		return fnType.ReturnTypes
	}

	returnTypes := []sema.Type{}
	for _, rt := range cfn.ReturnTypes {
		returnTypes = append(returnTypes, c.root.semaCtx.ResolveType(rt))
	}
	return returnTypes
}

// -------------------------------------------------------------
// クロージャ / 無名関数 (FuncLit) の Lowering
// -------------------------------------------------------------

func (c *CallLowerer) LowerFuncLit(fl *ast.FuncLit) hir.Value {
	c.root.anonFuncCount++
	anonName := fmt.Sprintf("__anon_func_%d", c.root.anonFuncCount)

	ft := c.root.semaCtx.InferExprType(fl, nil).(*sema.FuncType)
	anonFn := &hir.Function{
		Name:        anonName,
		Params:      []*hir.Reg{},
		ReturnTypes: ft.ReturnTypes,
		IsVariadic:  fl.IsVariadic,
		IsExtern:    false,
	}

	rawCaptures := sema.ScanCapturesFromLit(fl)
	captures := []string{}
	for _, name := range rawCaptures {
		if _, ok := c.root.symbols[name]; ok {
			captures = append(captures, name)
		}
	}

	prevFunc := c.root.curFunc
	prevBlock := c.root.curBlock
	prevLegacyBlocks := c.root.legacyBlocks
	prevSymbols := c.root.symbols
	prevTypes := c.root.symbolTypes
	prevLoopStack := c.root.loopStack
	prevDeferStack := c.root.deferStack
	prevAreaStack := c.root.areaStack
	prevStructuredRoot := c.root.structuredRoot
	prevStructuredStack := c.root.structuredStack
	prevStructuredFrames := c.root.structuredFrames
	prevEscapedVars := c.root.escapedVars
	prevStringMutationCounts := c.root.stringMutationCounts
	prevStringMutationInLoop := c.root.stringMutationInLoop
	prevRegCount := c.root.regCount

	c.root.curFunc = anonFn
	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	c.root.legacyBlocks = nil
	c.root.loopStack = []loopContext{}
	c.root.deferStack = []*ast.CallExpr{}
	c.root.areaStack = []*hir.Reg{}
	c.root.resetStructuredState()
	c.root.initFunctionControl(anonFn)
	c.root.regCount = 0
	if fl.Body != nil {
		c.root.escapedVars = sema.CollectAllCapturesInBlock(fl.Body)
		c.root.stringMutationCounts, c.root.stringMutationInLoop = countStringMutations(fl.Body)
	} else {
		c.root.escapedVars = make(map[string]bool)
		c.root.stringMutationCounts = make(map[string]int)
		c.root.stringMutationInLoop = make(map[string]bool)
	}

	var envParamReg *hir.Reg
	if len(captures) > 0 {
		envParamReg = c.root.nextReg(&sema.PointerType{Base: sema.TypeByte}, "__env_arg")
		anonFn.Params = append(anonFn.Params, envParamReg)
	}

	anonEntry := &basicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(anonEntry)

	if len(captures) > 0 {
		envTyped := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		c.root.emit(&hir.InstrCast{Dst: envTyped, Val: envParamReg, ToType: &sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}})

		for idx, name := range captures {
			symType := prevTypes[name]
			slot := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
			c.root.emit(&hir.InstrGetElemPtr{Dst: slot, BasePtr: envTyped, Index: &hir.ConstInt{Val: int64(idx), Typ: sema.TypeInt}})
			rawPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			c.root.emit(&hir.InstrLoad{Dst: rawPtr, Ptr: slot})
			typedPtr := c.root.nextReg(&sema.PointerType{Base: symType})
			c.root.emit(&hir.InstrCast{Dst: typedPtr, Val: rawPtr, ToType: &sema.PointerType{Base: symType}})
			c.root.symbols[name] = typedPtr
			c.root.symbolTypes[name] = symType
		}
	}

	for i, p := range fl.Params {
		pType := c.root.semaCtx.ResolveType(p.Type)
		if p.IsVariadic || (fl.IsVariadic && i == len(fl.Params)-1) {
			if _, isSlice := pType.(*sema.SliceType); !isSlice {
				pType = &sema.SliceType{Elem: pType}
			}
		}
		pReg := c.root.nextReg(pType, p.Name.Value+"_arg")
		anonFn.Params = append(anonFn.Params, pReg)
		allocaReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
		if p.IsEscaped || c.root.escapedVars[p.Name.Value] {
			sizeVal := &hir.ConstInt{Val: int64(sema.SizeOf(pType)), Typ: sema.TypeInt}
			c.root.emit(&hir.InstrHeapAlloc{Dst: allocaReg, Size: sizeVal, AllocType: pType})
		} else {
			c.root.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: pType})
		}
		c.root.emit(&hir.InstrStore{Val: pReg, Ptr: allocaReg})
		c.root.symbols[p.Name.Value] = allocaReg
		c.root.symbolTypes[p.Name.Value] = pType
	}

	// Materialize named results for Go-style function literals as local slots.
	// They are ordinary locals from the lowering layer's perspective, just like
	// named results on a function declaration.
	for i, name := range fl.ReturnNames {
		if name == "" || i >= len(ft.ReturnTypes) {
			continue
		}
		resultType := ft.ReturnTypes[i]
		ptr := c.root.nextReg(&sema.PointerType{Base: resultType}, name)
		c.root.emit(&hir.InstrAlloca{Dst: ptr, AllocType: resultType})
		c.root.emit(&hir.InstrStore{Val: c.root.defaultConstValue(resultType), Ptr: ptr})
		c.root.symbols[name] = ptr
		c.root.symbolTypes[name] = resultType
	}

	for _, s := range fl.Body.Statements {
		c.root.Stmt.LowerStmt(s)
	}

	if c.root.curBlock.Terminator == nil {
		for i := len(c.root.deferStack) - 1; i >= 0; i-- {
			c.LowerCall(c.root.deferStack[i])
		}

		if len(ft.ReturnTypes) == 0 {
			c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		} else {
			defaults := make([]hir.Value, len(ft.ReturnTypes))
			for i, rt := range ft.ReturnTypes {
				defaults[i] = c.root.defaultConstValue(rt)
			}
			c.root.terminate(&hir.InstrReturn{Vals: defaults})
		}
	}
	anonFn.StructuredBody = c.root.structuredRoot
	hir.FlattenTransparentBlocks(anonFn)

	c.root.hirProg.Functions = append(c.root.hirProg.Functions, anonFn)

	c.root.curFunc = prevFunc
	c.root.curBlock = prevBlock
	c.root.legacyBlocks = prevLegacyBlocks
	c.root.symbols = prevSymbols
	c.root.symbolTypes = prevTypes
	c.root.loopStack = prevLoopStack
	c.root.deferStack = prevDeferStack
	c.root.areaStack = prevAreaStack
	c.root.structuredRoot = prevStructuredRoot
	c.root.structuredStack = prevStructuredStack
	c.root.structuredFrames = prevStructuredFrames
	c.root.escapedVars = prevEscapedVars
	c.root.stringMutationCounts = prevStringMutationCounts
	c.root.stringMutationInLoop = prevStringMutationInLoop
	c.root.regCount = prevRegCount

	var envVal hir.Value = &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}
	if len(captures) > 0 {
		envSize := len(captures) * sema.PointerSize
		envRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		c.root.emit(&hir.InstrHeapAlloc{Dst: envRaw, Size: &hir.ConstInt{Val: int64(envSize), Typ: sema.TypeInt}, AllocType: sema.TypeByte, KeepOnHeap: true})

		envTyped := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		c.root.emit(&hir.InstrCast{Dst: envTyped, Val: envRaw, ToType: &sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}})

		for idx, name := range captures {
			symPtr := c.root.symbols[name]
			symRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			c.root.emit(&hir.InstrCast{Dst: symRaw, Val: symPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})
			slot := c.root.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
			c.root.emit(&hir.InstrGetElemPtr{Dst: slot, BasePtr: envTyped, Index: &hir.ConstInt{Val: int64(idx), Typ: sema.TypeInt}})
			c.root.emit(&hir.InstrStore{Val: symRaw, Ptr: slot})
		}
		envVal = envRaw
	}

	fatType := ft
	t1 := c.root.nextReg(fatType)
	fnGlobal := &hir.GlobalVar{Name: anonName, Typ: &sema.PointerType{Base: sema.TypeByte}}
	c.root.emit(&hir.InstrInsertValue{Dst: t1, Agg: c.root.defaultConstValue(fatType), Val: fnGlobal, Index: 0})
	t2 := c.root.nextReg(fatType)
	c.root.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: envVal, Index: 1})
	return t2
}

// countStringMutations records source-level writes to each local name. The
// type is checked at lowering time, after semantic resolution; keeping this
// pass type-agnostic also lets it cover inferred declarations. Writes nested
// in a loop are marked separately because even two iterations can justify the
// append-buffer optimization.
func countStringMutations(body *ast.BlockStmt) (map[string]int, map[string]bool) {
	counts := make(map[string]int)
	inLoop := make(map[string]bool)
	var walkBlock func(*ast.BlockStmt, bool)
	var walkStmt func(ast.Statement, bool)
	walkBlock = func(block *ast.BlockStmt, loop bool) {
		if block == nil {
			return
		}
		for _, stmt := range block.Statements {
			walkStmt(stmt, loop)
		}
	}
	walkStmt = func(stmt ast.Statement, loop bool) {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			for _, left := range s.Left {
				if ident, ok := left.(*ast.Identifier); ok && astIDValue(ident) != "_" {
					name := astIDValue(ident)
					counts[name]++
					if loop {
						inLoop[name] = true
					}
				}
			}
		case *ast.BlockStmt:
			walkBlock(s, loop)
		case *ast.IfStmt:
			walkStmt(s.Init, loop)
			walkBlock(s.Consequence, loop)
			walkStmt(s.Alternative, loop)
		case *ast.ForStmt:
			walkStmt(s.Init, loop)
			walkStmt(s.Post, true)
			walkBlock(s.Body, true)
		case *ast.ForRangeStmt:
			walkBlock(s.Body, true)
		case *ast.SwitchStmt:
			walkStmt(s.Init, loop)
			for _, clause := range s.Cases {
				for _, inner := range clause.Body {
					walkStmt(inner, loop)
				}
			}
		case *ast.TypeSwitchStmt:
			walkStmt(s.Init, loop)
			for _, clause := range s.Cases {
				for _, inner := range clause.Body {
					walkStmt(inner, loop)
				}
			}
		case *ast.AreaStmt:
			walkBlock(s.Body, loop)
		}
	}
	walkBlock(body, false)
	return counts, inLoop
}
