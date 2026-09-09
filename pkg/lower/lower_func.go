package lower

import (
	"fmt"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

// -----------------------------------------------------------------------------
// 関数定義 (Function Declaration) の変換
// -----------------------------------------------------------------------------

func (c *CallLowerer) LowerFunc(fn *ast.FuncDecl) {
	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	c.root.deferStack = []*ast.CallExpr{}
	c.root.regCount = 0
	if fn.Body != nil {
		c.root.escapedVars = sema.CollectAllCapturesInBlock(fn.Body)
	} else {
		c.root.escapedVars = make(map[string]bool)
	}

	fnName := fn.Name.Value
	var recvType sema.Type = nil
	if fn.Receiver != nil {
		recvType = c.root.semaCtx.ResolveType(fn.Receiver.Type)
		recvName := stringsTrimPrefix(recvType.TypeName(), "*")
		fnName = sema.CanonicalMethodName(recvName, fnName)
	}

	isMain := (fn.Name.Value == "main")
	returnTypes := []sema.Type{}
	if isMain {
		returnTypes = []sema.Type{sema.TypeInt}
	} else if fnType := c.root.semaCtx.Functions[fnName]; fnType != nil {
		returnTypes = fnType.ReturnTypes
	} else {
		for _, rt := range fn.ReturnTypes {
			returnTypes = append(returnTypes, c.root.semaCtx.ResolveType(rt))
		}
	}

	// C言語スタイルの可変長引数を持ち、本体を持つ関数は呼び出し側でインライン展開
	if fn.Body != nil && fn.IsVariadic {
		hasTypedVariadic := false
		for _, p := range fn.Params {
			if p.IsVariadic {
				hasTypedVariadic = true
				break
			}
		}
		if !hasTypedVariadic {
			return
		}
	}

	isIRVariadic := (fn.Body == nil && fn.IsVariadic)

	hirFn := &hir.Function{
		Name:        fnName,
		Params:      []*hir.Reg{},
		ReturnTypes: returnTypes,
		Blocks:      []*hir.BasicBlock{},
		IsVariadic:  isIRVariadic,
		IsExtern:    (fn.Body == nil),
	}
	c.root.curFunc = hirFn
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)

	if fn.Body == nil {
		switch fn.Name.Value {
		case "os_now_ns", "os_sleep_ms":
			return
		}

		hirFn.Blocks = nil
		for i, p := range fn.Params {
			pType := c.root.semaCtx.ResolveType(p.Type)
			if p.IsVariadic {
				if _, isSlice := pType.(*sema.SliceType); !isSlice {
					pType = &sema.SliceType{Elem: pType}
				}
			}
			hirFn.Params = append(hirFn.Params, &hir.Reg{ID: i + 1, Typ: pType, Name: p.Name.Value})
		}
		return
	}

	entryBB := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(entryBB)

	if isMain {
		argc32Reg := c.root.nextReg(&sema.BasicType{Name: "int32", ByteSize: 4, LLVM: "i32"}, "argc")
		argvReg := c.root.nextReg(&sema.PointerType{Base: sema.TypeString}, "argv")
		hirFn.Params = append(hirFn.Params, argc32Reg, argvReg)

		argcReg := c.root.nextReg(sema.TypeInt, "argc64")
		c.root.emit(&hir.InstrCast{Dst: argcReg, Val: argc32Reg, ToType: sema.TypeInt})

		if _, exists := c.root.semaCtx.Globals["os_Args"]; exists {
			callocRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
			c.root.emit(&hir.InstrCallStatic{Dst: callocRaw, CalleeName: "calloc", Args: []hir.Value{argcReg, &hir.ConstInt{Val: 8, Typ: sema.TypeInt}}})

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
			srcElemPtr := c.root.nextReg(&sema.PointerType{Base: sema.TypeString})
			c.root.emit(&hir.InstrGetElemPtr{Dst: srcElemPtr, BasePtr: argvReg, Index: curI})
			argStr := c.root.nextReg(sema.TypeString)
			c.root.emit(&hir.InstrLoad{Dst: argStr, Ptr: srcElemPtr})

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
	} else {
		if fn.Receiver != nil {
			paramReg := c.root.nextReg(recvType, fn.Receiver.Name.Value+"_arg")
			hirFn.Params = append(hirFn.Params, paramReg)

			ptrReg := c.root.nextReg(&sema.PointerType{Base: recvType}, fn.Receiver.Name.Value)
			if fn.Receiver.IsEscaped {
				sizeVal := &hir.ConstInt{Val: int64(recvType.Size()), Typ: sema.TypeInt}
				c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: recvType})
			} else {
				c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: recvType})
			}
			c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
			c.root.symbols[fn.Receiver.Name.Value] = ptrReg
			c.root.symbolTypes[fn.Receiver.Name.Value] = recvType
		}

		for _, p := range fn.Params {
			pType := c.root.semaCtx.ResolveType(p.Type)
			if p.IsVariadic {
				if _, isSlice := pType.(*sema.SliceType); !isSlice {
					pType = &sema.SliceType{Elem: pType}
				}
			}
			paramReg := c.root.nextReg(pType, p.Name.Value+"_arg")
			hirFn.Params = append(hirFn.Params, paramReg)

			ptrReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
			if p.IsEscaped || c.root.escapedVars[p.Name.Value] {
				sizeVal := &hir.ConstInt{Val: int64(pType.Size()), Typ: sema.TypeInt}
				c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: pType})
			} else {
				c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: pType})
			}
			c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
			c.root.symbols[p.Name.Value] = ptrReg
			c.root.symbolTypes[p.Name.Value] = pType
		}
	}

	for _, stmt := range fn.Body.Statements {
		c.root.Stmt.LowerStmt(stmt)
	}

	for i := len(c.root.deferStack) - 1; i >= 0; i-- {
		c.LowerCall(c.root.deferStack[i])
	}

	if c.root.curBlock.Terminator == nil {
		if isMain {
			c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{&hir.ConstInt{Val: 0, Typ: sema.TypeInt}}})
		} else if len(returnTypes) == 0 {
			c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		} else {
			defaults := make([]hir.Value, len(returnTypes))
			for i, rt := range returnTypes {
				defaults[i] = c.root.defaultConstValue(rt)
			}
			c.root.terminate(&hir.InstrReturn{Vals: defaults})
		}
	}
}

// -----------------------------------------------------------------------------
// 外部 C 関数宣言 (ExternFunc Declaration) の変換
// -----------------------------------------------------------------------------

func (c *CallLowerer) LowerExternFunc(efn *ast.ExternFuncDecl) {
	cName := efn.Name.Value
	if efn.TargetCName != nil {
		cName = efn.TargetCName.Value
	}

	returnTypes := []sema.Type{}
	if fnType := c.root.semaCtx.Functions[efn.Name.Value]; fnType != nil && len(fnType.ReturnTypes) > 0 {
		returnTypes = fnType.ReturnTypes
	} else {
		for _, rt := range efn.ReturnTypes {
			returnTypes = append(returnTypes, c.root.semaCtx.ResolveType(rt))
		}
	}

	params := []*hir.Reg{}
	for i, p := range efn.Params {
		pType := c.root.semaCtx.ResolveType(p.Type)
		params = append(params, &hir.Reg{ID: i + 1, Typ: pType, Name: p.Name.Value})
	}

	hirFn := &hir.Function{
		Name:        cName,
		Params:      params,
		ReturnTypes: returnTypes,
		Blocks:      nil,
		IsVariadic:  efn.IsVariadic,
		IsExtern:    true,
		IsCFunc:     true,
	}
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)
}

// -----------------------------------------------------------------------------
// C 連携関数 (CFunc Declaration) の変換 (Dual ABI: 実体 + トランポリン)
// -----------------------------------------------------------------------------

func (c *CallLowerer) LowerCFunc(cfn *ast.CFuncDecl) {
	targetCName := cfn.Name.Value
	if cfn.TargetCName != nil {
		targetCName = cfn.TargetCName.Value
	}

	returnTypes := []sema.Type{}
	if fnType := c.root.semaCtx.Functions[cfn.Name.Value]; fnType != nil && len(fnType.ReturnTypes) > 0 {
		returnTypes = fnType.ReturnTypes
	} else {
		for _, rt := range cfn.ReturnTypes {
			returnTypes = append(returnTypes, c.root.semaCtx.ResolveType(rt))
		}
	}

	// 1. 外部シンボル参照エイリアス (= c_func_name) の場合
	if cfn.IsAlias() || cfn.Body == nil {
		params := []*hir.Reg{}
		for i, p := range cfn.Params {
			pType := c.root.semaCtx.ResolveType(p.Type)
			params = append(params, &hir.Reg{ID: i + 1, Typ: pType, Name: p.Name.Value})
		}

		hirFn := &hir.Function{
			Name:          targetCName,
			Params:        params,
			ReturnTypes:   returnTypes,
			Blocks:        nil,
			IsVariadic:    cfn.IsVariadic,
			IsExtern:      true,
			IsCFunc:       true,
			IsPassThrough: cfn.IsPassThrough,
			CFuncTarget:   targetCName,
		}
		c.root.hirProg.Functions = append(c.root.hirProg.Functions, hirFn)
		return
	}

	// 2. 手書きブロックを持つ具象 cfunc の場合
	// ① Hike 内部用実体関数 (__hike_impl_<Name>) をコンパイル
	implName := "__hike_impl_" + cfn.Name.Value

	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	c.root.deferStack = []*ast.CallExpr{}
	c.root.regCount = 0
	c.root.escapedVars = sema.CollectAllCapturesInBlock(cfn.Body)

	implFn := &hir.Function{
		Name:          implName,
		Params:        []*hir.Reg{},
		ReturnTypes:   returnTypes,
		Blocks:        []*hir.BasicBlock{},
		IsVariadic:    cfn.IsVariadic,
		IsExtern:      false,
		IsCFunc:       false,
		IsPassThrough: cfn.IsPassThrough,
	}
	c.root.curFunc = implFn
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, implFn)

	entryBB := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(entryBB)

	trampolineParamTypes := []sema.Type{}
	for _, p := range cfn.Params {
		pType := c.root.semaCtx.ResolveType(p.Type)
		trampolineParamTypes = append(trampolineParamTypes, pType)

		paramReg := c.root.nextReg(pType, p.Name.Value+"_arg")
		implFn.Params = append(implFn.Params, paramReg)

		ptrReg := c.root.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
		if p.IsEscaped || c.root.escapedVars[p.Name.Value] {
			sizeVal := &hir.ConstInt{Val: int64(pType.Size()), Typ: sema.TypeInt}
			c.root.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: pType})
		} else {
			c.root.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: pType})
		}
		c.root.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
		c.root.symbols[p.Name.Value] = ptrReg
		c.root.symbolTypes[p.Name.Value] = pType
	}

	for _, stmt := range cfn.Body.Statements {
		c.root.Stmt.LowerStmt(stmt)
	}

	for i := len(c.root.deferStack) - 1; i >= 0; i-- {
		c.LowerCall(c.root.deferStack[i])
	}

	if c.root.curBlock.Terminator == nil {
		if len(returnTypes) == 0 {
			c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		} else {
			defaults := make([]hir.Value, len(returnTypes))
			for i, rt := range returnTypes {
				defaults[i] = c.root.defaultConstValue(rt)
			}
			c.root.terminate(&hir.InstrReturn{Vals: defaults})
		}
	}

	// ② 外部公開用トランポリン関数 (<TargetCName>) を生成
	c.root.regCount = 0
	trampolineFn := &hir.Function{
		Name:          targetCName,
		Params:        []*hir.Reg{},
		ReturnTypes:   returnTypes,
		Blocks:        []*hir.BasicBlock{},
		IsVariadic:    cfn.IsVariadic,
		IsExtern:      false,
		IsCFunc:       true,
		IsPassThrough: cfn.IsPassThrough,
		CFuncTarget:   targetCName,
	}
	c.root.curFunc = trampolineFn
	c.root.hirProg.Functions = append(c.root.hirProg.Functions, trampolineFn)

	tEntryBB := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	c.root.setBlock(tEntryBB)

	callArgs := []hir.Value{}
	for i, pType := range trampolineParamTypes {
		pReg := c.root.nextReg(pType, fmt.Sprintf("arg_%d", i))
		trampolineFn.Params = append(trampolineFn.Params, pReg)
		callArgs = append(callArgs, pReg)
	}

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

	if callDst != nil {
		c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{callDst}})
	} else {
		c.root.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
	}
}

// -------------------------------------------------------------
// クロージャ (FuncLit) の Lowering
// -------------------------------------------------------------

func (c *CallLowerer) LowerFuncLit(fl *ast.FuncLit) hir.Value {
	c.root.anonFuncCount++
	anonName := fmt.Sprintf("__anon_func_%d", c.root.anonFuncCount)

	ft := c.root.semaCtx.InferExprType(fl, nil).(*sema.FuncType)
	anonFn := &hir.Function{
		Name:        anonName,
		Params:      []*hir.Reg{},
		ReturnTypes: ft.ReturnTypes,
		Blocks:      []*hir.BasicBlock{},
		IsVariadic:  fl.IsVariadic,
		IsExtern:    false,
	}

	envParamReg := &hir.Reg{ID: 1, Typ: &sema.PointerType{Base: sema.TypeByte}, Name: "__env_arg"}
	anonFn.Params = append(anonFn.Params, envParamReg)

	rawCaptures := sema.ScanCapturesFromLit(fl)
	captures := []string{}
	for _, name := range rawCaptures {
		if _, ok := c.root.symbols[name]; ok {
			captures = append(captures, name)
		}
	}

	prevFunc := c.root.curFunc
	prevBlock := c.root.curBlock
	prevSymbols := c.root.symbols
	prevTypes := c.root.symbolTypes
	prevLoopStack := c.root.loopStack
	prevDeferStack := c.root.deferStack
	prevEscapedVars := c.root.escapedVars

	c.root.curFunc = anonFn
	c.root.symbols = make(map[string]hir.Value)
	c.root.symbolTypes = make(map[string]sema.Type)
	c.root.loopStack = []loopContext{}
	c.root.deferStack = []*ast.CallExpr{}
	if fl.Body != nil {
		c.root.escapedVars = sema.CollectAllCapturesInBlock(fl.Body)
	} else {
		c.root.escapedVars = make(map[string]bool)
	}

	anonEntry := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
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
			sizeVal := &hir.ConstInt{Val: int64(pType.Size()), Typ: sema.TypeInt}
			c.root.emit(&hir.InstrHeapAlloc{Dst: allocaReg, Size: sizeVal, AllocType: pType})
		} else {
			c.root.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: pType})
		}
		c.root.emit(&hir.InstrStore{Val: pReg, Ptr: allocaReg})
		c.root.symbols[p.Name.Value] = allocaReg
		c.root.symbolTypes[p.Name.Value] = pType
	}

	for _, s := range fl.Body.Statements {
		c.root.Stmt.LowerStmt(s)
	}

	for i := len(c.root.deferStack) - 1; i >= 0; i-- {
		c.LowerCall(c.root.deferStack[i])
	}

	if c.root.curBlock.Terminator == nil {
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

	c.root.hirProg.Functions = append(c.root.hirProg.Functions, anonFn)

	c.root.curFunc = prevFunc
	c.root.curBlock = prevBlock
	c.root.symbols = prevSymbols
	c.root.symbolTypes = prevTypes
	c.root.loopStack = prevLoopStack
	c.root.deferStack = prevDeferStack
	c.root.escapedVars = prevEscapedVars

	var envVal hir.Value = &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}
	if len(captures) > 0 {
		envSize := len(captures) * 8
		envRaw := c.root.nextReg(&sema.PointerType{Base: sema.TypeByte})
		c.root.emit(&hir.InstrHeapAlloc{Dst: envRaw, Size: &hir.ConstInt{Val: int64(envSize), Typ: sema.TypeInt}, AllocType: sema.TypeByte})

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

func stringsTrimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}
