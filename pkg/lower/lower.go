package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

type loopContext struct {
	breakBlock    *hir.BasicBlock
	continueBlock *hir.BasicBlock
}

type Lowerer struct {
	prog          *ast.Program
	semaCtx       *sema.Context
	hirProg       *hir.Program
	curFunc       *hir.Function
	curBlock      *hir.BasicBlock
	regCount      int
	blockCount    int
	anonFuncCount int
	stringPool    map[string]*hir.ConstString
	symbols       map[string]hir.Value
	symbolTypes   map[string]sema.Type
	loopStack     []loopContext
	deferStack    []*ast.CallExpr
	itabs         map[string]*hir.ItabDef
	escapedVars   map[string]bool // 追加
}

func New(prog *ast.Program, semaCtx *sema.Context) *Lowerer {
	return &Lowerer{
		prog:        prog,
		semaCtx:     semaCtx,
		hirProg:     &hir.Program{ModuleName: prog.Package},
		stringPool:  make(map[string]*hir.ConstString),
		symbols:     make(map[string]hir.Value),
		symbolTypes: make(map[string]sema.Type),
		loopStack:   []loopContext{},
		deferStack:  []*ast.CallExpr{},
		itabs:       make(map[string]*hir.ItabDef),
		escapedVars: make(map[string]bool), // 追加

	}
}

// nextReg は関数全体で必ず一意のレジスタ名を生成します
func (l *Lowerer) nextReg(typ sema.Type, name ...string) *hir.Reg {
	l.regCount++
	regName := ""
	if len(name) > 0 && name[0] != "" {
		regName = fmt.Sprintf("%s.%d", name[0], l.regCount)
	} else {
		regName = fmt.Sprintf("v%d", l.regCount)
	}
	return &hir.Reg{ID: l.regCount, Typ: typ, Name: regName}
}

func (l *Lowerer) newBlock(labelPrefix string) *hir.BasicBlock {
	l.blockCount++
	return &hir.BasicBlock{
		Label:        fmt.Sprintf("%s.%d", labelPrefix, l.blockCount),
		Instructions: []hir.Instruction{},
	}
}

func (l *Lowerer) setBlock(bb *hir.BasicBlock) {
	l.curBlock = bb
	if l.curFunc != nil {
		l.curFunc.Blocks = append(l.curFunc.Blocks, bb)
	}
}

func (l *Lowerer) emit(instr hir.Instruction) {
	if l.curBlock != nil {
		l.curBlock.Instructions = append(l.curBlock.Instructions, instr)
	}
}

func (l *Lowerer) terminate(term hir.Terminator) {
	if l.curBlock != nil && l.curBlock.Terminator == nil {
		l.curBlock.Terminator = term
	}
}

func (l *Lowerer) getStringConst(raw string) *hir.ConstString {
	if sc, ok := l.stringPool[raw]; ok {
		return sc
	}
	label := fmt.Sprintf("str.%d", len(l.stringPool)+1)
	sc := &hir.ConstString{
		Label:  label,
		Raw:    raw,
		Length: len(raw) + 1,
		Typ:    sema.TypeString,
	}
	l.stringPool[raw] = sc
	l.hirProg.StringConstants = append(l.hirProg.StringConstants, sc)
	return sc
}

func (l *Lowerer) defaultConstValue(t sema.Type) hir.Value {
	if t == sema.TypeBool {
		return &hir.ConstBool{Val: false, Typ: t}
	}
	if t == sema.TypeFloat64 || t == sema.TypeFloat32 {
		return &hir.ConstFloat{Val: 0.0, Typ: t}
	}
	if strings.HasSuffix(t.LLVMType(), "*") {
		return &hir.ConstNil{Typ: t}
	}
	if _, isSlice := t.(*sema.SliceType); isSlice {
		return &hir.ConstZero{Typ: t}
	}
	if _, isStruct := t.(*sema.StructType); isStruct {
		return &hir.ConstZero{Typ: t}
	}
	if _, isTuple := t.(*sema.TupleType); isTuple {
		return &hir.ConstZero{Typ: t}
	}
	if _, isArray := t.(*sema.ArrayType); isArray {
		return &hir.ConstZero{Typ: t}
	}
	if _, isIface := t.(*sema.InterfaceType); isIface {
		return &hir.ConstZero{Typ: t}
	}
	// 追記: 関数型 (クロージャ構造体 { i8*, i8* }) の初期値を zeroinitializer に設定
	if _, isFunc := t.(*sema.FuncType); isFunc {
		return &hir.ConstZero{Typ: t}
	}
	// 複合型全般に対するフォールバック
	if strings.HasPrefix(t.LLVMType(), "{") || strings.HasPrefix(t.LLVMType(), "[") || strings.HasPrefix(t.LLVMType(), "%struct.") {
		return &hir.ConstZero{Typ: t}
	}
	return &hir.ConstInt{Val: 0, Typ: t}
}

func (l *Lowerer) resolveTypeFromExpr(e ast.Expression) sema.Type {
	if e == nil {
		return nil
	}
	if te, ok := e.(ast.TypeExpr); ok {
		return l.semaCtx.ResolveType(te)
	}
	switch node := e.(type) {
	case *ast.PointerType:
		return l.semaCtx.ResolveType(node)
	case *ast.SliceType:
		return l.semaCtx.ResolveType(node)
	case *ast.ArrayType:
		return l.semaCtx.ResolveType(node)
	case *ast.MapType:
		return l.semaCtx.ResolveType(node)
	case *ast.NamedType:
		return l.semaCtx.ResolveType(node)
	case *ast.FuncType:
		return l.semaCtx.ResolveType(node)
	case *ast.Identifier:
		switch node.Value {
		case "int":
			return sema.TypeInt
		case "byte":
			return sema.TypeByte
		case "bool":
			return sema.TypeBool
		case "float32":
			return sema.TypeFloat32
		case "float64", "float":
			return sema.TypeFloat64
		case "string":
			return sema.TypeString
		case "void":
			return sema.TypeVoid
		case "any":
			return &sema.InterfaceType{Name: "any", Specializations: make(map[string]*sema.InterfaceType)}
		case "error":
			if iface, ok := l.semaCtx.Interfaces["error"]; ok {
				return iface
			}
			return nil
		}
		if st, _ := l.semaCtx.LookupStruct(node.Value); st != nil {
			if st.IsGeneric() {
				return nil
			}
			return st
		}
		if iface, _ := l.semaCtx.LookupInterface(node.Value); iface != nil {
			if iface.IsGeneric() {
				return nil
			}
			return iface
		}
		if alias, ok := l.semaCtx.Aliases[node.Value]; ok {
			return alias
		}
		return nil

	case *ast.PrefixExpr:
		if node.Operator == "*" {
			baseT := l.resolveTypeFromExpr(node.Right)
			if baseT != nil && baseT != sema.TypeVoid {
				return &sema.PointerType{Base: baseT}
			}
		}
		return nil

	case *ast.MemberExpr:
		if pkgId, okPkg := node.Object.(*ast.Identifier); okPkg {
			typeName := pkgId.Value + "_" + node.Field.Value
			if st, _ := l.semaCtx.LookupStruct(typeName); st != nil {
				return st
			}
			if iface, _ := l.semaCtx.LookupInterface(typeName); iface != nil {
				return iface
			}
			if alias, ok := l.semaCtx.Aliases[typeName]; ok {
				return alias
			}
		}
		return nil
	}
	return nil
}

func (l *Lowerer) Lower() *hir.Program {
	for name, typ := range l.semaCtx.Globals {
		l.hirProg.Globals = append(l.hirProg.Globals, &hir.GlobalVar{Name: name, Typ: typ})
	}

	for _, fn := range l.semaCtx.Functions {
		if fn.IsExtern {
			params := []*hir.Reg{}
			for i, pt := range fn.ParamTypes {
				params = append(params, &hir.Reg{ID: i + 1, Typ: pt})
			}
			l.hirProg.Functions = append(l.hirProg.Functions, &hir.Function{
				Name:        fn.Name,
				Params:      params,
				ReturnTypes: fn.ReturnTypes,
				Blocks:      nil,
				IsVariadic:  fn.IsVariadic,
				IsExtern:    true,
			})
		}
	}

	for _, decl := range l.prog.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Body != nil {
			if sema.IsGenericFuncDecl(fnDecl) {
				continue
			}
			l.lowerFunc(fnDecl)
		}
	}

	return l.hirProg
}

// -------------------------------------------------------------
// 関数 (Function) の変換と main エントリ初期化
// -------------------------------------------------------------

func (l *Lowerer) lowerFunc(fn *ast.FuncDecl) {
	l.symbols = make(map[string]hir.Value)
	l.symbolTypes = make(map[string]sema.Type)
	l.deferStack = []*ast.CallExpr{}
	l.regCount = 0
	if fn.Body != nil {
		l.escapedVars = sema.CollectAllCapturesInBlock(fn.Body)
	} else {
		l.escapedVars = make(map[string]bool)
	}

	fnName := fn.Name.Value
	var recvType sema.Type = nil
	if fn.Receiver != nil {
		recvType = l.semaCtx.ResolveType(fn.Receiver.Type)
		recvName := strings.TrimPrefix(recvType.TypeName(), "*")
		if !strings.Contains(fnName, recvName) {
			fnName = recvName + "_" + fnName
		}
	}

	isMain := (fn.Name.Value == "main")
	returnTypes := []sema.Type{}
	if isMain {
		returnTypes = []sema.Type{sema.TypeInt}
	} else if fnType := l.semaCtx.Functions[fnName]; fnType != nil {
		returnTypes = fnType.ReturnTypes
	} else {
		for _, rt := range fn.ReturnTypes {
			returnTypes = append(returnTypes, l.semaCtx.ResolveType(rt))
		}
	}

	hirFn := &hir.Function{
		Name:        fnName,
		Params:      []*hir.Reg{},
		ReturnTypes: returnTypes,
		Blocks:      []*hir.BasicBlock{},
		IsVariadic:  fn.IsVariadic,
		IsExtern:    false,
	}
	l.curFunc = hirFn
	l.hirProg.Functions = append(l.hirProg.Functions, hirFn)

	entryBB := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	l.setBlock(entryBB)

	if isMain {
		// C ランタイム互換: int main(int argc, char** argv)
		argc32Reg := l.nextReg(&sema.BasicType{Name: "int32", ByteSize: 4, LLVM: "i32"}, "argc")
		argvReg := l.nextReg(&sema.PointerType{Base: sema.TypeString}, "argv")
		hirFn.Params = append(hirFn.Params, argc32Reg, argvReg)

		argcReg := l.nextReg(sema.TypeInt, "argc64")
		l.emit(&hir.InstrCast{Dst: argcReg, Val: argc32Reg, ToType: sema.TypeInt})

		if _, exists := l.semaCtx.Globals["os_Args"]; exists {
			callocRaw := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
			l.emit(&hir.InstrCallStatic{Dst: callocRaw, CalleeName: "calloc", Args: []hir.Value{argcReg, &hir.ConstInt{Val: 8, Typ: sema.TypeInt}}})

			callocRes := l.nextReg(&sema.PointerType{Base: sema.TypeString})
			l.emit(&hir.InstrCast{Dst: callocRes, Val: callocRaw, ToType: &sema.PointerType{Base: sema.TypeString}})

			loopCondBB := l.newBlock("argv.loop.cond")
			loopBodyBB := l.newBlock("argv.loop.body")
			loopEndBB := l.newBlock("argv.loop.end")

			idxAlloca := l.nextReg(&sema.PointerType{Base: sema.TypeInt}, "i.arg")
			l.emit(&hir.InstrAlloca{Dst: idxAlloca, AllocType: sema.TypeInt})
			l.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: idxAlloca})
			l.terminate(&hir.InstrJump{Target: loopCondBB.Label})

			l.setBlock(loopCondBB)
			curI := l.nextReg(sema.TypeInt)
			l.emit(&hir.InstrLoad{Dst: curI, Ptr: idxAlloca})
			cmp := l.nextReg(sema.TypeBool)
			l.emit(&hir.InstrBinary{Dst: cmp, Op: hir.OpLt, L: curI, R: argcReg})
			l.terminate(&hir.InstrBranch{Cond: cmp, ThenTarget: loopBodyBB.Label, ElseTarget: loopEndBB.Label})

			l.setBlock(loopBodyBB)
			srcElemPtr := l.nextReg(&sema.PointerType{Base: sema.TypeString})
			l.emit(&hir.InstrGetElemPtr{Dst: srcElemPtr, BasePtr: argvReg, Index: curI})
			argStr := l.nextReg(sema.TypeString)
			l.emit(&hir.InstrLoad{Dst: argStr, Ptr: srcElemPtr})

			dstElemPtr := l.nextReg(&sema.PointerType{Base: sema.TypeString})
			l.emit(&hir.InstrGetElemPtr{Dst: dstElemPtr, BasePtr: callocRes, Index: curI})
			l.emit(&hir.InstrStore{Val: argStr, Ptr: dstElemPtr})

			nextI := l.nextReg(sema.TypeInt)
			l.emit(&hir.InstrBinary{Dst: nextI, Op: hir.OpAdd, L: curI, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
			l.emit(&hir.InstrStore{Val: nextI, Ptr: idxAlloca})
			l.terminate(&hir.InstrJump{Target: loopCondBB.Label})

			l.setBlock(loopEndBB)
			sliceType := &sema.SliceType{Elem: sema.TypeString}
			t1 := l.nextReg(sliceType)
			l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(sliceType), Val: callocRaw, Index: 0})
			t2 := l.nextReg(sliceType)
			l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: argcReg, Index: 1})
			t3 := l.nextReg(sliceType)
			l.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: argcReg, Index: 2})
			l.emit(&hir.InstrStore{Val: t3, Ptr: &hir.GlobalVar{Name: "os_Args", Typ: &sema.PointerType{Base: sliceType}}})
		}
	} else {
		if fn.Receiver != nil {
			paramReg := l.nextReg(recvType, fn.Receiver.Name.Value+"_arg")
			hirFn.Params = append(hirFn.Params, paramReg)

			ptrReg := l.nextReg(&sema.PointerType{Base: recvType}, fn.Receiver.Name.Value)
			if fn.Receiver.IsEscaped {
				sizeVal := &hir.ConstInt{Val: int64(recvType.Size()), Typ: sema.TypeInt}
				l.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: recvType})
			} else {
				l.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: recvType})
			}
			l.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
			l.symbols[fn.Receiver.Name.Value] = ptrReg
			l.symbolTypes[fn.Receiver.Name.Value] = recvType
		}

		for _, p := range fn.Params {
			pType := l.semaCtx.ResolveType(p.Type)
			paramReg := l.nextReg(pType, p.Name.Value+"_arg")
			hirFn.Params = append(hirFn.Params, paramReg)

			ptrReg := l.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
			if p.IsEscaped || l.escapedVars[p.Name.Value] {
				sizeVal := &hir.ConstInt{Val: int64(pType.Size()), Typ: sema.TypeInt}
				l.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: pType})
			} else {
				l.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: pType})
			}
			l.emit(&hir.InstrStore{Val: paramReg, Ptr: ptrReg})
			l.symbols[p.Name.Value] = ptrReg
			l.symbolTypes[p.Name.Value] = pType
		}
	}

	for _, stmt := range fn.Body.Statements {
		l.lowerStmt(stmt)
	}

	for i := len(l.deferStack) - 1; i >= 0; i-- {
		l.lowerCall(l.deferStack[i])
	}

	if l.curBlock.Terminator == nil {
		if isMain {
			l.terminate(&hir.InstrReturn{Vals: []hir.Value{&hir.ConstInt{Val: 0, Typ: sema.TypeInt}}})
		} else if len(returnTypes) == 0 {
			l.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
		} else {
			defaults := make([]hir.Value, len(returnTypes))
			for i, rt := range returnTypes {
				defaults[i] = l.defaultConstValue(rt)
			}
			l.terminate(&hir.InstrReturn{Vals: defaults})
		}
	}
}

// -------------------------------------------------------------
// 文 (Statement) の変換
// -------------------------------------------------------------

func (l *Lowerer) lowerStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.VarDecl:
		l.lowerVarDecl(s)
	case *ast.AssignStmt:
		l.lowerAssignStmt(s)
	case *ast.ExprStmt:
		l.lowerExpr(s.Expr)
	case *ast.BlockStmt:
		for _, inner := range s.Statements {
			l.lowerStmt(inner)
		}
	case *ast.IfStmt:
		l.lowerIfStmt(s)
	case *ast.ForStmt:
		l.lowerForStmt(s)
	case *ast.ForRangeStmt:
		l.lowerForRangeStmt(s)
	case *ast.SwitchStmt:
		l.lowerSwitchStmt(s)
	case *ast.TypeSwitchStmt:
		l.lowerTypeSwitchStmt(s)
	case *ast.ReturnStmt:
		l.lowerReturnStmt(s)
	case *ast.DeferStmt:
		if s.Call != nil {
			l.deferStack = append(l.deferStack, s.Call)
		}
	case *ast.BreakStmt:
		if len(l.loopStack) > 0 {
			ctx := l.loopStack[len(l.loopStack)-1]
			l.terminate(&hir.InstrJump{Target: ctx.breakBlock.Label})
		}
	case *ast.ContinueStmt:
		if len(l.loopStack) > 0 {
			ctx := l.loopStack[len(l.loopStack)-1]
			l.terminate(&hir.InstrJump{Target: ctx.continueBlock.Label})
		}
	}
}

func (l *Lowerer) lowerVarDecl(vd *ast.VarDecl) {
	var targetType sema.Type = sema.TypeInt
	if vd.Type != nil {
		targetType = l.semaCtx.ResolveType(vd.Type)
	}

	ptrReg := l.nextReg(&sema.PointerType{Base: targetType}, vd.Name.Value)
	if vd.IsEscaped {
		sizeVal := &hir.ConstInt{Val: int64(targetType.Size()), Typ: sema.TypeInt}
		l.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: targetType})
	} else {
		l.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: targetType})
	}

	l.symbols[vd.Name.Value] = ptrReg
	l.symbolTypes[vd.Name.Value] = targetType

	if vd.Value != nil {
		val := l.lowerExpr(vd.Value)
		l.emit(&hir.InstrStore{Val: val, Ptr: ptrReg})
	} else {
		defVal := l.defaultConstValue(targetType)
		l.emit(&hir.InstrStore{Val: defVal, Ptr: ptrReg})
	}
}

func (l *Lowerer) emitValueCoerce(val hir.Value, targetType sema.Type) hir.Value {
	if val.Type().LLVMType() == targetType.LLVMType() {
		return val
	}
	if iface, ok := targetType.(*sema.InterfaceType); ok {
		itabName := ""
		if !iface.IsAny() {
			itabDef := l.getOrCreateItab(val.Type(), iface)
			itabName = itabDef.GlobalName
		}
		dst := l.nextReg(iface)
		l.emit(&hir.InstrBoxInterface{Dst: dst, Val: val, Iface: iface, ItabName: itabName})
		return dst
	}
	dst := l.nextReg(targetType)
	l.emit(&hir.InstrCast{Dst: dst, Val: val, ToType: targetType})
	return dst
}

func (l *Lowerer) lowerAssignStmt(as *ast.AssignStmt) {
	isDefine := (as.Token.Type == token.DEFINE) || (as.Token.Literal == ":=") ||
		(as.Token.Type == token.VAR) || (as.Token.Literal == "var") || (as.Type != nil)

	// 1. 多値代入 (Tuple unpacking)
	if len(as.Left) > 1 && len(as.Right) == 1 {
		tupleVal := l.lowerExpr(as.Right[0])
		if tt, isTuple := tupleVal.Type().(*sema.TupleType); isTuple {
			for i, left := range as.Left {
				if i >= len(tt.Types) {
					break
				}
				elemType := tt.Types[i]
				elemReg := l.nextReg(elemType)
				l.emit(&hir.InstrExtractValue{Dst: elemReg, Agg: tupleVal, Index: i})

				if id, ok := left.(*ast.Identifier); ok {
					if isDefine {
						ptrReg := l.nextReg(&sema.PointerType{Base: elemType}, id.Value)
						if l.escapedVars[id.Value] {
							sizeVal := &hir.ConstInt{Val: int64(elemType.Size()), Typ: sema.TypeInt}
							l.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: elemType})
						} else {
							l.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: elemType})
						}
						l.symbols[id.Value] = ptrReg
						l.symbolTypes[id.Value] = elemType
						l.emit(&hir.InstrStore{Val: elemReg, Ptr: ptrReg})
					} else {
						ptr := l.lowerLValue(id)
						l.emit(&hir.InstrStore{Val: elemReg, Ptr: ptr})
					}
				}
			}
			return
		}
	}

	// 2. マップインデックス代入: m[k] = v
	// ... (中略: 既存のまま) ...

	// 3. 通常の代入 / 定義
	// 右辺のすべての式をあらかじめ評価し、多重代入 (例: a, b = b, a) の値上書き競合を防止する
	rhsVals := make([]hir.Value, len(as.Right))
	for i, r := range as.Right {
		rhsVals[i] = l.lowerExpr(r)
	}

	for i, left := range as.Left {
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
				actualType = l.semaCtx.ResolveType(as.Type)
			}

			isUninitVar := false
			if il, okIl := rhs.(*ast.IntegerLiteral); okIl && (as.Token.Type == token.VAR || as.Token.Literal == "var") && il.Token.Type == token.VAR {
				isUninitVar = true
			}

			if isUninitVar && actualType != nil {
				val = l.defaultConstValue(actualType)
			} else if val != nil {
				if actualType == nil {
					actualType = val.Type()
				} else {
					val = l.emitValueCoerce(val, actualType)
				}
			} else if actualType != nil {
				val = l.defaultConstValue(actualType)
			} else {
				actualType = sema.TypeInt
				val = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
			}

			ptrReg := l.nextReg(&sema.PointerType{Base: actualType}, id.Value)
			if l.escapedVars[id.Value] {
				sizeVal := &hir.ConstInt{Val: int64(actualType.Size()), Typ: sema.TypeInt}
				l.emit(&hir.InstrHeapAlloc{Dst: ptrReg, Size: sizeVal, AllocType: actualType})
			} else {
				l.emit(&hir.InstrAlloca{Dst: ptrReg, AllocType: actualType})
			}
			l.symbols[id.Value] = ptrReg
			l.symbolTypes[id.Value] = actualType
			l.emit(&hir.InstrStore{Val: val, Ptr: ptrReg})
			continue
		}

		ptr := l.lowerLValue(left)

		// ++ / -- の場合は val を明示的に 1 とする
		switch as.Token.Literal {
		case "++":
			targetType := ptr.Type().(*sema.PointerType).Base
			curValReg := l.nextReg(targetType)
			l.emit(&hir.InstrLoad{Dst: curValReg, Ptr: ptr})
			resReg := l.nextReg(targetType)
			l.emit(&hir.InstrBinary{Dst: resReg, Op: hir.OpAdd, L: curValReg, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
			val = resReg
		case "--":
			targetType := ptr.Type().(*sema.PointerType).Base
			curValReg := l.nextReg(targetType)
			l.emit(&hir.InstrLoad{Dst: curValReg, Ptr: ptr})
			resReg := l.nextReg(targetType)
			l.emit(&hir.InstrBinary{Dst: resReg, Op: hir.OpSub, L: curValReg, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
			val = resReg
		case "+=", "-=", "*=", "/=", "%=":
			curValReg := l.nextReg(val.Type())
			l.emit(&hir.InstrLoad{Dst: curValReg, Ptr: ptr})
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
			resReg := l.nextReg(val.Type())
			l.emit(&hir.InstrBinary{Dst: resReg, Op: op, L: curValReg, R: val})
			val = resReg
		}

		targetType := ptr.Type().(*sema.PointerType).Base
		val = l.emitValueCoerce(val, targetType)
		l.emit(&hir.InstrStore{Val: val, Ptr: ptr})
	}
}

func (l *Lowerer) coerceToI64(v hir.Value, fromType sema.Type) hir.Value {
	if fromType == sema.TypeInt {
		return v
	}
	dst := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrCast{Dst: dst, Val: v, ToType: sema.TypeInt})
	return dst
}

func (l *Lowerer) coerceFromI64(v hir.Value, toType sema.Type) hir.Value {
	if toType == sema.TypeInt {
		return v
	}
	dst := l.nextReg(toType)
	l.emit(&hir.InstrCast{Dst: dst, Val: v, ToType: toType})
	return dst
}

func (l *Lowerer) lowerIfStmt(is *ast.IfStmt) {
	if is.Init != nil {
		l.lowerStmt(is.Init)
	}

	condVal := l.lowerExpr(is.Condition)
	thenBB := l.newBlock("if.then")
	elseBB := l.newBlock("if.else")
	endBB := l.newBlock("if.end")

	if is.Alternative != nil {
		l.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: thenBB.Label, ElseTarget: elseBB.Label})
	} else {
		l.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: thenBB.Label, ElseTarget: endBB.Label})
	}

	l.setBlock(thenBB)
	l.lowerStmt(is.Consequence)
	if l.curBlock.Terminator == nil {
		l.terminate(&hir.InstrJump{Target: endBB.Label})
	}

	if is.Alternative != nil {
		l.setBlock(elseBB)
		l.lowerStmt(is.Alternative)
		if l.curBlock.Terminator == nil {
			l.terminate(&hir.InstrJump{Target: endBB.Label})
		}
	}

	l.setBlock(endBB)
}

func (l *Lowerer) lowerForStmt(fs *ast.ForStmt) {
	if fs.Init != nil {
		l.lowerStmt(fs.Init)
	}

	condBB := l.newBlock("for.cond")
	bodyBB := l.newBlock("for.body")
	postBB := l.newBlock("for.post")
	endBB := l.newBlock("for.end")

	l.loopStack = append(l.loopStack, loopContext{breakBlock: endBB, continueBlock: postBB})
	defer func() {
		l.loopStack = l.loopStack[:len(l.loopStack)-1]
	}()

	l.terminate(&hir.InstrJump{Target: condBB.Label})

	l.setBlock(condBB)
	if fs.Cond != nil {
		condVal := l.lowerExpr(fs.Cond)
		l.terminate(&hir.InstrBranch{Cond: condVal, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})
	} else {
		l.terminate(&hir.InstrJump{Target: bodyBB.Label})
	}

	l.setBlock(bodyBB)
	l.lowerStmt(fs.Body)
	if l.curBlock.Terminator == nil {
		l.terminate(&hir.InstrJump{Target: postBB.Label})
	}

	l.setBlock(postBB)
	if fs.Post != nil {
		l.lowerStmt(fs.Post)
	}
	l.terminate(&hir.InstrJump{Target: condBB.Label})

	l.setBlock(endBB)
}

func (l *Lowerer) lowerForRangeStmt(fr *ast.ForRangeStmt) {
	xVal := l.lowerExpr(fr.X)
	xType := xVal.Type()

	if _, _, isBeh := l.semaCtx.CheckMapBehavior(xType); isBeh {
		objPtr := l.lowerStructPtr(fr.X)
		st, sName := l.findStruct(xType)
		initFnName, initFn, finalRecv, hasInit := l.resolveMethodPath(st, sName, objPtr, "InitIterator")
		nextFnName, nextFn, _, hasNext := l.resolveMethodPath(st, sName, objPtr, "Next")

		if hasInit && hasNext && initFn != nil && nextFn != nil {
			sizeReg := l.nextReg(sema.TypeInt)
			l.emit(&hir.InstrCallStatic{Dst: sizeReg, CalleeName: initFnName, Args: []hir.Value{finalRecv, &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}}})

			bufReg := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
			l.emit(&hir.InstrAllocaDynamic{Dst: bufReg, Size: sizeReg, AllocType: sema.TypeByte})
			l.emit(&hir.InstrCallStatic{CalleeName: initFnName, Args: []hir.Value{finalRecv, bufReg}})

			condBB := l.newBlock("mapbeh.cond")
			bodyBB := l.newBlock("mapbeh.body")
			endBB := l.newBlock("mapbeh.end")

			l.loopStack = append(l.loopStack, loopContext{breakBlock: endBB, continueBlock: condBB})
			defer func() {
				l.loopStack = l.loopStack[:len(l.loopStack)-1]
			}()

			l.terminate(&hir.InstrJump{Target: condBB.Label})

			l.setBlock(condBB)
			retTupleType := nextFn.ReturnTypes[0]
			nextRes := l.nextReg(retTupleType)
			l.emit(&hir.InstrCallStatic{Dst: nextRes, CalleeName: nextFnName, Args: []hir.Value{finalRecv, bufReg}})
			okReg := l.nextReg(sema.TypeBool)
			l.emit(&hir.InstrExtractValue{Dst: okReg, Agg: nextRes, Index: 2})
			l.terminate(&hir.InstrBranch{Cond: okReg, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})

			l.setBlock(bodyBB)
			if fr.Key != nil {
				if kId, ok := fr.Key.(*ast.Identifier); ok && kId.Value != "_" {
					kPtrReg := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
					l.emit(&hir.InstrExtractValue{Dst: kPtrReg, Agg: nextRes, Index: 0})
					kValReg := l.nextReg(sema.TypeInt)
					l.emit(&hir.InstrLoad{Dst: kValReg, Ptr: kPtrReg})
					kPtr := l.symbols[kId.Value]
					if kPtr == nil {
						kPtr = l.nextReg(&sema.PointerType{Base: sema.TypeInt}, kId.Value)
						l.emit(&hir.InstrAlloca{Dst: kPtr.(*hir.Reg), AllocType: sema.TypeInt})
						l.symbols[kId.Value] = kPtr
						l.symbolTypes[kId.Value] = sema.TypeInt
					}
					l.emit(&hir.InstrStore{Val: kValReg, Ptr: kPtr})
				}
			}
			if fr.Value != nil {
				if vId, ok := fr.Value.(*ast.Identifier); ok && vId.Value != "_" {
					vPtrReg := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
					l.emit(&hir.InstrExtractValue{Dst: vPtrReg, Agg: nextRes, Index: 1})
					vValReg := l.nextReg(sema.TypeInt)
					l.emit(&hir.InstrLoad{Dst: vValReg, Ptr: vPtrReg})
					vPtr := l.symbols[vId.Value]
					if vPtr == nil {
						vPtr = l.nextReg(&sema.PointerType{Base: sema.TypeInt}, vId.Value)
						l.emit(&hir.InstrAlloca{Dst: vPtr.(*hir.Reg), AllocType: sema.TypeInt})
						l.symbols[vId.Value] = vPtr
						l.symbolTypes[vId.Value] = sema.TypeInt
					}
					l.emit(&hir.InstrStore{Val: vValReg, Ptr: vPtr})
				}
			}

			l.lowerStmt(fr.Body)
			if l.curBlock.Terminator == nil {
				l.terminate(&hir.InstrJump{Target: condBB.Label})
			}

			l.setBlock(endBB)
			return
		}
	}

	if mp, isMap := xType.(*sema.MapType); isMap {
		bIdxAlloca := l.nextReg(&sema.PointerType{Base: sema.TypeInt}, "maprange.bidx")
		l.emit(&hir.InstrAlloca{Dst: bIdxAlloca, AllocType: sema.TypeInt})
		l.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: bIdxAlloca})

		entryAlloca := l.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}, "maprange.cur")
		l.emit(&hir.InstrAlloca{Dst: entryAlloca, AllocType: &sema.PointerType{Base: sema.TypeByte}})

		pBuckets := l.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		l.emit(&hir.InstrGetFieldPtr{Dst: pBuckets, BasePtr: xVal, FieldIndex: 0, FieldName: "buckets"})
		buckets := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrLoad{Dst: buckets, Ptr: pBuckets})

		pNumBuckets := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
		l.emit(&hir.InstrGetFieldPtr{Dst: pNumBuckets, BasePtr: xVal, FieldIndex: 1, FieldName: "numBuckets"})
		numBuckets := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrLoad{Dst: numBuckets, Ptr: pNumBuckets})

		bCondBB := l.newBlock("maprange.bcond")
		bBodyBB := l.newBlock("maprange.bbody")
		bPostBB := l.newBlock("maprange.bpost")
		eCondBB := l.newBlock("maprange.econd")
		eBodyBB := l.newBlock("maprange.ebody")
		ePostBB := l.newBlock("maprange.epost")
		endBB := l.newBlock("maprange.end")

		l.loopStack = append(l.loopStack, loopContext{breakBlock: endBB, continueBlock: ePostBB})
		defer func() {
			l.loopStack = l.loopStack[:len(l.loopStack)-1]
		}()

		l.terminate(&hir.InstrJump{Target: bCondBB.Label})

		l.setBlock(bCondBB)
		curBIdx := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrLoad{Dst: curBIdx, Ptr: bIdxAlloca})
		cmpB := l.nextReg(sema.TypeBool)
		l.emit(&hir.InstrBinary{Dst: cmpB, Op: hir.OpLt, L: curBIdx, R: numBuckets})
		l.terminate(&hir.InstrBranch{Cond: cmpB, ThenTarget: bBodyBB.Label, ElseTarget: endBB.Label})

		l.setBlock(bBodyBB)
		pHead := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrGetElemPtr{Dst: pHead, BasePtr: buckets, Index: curBIdx})
		head := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrLoad{Dst: head, Ptr: pHead})
		l.emit(&hir.InstrStore{Val: head, Ptr: entryAlloca})
		l.terminate(&hir.InstrJump{Target: eCondBB.Label})

		l.setBlock(eCondBB)
		curE := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrLoad{Dst: curE, Ptr: entryAlloca})
		hasE := l.nextReg(sema.TypeBool)
		l.emit(&hir.InstrBinary{Dst: hasE, Op: hir.OpNeq, L: curE, R: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}})
		l.terminate(&hir.InstrBranch{Cond: hasE, ThenTarget: eBodyBB.Label, ElseTarget: bPostBB.Label})

		l.setBlock(eBodyBB)
		if fr.Key != nil {
			if kId, ok := fr.Key.(*ast.Identifier); ok && kId.Value != "_" {
				pKey := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
				l.emit(&hir.InstrGetFieldPtr{Dst: pKey, BasePtr: curE, FieldIndex: 1, FieldName: "key"})
				rawKey := l.nextReg(sema.TypeInt)
				l.emit(&hir.InstrLoad{Dst: rawKey, Ptr: pKey})
				realKey := l.coerceFromI64(rawKey, mp.Key)

				kPtr := l.symbols[kId.Value]
				if kPtr == nil {
					kPtr = l.nextReg(&sema.PointerType{Base: mp.Key}, kId.Value)
					l.emit(&hir.InstrAlloca{Dst: kPtr.(*hir.Reg), AllocType: mp.Key})
					l.symbols[kId.Value] = kPtr
					l.symbolTypes[kId.Value] = mp.Key
				}
				l.emit(&hir.InstrStore{Val: realKey, Ptr: kPtr})
			}
		}
		if fr.Value != nil {
			if vId, ok := fr.Value.(*ast.Identifier); ok && vId.Value != "_" {
				pVal := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
				l.emit(&hir.InstrGetFieldPtr{Dst: pVal, BasePtr: curE, FieldIndex: 2, FieldName: "val"})
				rawVal := l.nextReg(sema.TypeInt)
				l.emit(&hir.InstrLoad{Dst: rawVal, Ptr: pVal})
				realVal := l.coerceFromI64(rawVal, mp.Value)

				vPtr := l.symbols[vId.Value]
				if vPtr == nil {
					vPtr = l.nextReg(&sema.PointerType{Base: mp.Value}, vId.Value)
					l.emit(&hir.InstrAlloca{Dst: vPtr.(*hir.Reg), AllocType: mp.Value})
					l.symbols[vId.Value] = vPtr
					l.symbolTypes[vId.Value] = mp.Value
				}
				l.emit(&hir.InstrStore{Val: realVal, Ptr: vPtr})
			}
		}

		l.lowerStmt(fr.Body)
		if l.curBlock.Terminator == nil {
			l.terminate(&hir.InstrJump{Target: ePostBB.Label})
		}

		l.setBlock(ePostBB)
		curEPost := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrLoad{Dst: curEPost, Ptr: entryAlloca})
		pNextE := l.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		l.emit(&hir.InstrGetFieldPtr{Dst: pNextE, BasePtr: curEPost, FieldIndex: 3, FieldName: "next"})
		nextE := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrLoad{Dst: nextE, Ptr: pNextE})
		l.emit(&hir.InstrStore{Val: nextE, Ptr: entryAlloca})
		l.terminate(&hir.InstrJump{Target: eCondBB.Label})

		l.setBlock(bPostBB)
		nextB := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrBinary{Dst: nextB, Op: hir.OpAdd, L: curBIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
		l.emit(&hir.InstrStore{Val: nextB, Ptr: bIdxAlloca})
		l.terminate(&hir.InstrJump{Target: bCondBB.Label})

		l.setBlock(endBB)
		return
	}

	var elemType sema.Type = sema.TypeByte
	var lenVal hir.Value = nil
	var dataPtr hir.Value = nil

	if sl, isSlice := xType.(*sema.SliceType); isSlice {
		elemType = sl.Elem
		rawBytePtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: xVal, Index: 0})
		typedPtr := l.nextReg(&sema.PointerType{Base: elemType})
		l.emit(&hir.InstrCast{Dst: typedPtr, Val: rawBytePtr, ToType: &sema.PointerType{Base: elemType}})
		lenReg := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrExtractValue{Dst: lenReg, Agg: xVal, Index: 1})
		dataPtr = typedPtr
		lenVal = lenReg
	} else if ar, isArr := xType.(*sema.ArrayType); isArr {
		elemType = ar.Elem
		lenVal = &hir.ConstInt{Val: int64(ar.Len), Typ: sema.TypeInt}
		dataPtr = l.lowerLValue(fr.X)
	} else {
		lenReg := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrCallStatic{Dst: lenReg, CalleeName: "strlen", Args: []hir.Value{xVal}})
		dataPtr = xVal
		lenVal = lenReg
		elemType = sema.TypeByte
	}

	idxAlloca := l.nextReg(&sema.PointerType{Base: sema.TypeInt}, "range.idx")
	l.emit(&hir.InstrAlloca{Dst: idxAlloca, AllocType: sema.TypeInt})
	l.emit(&hir.InstrStore{Val: &hir.ConstInt{Val: 0, Typ: sema.TypeInt}, Ptr: idxAlloca})

	condBB := l.newBlock("forrange.cond")
	bodyBB := l.newBlock("forrange.body")
	postBB := l.newBlock("forrange.post")
	endBB := l.newBlock("forrange.end")

	l.loopStack = append(l.loopStack, loopContext{breakBlock: endBB, continueBlock: postBB})
	defer func() {
		l.loopStack = l.loopStack[:len(l.loopStack)-1]
	}()

	l.terminate(&hir.InstrJump{Target: condBB.Label})

	l.setBlock(condBB)
	curIdx := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrLoad{Dst: curIdx, Ptr: idxAlloca})
	cmpReg := l.nextReg(sema.TypeBool)
	l.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpLt, L: curIdx, R: lenVal})
	l.terminate(&hir.InstrBranch{Cond: cmpReg, ThenTarget: bodyBB.Label, ElseTarget: endBB.Label})

	l.setBlock(bodyBB)
	if fr.Key != nil {
		if kId, ok := fr.Key.(*ast.Identifier); ok && kId.Value != "_" {
			kPtr := l.symbols[kId.Value]
			if kPtr == nil {
				kPtr = l.nextReg(&sema.PointerType{Base: sema.TypeInt}, kId.Value)
				l.emit(&hir.InstrAlloca{Dst: kPtr.(*hir.Reg), AllocType: sema.TypeInt})
				l.symbols[kId.Value] = kPtr
				l.symbolTypes[kId.Value] = sema.TypeInt
			}
			l.emit(&hir.InstrStore{Val: curIdx, Ptr: kPtr})
		}
	}
	if fr.Value != nil {
		if vId, ok := fr.Value.(*ast.Identifier); ok && vId.Value != "_" {
			elemPtrReg := l.nextReg(&sema.PointerType{Base: elemType})
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtrReg, BasePtr: dataPtr, Index: curIdx})
			elemValReg := l.nextReg(elemType)
			l.emit(&hir.InstrLoad{Dst: elemValReg, Ptr: elemPtrReg})

			vPtr := l.symbols[vId.Value]
			if vPtr == nil {
				vPtr = l.nextReg(&sema.PointerType{Base: elemType}, vId.Value)
				l.emit(&hir.InstrAlloca{Dst: vPtr.(*hir.Reg), AllocType: elemType})
				l.symbols[vId.Value] = vPtr
				l.symbolTypes[vId.Value] = elemType
			}
			l.emit(&hir.InstrStore{Val: elemValReg, Ptr: vPtr})
		}
	}

	l.lowerStmt(fr.Body)
	if l.curBlock.Terminator == nil {
		l.terminate(&hir.InstrJump{Target: postBB.Label})
	}

	l.setBlock(postBB)
	incIdx := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrBinary{Dst: incIdx, Op: hir.OpAdd, L: curIdx, R: &hir.ConstInt{Val: 1, Typ: sema.TypeInt}})
	l.emit(&hir.InstrStore{Val: incIdx, Ptr: idxAlloca})
	l.terminate(&hir.InstrJump{Target: condBB.Label})

	l.setBlock(endBB)
}

func (l *Lowerer) lowerSwitchStmt(ss *ast.SwitchStmt) {
	if ss.Init != nil {
		l.lowerStmt(ss.Init)
	}

	switchVal := l.lowerExpr(ss.Value)
	endBB := l.newBlock("switch.end")

	l.loopStack = append(l.loopStack, loopContext{breakBlock: endBB, continueBlock: endBB})
	defer func() {
		l.loopStack = l.loopStack[:len(l.loopStack)-1]
	}()

	var defaultCase *ast.CaseClause = nil

	for _, cc := range ss.Cases {
		if len(cc.Values) == 0 {
			defaultCase = cc
			continue
		}

		caseBodyBB := l.newBlock("switch.case.body")
		nextCaseBB := l.newBlock("switch.case.next")

		var matchedCond hir.Value = nil
		for _, valExpr := range cc.Values {
			vVal := l.lowerExpr(valExpr)
			var cmpReg *hir.Reg
			if vVal.Type() == sema.TypeString {
				cmpReg = l.nextReg(sema.TypeBool)
				l.emit(&hir.InstrCallStatic{Dst: cmpReg, CalleeName: "hike_streq", Args: []hir.Value{switchVal, vVal}})
			} else {
				cmpReg = l.nextReg(sema.TypeBool)
				l.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpEq, L: switchVal, R: vVal})
			}

			if matchedCond == nil {
				matchedCond = cmpReg
			} else {
				orReg := l.nextReg(sema.TypeBool)
				l.emit(&hir.InstrBinary{Dst: orReg, Op: hir.OpOr, L: matchedCond, R: cmpReg})
				matchedCond = orReg
			}
		}

		l.terminate(&hir.InstrBranch{Cond: matchedCond, ThenTarget: caseBodyBB.Label, ElseTarget: nextCaseBB.Label})

		l.setBlock(caseBodyBB)
		for _, stmt := range cc.Body {
			l.lowerStmt(stmt)
		}
		if l.curBlock.Terminator == nil {
			l.terminate(&hir.InstrJump{Target: endBB.Label})
		}

		l.setBlock(nextCaseBB)
	}

	if defaultCase != nil {
		for _, stmt := range defaultCase.Body {
			l.lowerStmt(stmt)
		}
	}
	if l.curBlock.Terminator == nil {
		l.terminate(&hir.InstrJump{Target: endBB.Label})
	}

	l.setBlock(endBB)
}

func (l *Lowerer) lowerTypeSwitchStmt(tss *ast.TypeSwitchStmt) {
	if tss.Init != nil {
		l.lowerStmt(tss.Init)
	}

	exprVal := l.lowerExpr(tss.Expr)
	exprType := exprVal.Type()
	endBB := l.newBlock("typeswitch.end")

	l.loopStack = append(l.loopStack, loopContext{breakBlock: endBB, continueBlock: endBB})
	defer func() {
		l.loopStack = l.loopStack[:len(l.loopStack)-1]
	}()

	dataPtrReg := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	actualTypeIDReg := l.nextReg(sema.TypeInt)

	if it, ok := exprType.(*sema.InterfaceType); ok && !it.IsAny() {
		itabRawReg := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 0})
		l.emit(&hir.InstrExtractValue{Dst: itabRawReg, Agg: exprVal, Index: 1})
		typeIDPtr := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
		l.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabRawReg, ToType: &sema.PointerType{Base: sema.TypeInt}})
		l.emit(&hir.InstrLoad{Dst: actualTypeIDReg, Ptr: typeIDPtr})
	} else {
		l.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: exprVal, Index: 0})
		l.emit(&hir.InstrExtractValue{Dst: actualTypeIDReg, Agg: exprVal, Index: 1})
	}

	var defaultCase *ast.TypeCaseClause = nil

	for _, c := range tss.Cases {
		if len(c.Types) == 0 {
			defaultCase = c
			continue
		}

		caseBodyBB := l.newBlock("typeswitch.case.body")
		nextCaseBB := l.newBlock("typeswitch.case.next")

		var matchedCond hir.Value = nil
		for _, tExpr := range c.Types {
			targetType := l.semaCtx.ResolveType(tExpr)
			targetTypeID := l.semaCtx.GetTypeID(targetType)

			cmpReg := l.nextReg(sema.TypeBool)
			l.emit(&hir.InstrBinary{Dst: cmpReg, Op: hir.OpEq, L: actualTypeIDReg, R: &hir.ConstInt{Val: targetTypeID, Typ: sema.TypeInt}})

			if matchedCond == nil {
				matchedCond = cmpReg
			} else {
				orReg := l.nextReg(sema.TypeBool)
				l.emit(&hir.InstrBinary{Dst: orReg, Op: hir.OpOr, L: matchedCond, R: cmpReg})
				matchedCond = orReg
			}
		}

		l.terminate(&hir.InstrBranch{Cond: matchedCond, ThenTarget: caseBodyBB.Label, ElseTarget: nextCaseBB.Label})

		l.setBlock(caseBodyBB)
		if tss.Variable != nil {
			var valToStore hir.Value
			if len(c.Types) == 1 {
				targetType := l.semaCtx.ResolveType(c.Types[0])
				castVal := l.nextReg(targetType)
				l.emit(&hir.InstrCast{Dst: castVal, Val: dataPtrReg, ToType: targetType})
				valToStore = castVal
			} else {
				valToStore = exprVal
			}
			vAlloca := l.nextReg(&sema.PointerType{Base: valToStore.Type()}, tss.Variable.Value)
			l.emit(&hir.InstrAlloca{Dst: vAlloca, AllocType: valToStore.Type()})
			l.emit(&hir.InstrStore{Val: valToStore, Ptr: vAlloca})
			l.symbols[tss.Variable.Value] = vAlloca
			l.symbolTypes[tss.Variable.Value] = valToStore.Type()
		}

		for _, stmt := range c.Body {
			l.lowerStmt(stmt)
		}
		if l.curBlock.Terminator == nil {
			l.terminate(&hir.InstrJump{Target: endBB.Label})
		}

		l.setBlock(nextCaseBB)
	}

	if defaultCase != nil {
		if tss.Variable != nil {
			vAlloca := l.nextReg(&sema.PointerType{Base: exprType}, tss.Variable.Value)
			l.emit(&hir.InstrAlloca{Dst: vAlloca, AllocType: exprType})
			l.emit(&hir.InstrStore{Val: exprVal, Ptr: vAlloca})
			l.symbols[tss.Variable.Value] = vAlloca
			l.symbolTypes[tss.Variable.Value] = exprType
		}
		for _, stmt := range defaultCase.Body {
			l.lowerStmt(stmt)
		}
	}
	if l.curBlock.Terminator == nil {
		l.terminate(&hir.InstrJump{Target: endBB.Label})
	}

	l.setBlock(endBB)
}

func (l *Lowerer) lowerReturnStmt(rs *ast.ReturnStmt) {
	vals := make([]hir.Value, len(rs.Values))
	for i, v := range rs.Values {
		vals[i] = l.lowerExpr(v)
	}

	for i := len(l.deferStack) - 1; i >= 0; i-- {
		l.lowerCall(l.deferStack[i])
	}

	l.terminate(&hir.InstrReturn{Vals: vals})
}

// -------------------------------------------------------------
// 式 (Expression) および LValue の変換
// -------------------------------------------------------------

func (l *Lowerer) lowerStructPtr(expr ast.Expression) hir.Value {
	if id, ok := expr.(*ast.Identifier); ok {
		if ptr, exists := l.symbols[id.Value]; exists {
			ptrType := ptr.Type().(*sema.PointerType)
			if _, isPtr := ptrType.Base.(*sema.PointerType); isPtr {
				loadReg := l.nextReg(ptrType.Base)
				l.emit(&hir.InstrLoad{Dst: loadReg, Ptr: ptr})
				return loadReg
			}
			return ptr
		}
		if g, exists := l.semaCtx.Globals[id.Value]; exists {
			if _, isPtr := g.(*sema.PointerType); isPtr {
				loadReg := l.nextReg(g)
				l.emit(&hir.InstrLoad{Dst: loadReg, Ptr: &hir.GlobalVar{Name: id.Value, Typ: &sema.PointerType{Base: g}}})
				return loadReg
			}
			return &hir.GlobalVar{Name: id.Value, Typ: &sema.PointerType{Base: g}}
		}
	}
	val := l.lowerExpr(expr)
	if _, isPtr := val.Type().(*sema.PointerType); isPtr {
		return val
	}
	allocaTmp := l.nextReg(&sema.PointerType{Base: val.Type()})
	l.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: val.Type()})
	l.emit(&hir.InstrStore{Val: val, Ptr: allocaTmp})
	return allocaTmp
}

func (l *Lowerer) lowerLValue(expr ast.Expression) hir.Value {
	switch e := expr.(type) {
	case *ast.Identifier:
		if ptr, ok := l.symbols[e.Value]; ok {
			return ptr
		}
		if g, ok := l.semaCtx.Globals[e.Value]; ok {
			return &hir.GlobalVar{Name: e.Value, Typ: &sema.PointerType{Base: g}}
		}
		panic(fmt.Sprintf("[Lower Error] undefined identifier for LValue: %s", e.Value))

	case *ast.PrefixExpr:
		if e.Operator == "*" {
			return l.lowerExpr(e.Right)
		}

	// 追記: 構造体リテラルのアドレス取得 (&Struct{...}) 用の一時メモリ化
	case *ast.StructLiteral:
		val := l.lowerExpr(e)
		allocaTmp := l.nextReg(&sema.PointerType{Base: val.Type()})
		l.emit(&hir.InstrAlloca{Dst: allocaTmp, AllocType: val.Type()})
		l.emit(&hir.InstrStore{Val: val, Ptr: allocaTmp})
		return allocaTmp

	case *ast.MemberExpr:
		if pkgId, okPkg := e.Object.(*ast.Identifier); okPkg {
			qualified := pkgId.Value + "_" + e.Field.Value
			if g, ok := l.semaCtx.Globals[qualified]; ok {
				return &hir.GlobalVar{Name: qualified, Typ: &sema.PointerType{Base: g}}
			}
		}
		objPtr := l.lowerStructPtr(e.Object)
		objType := objPtr.Type().(*sema.PointerType).Base
		st, sName := l.findStruct(objType)
		if st != nil {
			fieldPtr, _, _, found := l.resolveFieldPath(st, sName, objPtr, e.Field.Value)
			if found {
				return fieldPtr
			}
		}

	case *ast.IndexExpr:
		idxVal := l.lowerExpr(e.Index)
		baseVal := l.lowerExpr(e.Left)

		if sl, ok := baseVal.Type().(*sema.SliceType); ok {
			rawBytePtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
			l.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: baseVal, Index: 0})
			typedPtr := l.nextReg(&sema.PointerType{Base: sl.Elem})
			l.emit(&hir.InstrCast{Dst: typedPtr, Val: rawBytePtr, ToType: &sema.PointerType{Base: sl.Elem}})
			elemPtr := l.nextReg(&sema.PointerType{Base: sl.Elem})
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedPtr, Index: idxVal})
			return elemPtr
		} else if pt, ok := baseVal.Type().(*sema.PointerType); ok {
			elemPtr := l.nextReg(pt)
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: baseVal, Index: idxVal})
			return elemPtr
		} else if ar, ok := baseVal.Type().(*sema.ArrayType); ok {
			basePtr := l.lowerLValue(e.Left)
			elemPtr := l.nextReg(&sema.PointerType{Base: ar.Elem})
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: basePtr, Index: idxVal})
			return elemPtr
		}
	}

	panic(fmt.Sprintf("[Lower Error] expression is not an lvalue: %T", expr))
}

func (l *Lowerer) lowerExpr(expr ast.Expression) hir.Value {
	if expr == nil {
		return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
	}

	switch e := expr.(type) {
	case *ast.IntegerLiteral:
		return &hir.ConstInt{Val: e.Value, Typ: sema.TypeInt}

	case *ast.FloatLiteral:
		return &hir.ConstFloat{Val: e.Value, Typ: sema.TypeFloat64}

	case *ast.StringLiteral:
		return l.getStringConst(e.Value)

	case *ast.NilLiteral:
		return &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}

	case *ast.ImplicitCastExpr:
		val := l.lowerExpr(e.Expr)
		targetT := l.semaCtx.ResolveType(e.TargetType)
		if val.Type().LLVMType() == targetT.LLVMType() {
			return val
		}
		if iface, ok := targetT.(*sema.InterfaceType); ok {
			itabName := ""
			if !iface.IsAny() {
				itabDef := l.getOrCreateItab(val.Type(), iface)
				itabName = itabDef.GlobalName
			}
			dst := l.nextReg(iface)
			l.emit(&hir.InstrBoxInterface{Dst: dst, Val: val, Iface: iface, ItabName: itabName})
			return dst
		}
		dst := l.nextReg(targetT)
		l.emit(&hir.InstrCast{Dst: dst, Val: val, ToType: targetT})
		return dst

	case *ast.Identifier:
		if e.Value == "true" {
			return &hir.ConstBool{Val: true, Typ: sema.TypeBool}
		}
		if e.Value == "false" {
			return &hir.ConstBool{Val: false, Typ: sema.TypeBool}
		}
		if c, ok := l.semaCtx.Constants[e.Value]; ok {
			return &hir.ConstInt{Val: c, Typ: sema.TypeInt}
		}
		if c, ok := l.semaCtx.FloatConstants[e.Value]; ok {
			return &hir.ConstFloat{Val: c, Typ: sema.TypeFloat64}
		}
		if ptr, ok := l.symbols[e.Value]; ok {
			ptrType := ptr.Type().(*sema.PointerType)
			dst := l.nextReg(ptrType.Base)
			l.emit(&hir.InstrLoad{Dst: dst, Ptr: ptr})
			return dst
		}
		if g, ok := l.semaCtx.Globals[e.Value]; ok {
			dst := l.nextReg(g)
			l.emit(&hir.InstrLoad{Dst: dst, Ptr: &hir.GlobalVar{Name: e.Value, Typ: &sema.PointerType{Base: g}}})
			return dst
		}
		if fn, ok := l.semaCtx.Functions[e.Value]; ok {
			fatType := fn
			t1 := l.nextReg(fatType)
			l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(fatType), Val: &hir.GlobalVar{Name: fn.Name, Typ: &sema.PointerType{Base: sema.TypeByte}}, Index: 0})
			t2 := l.nextReg(fatType)
			l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}, Index: 1})
			return t2
		}
		panic(fmt.Sprintf("[Lower Error] undefined identifier: %s", e.Value))

	case *ast.BinaryExpr:
		return l.lowerBinaryExpr(e)

	case *ast.PrefixExpr:
		if e.Operator == "&" {
			return l.lowerLValue(e.Right)
		}
		if e.Operator == "*" {
			ptrVal := l.lowerExpr(e.Right)
			baseType := ptrVal.Type().(*sema.PointerType).Base
			dst := l.nextReg(baseType)
			l.emit(&hir.InstrLoad{Dst: dst, Ptr: ptrVal})
			return dst
		}
		val := l.lowerExpr(e.Right)
		op := hir.OpNeg
		if e.Operator == "!" {
			op = hir.OpNot
		}
		dst := l.nextReg(val.Type())
		l.emit(&hir.InstrUnary{Dst: dst, Op: op, Val: val})
		return dst

	case *ast.StructLiteral:
		stType := l.semaCtx.ResolveType(e.Type).(*sema.StructType)
		allocaReg := l.nextReg(&sema.PointerType{Base: stType})
		l.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: stType})

		for i, fVal := range e.Fields {
			val := l.lowerExpr(fVal.Value)
			fieldIdx := i
			var fName string
			if fVal.Name != nil {
				fName = fVal.Name.Value
				for idx, sf := range stType.Fields {
					if sf.Name == fName {
						fieldIdx = idx
						break
					}
				}
			} else {
				fName = stType.Fields[i].Name
			}
			fPtrReg := l.nextReg(&sema.PointerType{Base: val.Type()})
			l.emit(&hir.InstrGetFieldPtr{Dst: fPtrReg, BasePtr: allocaReg, FieldIndex: fieldIdx, FieldName: fName})
			l.emit(&hir.InstrStore{Val: val, Ptr: fPtrReg})
		}

		resReg := l.nextReg(stType)
		l.emit(&hir.InstrLoad{Dst: resReg, Ptr: allocaReg})
		return resReg

	case *ast.ArrayLiteral:
		arType := l.semaCtx.ResolveType(e.Type).(*sema.ArrayType)
		allocaReg := l.nextReg(&sema.PointerType{Base: arType})
		l.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: arType})

		for i, el := range e.Elements {
			val := l.lowerExpr(el)
			elemPtr := l.nextReg(&sema.PointerType{Base: arType.Elem})
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: allocaReg, Index: &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt}})
			l.emit(&hir.InstrStore{Val: val, Ptr: elemPtr})
		}

		resReg := l.nextReg(arType)
		l.emit(&hir.InstrLoad{Dst: resReg, Ptr: allocaReg})
		return resReg

	case *ast.SliceLiteral:
		slType := l.semaCtx.ResolveType(e.Type).(*sema.SliceType)
		count := len(e.Elements)
		elemSize := slType.Elem.Size()
		if elemSize <= 0 {
			elemSize = 1
		}
		totalBytes := count * elemSize

		mallocRaw := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrHeapAlloc{Dst: mallocRaw, Size: &hir.ConstInt{Val: int64(totalBytes), Typ: sema.TypeInt}, AllocType: sema.TypeByte})

		typedBase := l.nextReg(&sema.PointerType{Base: slType.Elem})
		l.emit(&hir.InstrCast{Dst: typedBase, Val: mallocRaw, ToType: &sema.PointerType{Base: slType.Elem}})

		for i, el := range e.Elements {
			val := l.lowerExpr(el)
			elemPtr := l.nextReg(&sema.PointerType{Base: slType.Elem})
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedBase, Index: &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt}})
			l.emit(&hir.InstrStore{Val: val, Ptr: elemPtr})
		}

		t1 := l.nextReg(slType)
		l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(slType), Val: mallocRaw, Index: 0})
		t2 := l.nextReg(slType)
		l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: &hir.ConstInt{Val: int64(count), Typ: sema.TypeInt}, Index: 1})
		t3 := l.nextReg(slType)
		l.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: &hir.ConstInt{Val: int64(count), Typ: sema.TypeInt}, Index: 2})
		return t3

	case *ast.SliceExpr:
		baseVal := l.lowerExpr(e.Left)
		if baseVal.Type() == sema.TypeString {
			lowVal := hir.Value(&hir.ConstInt{Val: 0, Typ: sema.TypeInt})
			if e.Low != nil {
				lowVal = l.lowerExpr(e.Low)
			}
			highVal := hir.Value(l.nextReg(sema.TypeInt))
			if e.High != nil {
				highVal = l.lowerExpr(e.High)
			} else {
				l.emit(&hir.InstrCallStatic{Dst: highVal.(*hir.Reg), CalleeName: "strlen", Args: []hir.Value{baseVal}})
			}
			subRes := l.nextReg(sema.TypeString)
			l.emit(&hir.InstrCallStatic{Dst: subRes, CalleeName: "hike_substr", Args: []hir.Value{baseVal, lowVal, highVal}})
			return subRes
		}

		var elemType sema.Type = sema.TypeByte
		var typedDataPtr hir.Value = nil
		var capVal hir.Value = nil

		if slType, isSlice := baseVal.Type().(*sema.SliceType); isSlice {
			elemType = slType.Elem
			rawBytePtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
			cVal := l.nextReg(sema.TypeInt)
			l.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: baseVal, Index: 0})
			l.emit(&hir.InstrExtractValue{Dst: cVal, Agg: baseVal, Index: 2})
			tPtr := l.nextReg(&sema.PointerType{Base: elemType})
			l.emit(&hir.InstrCast{Dst: tPtr, Val: rawBytePtr, ToType: &sema.PointerType{Base: elemType}})
			typedDataPtr = tPtr
			capVal = cVal
		} else if arType, isArray := baseVal.Type().(*sema.ArrayType); isArray {
			elemType = arType.Elem
			arrPtr := l.lowerLValue(e.Left)
			tPtr := l.nextReg(&sema.PointerType{Base: elemType})
			l.emit(&hir.InstrCast{Dst: tPtr, Val: arrPtr, ToType: &sema.PointerType{Base: elemType}})
			typedDataPtr = tPtr
			capVal = &hir.ConstInt{Val: int64(arType.Len), Typ: sema.TypeInt}
		} else {
			panic(fmt.Sprintf("[Lower Error] cannot slice type %s", baseVal.Type().TypeName()))
		}

		lowVal := hir.Value(&hir.ConstInt{Val: 0, Typ: sema.TypeInt})
		if e.Low != nil {
			lowVal = l.lowerExpr(e.Low)
		}
		highVal := hir.Value(capVal)
		if e.High != nil {
			highVal = l.lowerExpr(e.High)
		}

		elemPtr := l.nextReg(&sema.PointerType{Base: elemType})
		l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedDataPtr, Index: lowVal})
		newLen := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrBinary{Dst: newLen, Op: hir.OpSub, L: highVal, R: lowVal})
		newCap := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrBinary{Dst: newCap, Op: hir.OpSub, L: capVal, R: lowVal})

		elemBytePtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrCast{Dst: elemBytePtr, Val: elemPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})

		resSliceType := &sema.SliceType{Elem: elemType}
		t1 := l.nextReg(resSliceType)
		l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(resSliceType), Val: elemBytePtr, Index: 0})
		t2 := l.nextReg(resSliceType)
		l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: newLen, Index: 1})
		t3 := l.nextReg(resSliceType)
		l.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: newCap, Index: 2})
		return t3

	case *ast.CallExpr:
		if len(e.Args) == 1 {
			if targetType := l.resolveTypeFromExpr(e.Function); targetType != nil && targetType != sema.TypeVoid {
				if _, isFunc := targetType.(*sema.FuncType); !isFunc {
					argVal := l.lowerExpr(e.Args[0])
					if argVal.Type().LLVMType() == targetType.LLVMType() {
						return argVal
					}
					// []byte から string へのキャスト: スライスのデータポインタ (Index 0) を抽出
					if _, isSlice := argVal.Type().(*sema.SliceType); isSlice && targetType == sema.TypeString {
						dst := l.nextReg(sema.TypeString)
						l.emit(&hir.InstrExtractValue{Dst: dst, Agg: argVal, Index: 0})
						return dst
					}
					dst := l.nextReg(targetType)
					l.emit(&hir.InstrCast{Dst: dst, Val: argVal, ToType: targetType})
					return dst
				}
			}
		}
		return l.lowerCall(e)

	case *ast.MemberExpr:
		if pkgId, okPkg := e.Object.(*ast.Identifier); okPkg {
			qualified := pkgId.Value + "_" + e.Field.Value
			if c, ok := l.semaCtx.Constants[qualified]; ok {
				return &hir.ConstInt{Val: c, Typ: sema.TypeInt}
			}
			if c, ok := l.semaCtx.FloatConstants[qualified]; ok {
				return &hir.ConstFloat{Val: c, Typ: sema.TypeFloat64}
			}
			if g, ok := l.semaCtx.Globals[qualified]; ok {
				dst := l.nextReg(g)
				l.emit(&hir.InstrLoad{Dst: dst, Ptr: &hir.GlobalVar{Name: qualified, Typ: &sema.PointerType{Base: g}}})
				return dst
			}
		}
		ptr := l.lowerLValue(e)
		ptrType := ptr.Type().(*sema.PointerType)
		dst := l.nextReg(ptrType.Base)
		l.emit(&hir.InstrLoad{Dst: dst, Ptr: ptr})
		return dst

	case *ast.IndexExpr:
		baseVal := l.lowerExpr(e.Left)
		idxVal := l.lowerExpr(e.Index)

		// 1. Map
		if mp, isMap := baseVal.Type().(*sema.MapType); isMap {
			keyI64 := l.coerceToI64(idxVal, mp.Key)
			outPtr := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
			l.emit(&hir.InstrAlloca{Dst: outPtr, AllocType: sema.TypeInt})
			l.emit(&hir.InstrCallStatic{CalleeName: "__hike_map_get", Args: []hir.Value{baseVal, keyI64, outPtr}})
			rawVal := l.nextReg(sema.TypeInt)
			l.emit(&hir.InstrLoad{Dst: rawVal, Ptr: outPtr})
			return l.coerceFromI64(rawVal, mp.Value)
		}

		// 2. String (byte indexing)
		if baseVal.Type() == sema.TypeString {
			elemPtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: baseVal, Index: idxVal})
			elemVal := l.nextReg(sema.TypeByte)
			l.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}

		// 3. Slice
		if sl, isSlice := baseVal.Type().(*sema.SliceType); isSlice {
			rawBytePtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
			l.emit(&hir.InstrExtractValue{Dst: rawBytePtr, Agg: baseVal, Index: 0})
			typedPtr := l.nextReg(&sema.PointerType{Base: sl.Elem})
			l.emit(&hir.InstrCast{Dst: typedPtr, Val: rawBytePtr, ToType: &sema.PointerType{Base: sl.Elem}})
			elemPtr := l.nextReg(&sema.PointerType{Base: sl.Elem})
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: typedPtr, Index: idxVal})
			elemVal := l.nextReg(sl.Elem)
			l.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}

		// 4. Pointer (*T indexing)
		if pt, isPtr := baseVal.Type().(*sema.PointerType); isPtr {
			elemPtr := l.nextReg(pt)
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: baseVal, Index: idxVal})
			elemVal := l.nextReg(pt.Base)
			l.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}

		// 5. Array
		if ar, isArr := baseVal.Type().(*sema.ArrayType); isArr {
			arrPtr := l.lowerLValue(e.Left)
			elemPtr := l.nextReg(&sema.PointerType{Base: ar.Elem})
			l.emit(&hir.InstrGetElemPtr{Dst: elemPtr, BasePtr: arrPtr, Index: idxVal})
			elemVal := l.nextReg(ar.Elem)
			l.emit(&hir.InstrLoad{Dst: elemVal, Ptr: elemPtr})
			return elemVal
		}

		panic(fmt.Sprintf("[Lower Error] unsupported index target type: %s", baseVal.Type().TypeName()))

	case *ast.TypeAssertExpr:
		return l.lowerTypeAssertExpr(e)

	case *ast.FuncLit:
		return l.lowerFuncLit(e)
	}

	return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
}

func (l *Lowerer) lowerTypeAssertExpr(tae *ast.TypeAssertExpr) hir.Value {
	ifaceVal := l.lowerExpr(tae.Expr)
	ifaceType := ifaceVal.Type()
	targetType := l.semaCtx.ResolveType(tae.Target)
	targetTypeID := l.semaCtx.GetTypeID(targetType)

	dataPtrReg := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	typeIDReg := l.nextReg(sema.TypeInt)

	if it, ok := ifaceType.(*sema.InterfaceType); ok && !it.IsAny() {
		itabRawReg := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: ifaceVal, Index: 0})
		l.emit(&hir.InstrExtractValue{Dst: itabRawReg, Agg: ifaceVal, Index: 1})
		typeIDPtr := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
		l.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabRawReg, ToType: &sema.PointerType{Base: sema.TypeInt}})
		l.emit(&hir.InstrLoad{Dst: typeIDReg, Ptr: typeIDPtr})
	} else {
		l.emit(&hir.InstrExtractValue{Dst: dataPtrReg, Agg: ifaceVal, Index: 0})
		l.emit(&hir.InstrExtractValue{Dst: typeIDReg, Agg: ifaceVal, Index: 1})
	}

	matchReg := l.nextReg(sema.TypeBool)
	l.emit(&hir.InstrBinary{Dst: matchReg, Op: hir.OpEq, L: typeIDReg, R: &hir.ConstInt{Val: targetTypeID, Typ: sema.TypeInt}})

	unpackedReg := l.nextReg(targetType)
	if strings.HasSuffix(targetType.LLVMType(), "*") {
		l.emit(&hir.InstrCast{Dst: unpackedReg, Val: dataPtrReg, ToType: targetType})
	} else {
		typedPtr := l.nextReg(&sema.PointerType{Base: targetType})
		l.emit(&hir.InstrCast{Dst: typedPtr, Val: dataPtrReg, ToType: &sema.PointerType{Base: targetType}})
		l.emit(&hir.InstrLoad{Dst: unpackedReg, Ptr: typedPtr})
	}

	tupleType := &sema.TupleType{Types: []sema.Type{targetType, sema.TypeBool}}
	t1 := l.nextReg(tupleType)
	l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(tupleType), Val: unpackedReg, Index: 0})
	t2 := l.nextReg(tupleType)
	l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: matchReg, Index: 1})
	return t2
}

func (l *Lowerer) lowerBinaryExpr(e *ast.BinaryExpr) hir.Value {
	if e.Operator == "&&" {
		resAlloca := l.nextReg(&sema.PointerType{Base: sema.TypeBool}, "land.res")
		l.emit(&hir.InstrAlloca{Dst: resAlloca, AllocType: sema.TypeBool})
		l.emit(&hir.InstrStore{Val: &hir.ConstBool{Val: false, Typ: sema.TypeBool}, Ptr: resAlloca})

		leftVal := l.lowerExpr(e.Left)
		rhsBB := l.newBlock("land.rhs")
		endBB := l.newBlock("land.end")

		l.terminate(&hir.InstrBranch{Cond: leftVal, ThenTarget: rhsBB.Label, ElseTarget: endBB.Label})

		l.setBlock(rhsBB)
		rightVal := l.lowerExpr(e.Right)
		l.emit(&hir.InstrStore{Val: rightVal, Ptr: resAlloca})
		l.terminate(&hir.InstrJump{Target: endBB.Label})

		l.setBlock(endBB)
		finalReg := l.nextReg(sema.TypeBool)
		l.emit(&hir.InstrLoad{Dst: finalReg, Ptr: resAlloca})
		return finalReg
	}

	if e.Operator == "||" {
		resAlloca := l.nextReg(&sema.PointerType{Base: sema.TypeBool}, "lor.res")
		l.emit(&hir.InstrAlloca{Dst: resAlloca, AllocType: sema.TypeBool})
		l.emit(&hir.InstrStore{Val: &hir.ConstBool{Val: true, Typ: sema.TypeBool}, Ptr: resAlloca})

		leftVal := l.lowerExpr(e.Left)
		rhsBB := l.newBlock("lor.rhs")
		endBB := l.newBlock("lor.end")

		l.terminate(&hir.InstrBranch{Cond: leftVal, ThenTarget: endBB.Label, ElseTarget: rhsBB.Label})

		l.setBlock(rhsBB)
		rightVal := l.lowerExpr(e.Right)
		l.emit(&hir.InstrStore{Val: rightVal, Ptr: resAlloca})
		l.terminate(&hir.InstrJump{Target: endBB.Label})

		l.setBlock(endBB)
		finalReg := l.nextReg(sema.TypeBool)
		l.emit(&hir.InstrLoad{Dst: finalReg, Ptr: resAlloca})
		return finalReg
	}

	leftVal := l.lowerExpr(e.Left)
	rightVal := l.lowerExpr(e.Right)

	if leftVal.Type() == sema.TypeString || rightVal.Type() == sema.TypeString {
		if e.Operator == "+" {
			res := l.nextReg(sema.TypeString)
			l.emit(&hir.InstrCallStatic{Dst: res, CalleeName: "hike_strcat", Args: []hir.Value{leftVal, rightVal}})
			return res
		}
		if e.Operator == "==" || e.Operator == "!=" {
			eqRes := l.nextReg(sema.TypeBool)
			l.emit(&hir.InstrCallStatic{Dst: eqRes, CalleeName: "hike_streq", Args: []hir.Value{leftVal, rightVal}})
			if e.Operator == "!=" {
				notRes := l.nextReg(sema.TypeBool)
				l.emit(&hir.InstrUnary{Dst: notRes, Op: hir.OpNot, Val: eqRes})
				return notRes
			}
			return eqRes
		}
	}

	op := hir.OpAdd
	resType := leftVal.Type()

	switch e.Operator {
	case "+":
		op = hir.OpAdd
	case "-":
		op = hir.OpSub
	case "*":
		op = hir.OpMul
	case "/":
		op = hir.OpDiv
	case "%":
		op = hir.OpRem
	case "&":
		op = hir.OpAnd
	case "|":
		op = hir.OpOr
	case "^":
		op = hir.OpXor
	case "<<":
		op = hir.OpShl
	case ">>":
		op = hir.OpShr
	case "==":
		op = hir.OpEq
		resType = sema.TypeBool
	case "!=":
		op = hir.OpNeq
		resType = sema.TypeBool
	case "<":
		op = hir.OpLt
		resType = sema.TypeBool
	case "<=":
		op = hir.OpLe
		resType = sema.TypeBool
	case ">":
		op = hir.OpGt
		resType = sema.TypeBool
	case ">=":
		op = hir.OpGe
		resType = sema.TypeBool
	}

	dst := l.nextReg(resType)
	l.emit(&hir.InstrBinary{Dst: dst, Op: op, L: leftVal, R: rightVal})
	return dst
}

// -------------------------------------------------------------
// 関数呼び出し (Call) と ビルトイン操作の変換
// -------------------------------------------------------------

func (l *Lowerer) lowerCall(call *ast.CallExpr) hir.Value {
	if len(call.Args) == 1 {
		targetType := l.resolveTypeFromExpr(call.Function)
		if targetType != nil && targetType != sema.TypeVoid {
			if _, isFunc := targetType.(*sema.FuncType); !isFunc {
				argVal := l.lowerExpr(call.Args[0])
				if argVal.Type().LLVMType() == targetType.LLVMType() {
					return argVal
				}
				dst := l.nextReg(targetType)
				l.emit(&hir.InstrCast{Dst: dst, Val: argVal, ToType: targetType})
				return dst
			}
		}
	}

	if fnId, ok := call.Function.(*ast.Identifier); ok {
		switch fnId.Value {
		case "make":
			if mapTypeNode, okMap := call.Args[0].(*ast.MapType); okMap {
				kType := l.semaCtx.ResolveType(mapTypeNode.Key)
				vType := l.semaCtx.ResolveType(mapTypeNode.Value)
				resMapType := &sema.MapType{Key: kType, Value: vType}
				isStr := 0
				if kType == sema.TypeString {
					isStr = 1
				}
				capVal := hir.Value(&hir.ConstInt{Val: 16, Typ: sema.TypeInt})
				if len(call.Args) >= 2 {
					capVal = l.lowerExpr(call.Args[1])
				}
				dst := l.nextReg(resMapType)
				l.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "__hike_map_create", Args: []hir.Value{capVal, &hir.ConstInt{Val: int64(isStr), Typ: sema.TypeInt}}})
				return dst
			}
			if slNode, okSlice := call.Args[0].(*ast.SliceType); okSlice {
				elemType := l.semaCtx.ResolveType(slNode.Elem)
				resSliceType := &sema.SliceType{Elem: elemType}
				lenVal := l.lowerExpr(call.Args[1])
				capVal := lenVal
				if len(call.Args) >= 3 {
					capVal = l.lowerExpr(call.Args[2])
				}
				elemSize := elemType.Size()
				if elemSize <= 0 {
					elemSize = 1
				}
				callocRaw := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
				l.emit(&hir.InstrCallStatic{Dst: callocRaw, CalleeName: "calloc", Args: []hir.Value{capVal, &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}}})

				t1 := l.nextReg(resSliceType)
				l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(resSliceType), Val: callocRaw, Index: 0})
				t2 := l.nextReg(resSliceType)
				l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: lenVal, Index: 1})
				t3 := l.nextReg(resSliceType)
				l.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: capVal, Index: 2})
				return t3
			}

		case "delete":
			argVal := l.lowerExpr(call.Args[0])
			keyVal := l.lowerExpr(call.Args[1])
			if mp, isMap := argVal.Type().(*sema.MapType); isMap {
				keyI64 := l.coerceToI64(keyVal, mp.Key)
				l.emit(&hir.InstrCallStatic{CalleeName: "__hike_map_delete", Args: []hir.Value{argVal, keyI64}})
				return nil
			}

		case "len", "cap":
			argVal := l.lowerExpr(call.Args[0])
			if _, isSlice := argVal.Type().(*sema.SliceType); isSlice {
				idx := 1
				if fnId.Value == "cap" {
					idx = 2
				}
				dst := l.nextReg(sema.TypeInt)
				l.emit(&hir.InstrExtractValue{Dst: dst, Agg: argVal, Index: idx})
				return dst
			}
			if fnId.Value == "len" && argVal.Type() == sema.TypeString {
				dst := l.nextReg(sema.TypeInt)
				l.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "strlen", Args: []hir.Value{argVal}})
				return dst
			}
			if _, isMap := argVal.Type().(*sema.MapType); isMap {
				dst := l.nextReg(sema.TypeInt)
				l.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: "__hike_map_len", Args: []hir.Value{argVal}})
				return dst
			}

		case "append":
			return l.lowerAppend(call)

		case "string":
			argVal := l.lowerExpr(call.Args[0])
			if _, isSlice := argVal.Type().(*sema.SliceType); isSlice {
				dst := l.nextReg(sema.TypeString)
				l.emit(&hir.InstrExtractValue{Dst: dst, Agg: argVal, Index: 0})
				return dst
			}
			return argVal
		}
	}

	if mem, ok := call.Function.(*ast.MemberExpr); ok {
		isVariable := false
		if objIdent, okObj := mem.Object.(*ast.Identifier); okObj {
			if _, exists := l.symbols[objIdent.Value]; exists {
				isVariable = true
			}
			if _, exists := l.semaCtx.Globals[objIdent.Value]; exists {
				isVariable = true
			}
		}

		if !isVariable {
			if pkgIdent, isIdent := mem.Object.(*ast.Identifier); isIdent {
				methodName := mem.Field.Value
				targetFnName := pkgIdent.Value + "_" + methodName
				targetFn, canonicalName := l.semaCtx.LookupFunction(targetFnName)
				if targetFn == nil {
					targetFn, canonicalName = l.semaCtx.LookupFunction(methodName)
				}

				if targetFn != nil {
					args := make([]hir.Value, len(call.Args))
					for i, arg := range call.Args {
						args[i] = l.lowerExpr(arg)
					}

					var retType sema.Type = sema.TypeVoid
					if len(targetFn.ReturnTypes) == 1 {
						retType = targetFn.ReturnTypes[0]
					} else if len(targetFn.ReturnTypes) > 1 {
						retType = &sema.TupleType{Types: targetFn.ReturnTypes}
					}

					var dst *hir.Reg = nil
					if retType != sema.TypeVoid {
						dst = l.nextReg(retType)
					}
					l.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: canonicalName, Args: args})
					return dst
				}
			}
		}

		objPtr := l.lowerStructPtr(mem.Object)
		objType := objPtr.Type().(*sema.PointerType).Base

		if iface, isIface := objType.(*sema.InterfaceType); isIface && !iface.IsAny() {
			methodIdx := 0
			var targetMethod sema.Method
			for idx, m := range iface.Methods {
				if m.Name == mem.Field.Value {
					methodIdx = idx
					targetMethod = m
					break
				}
			}

			args := make([]hir.Value, len(call.Args))
			for i, arg := range call.Args {
				args[i] = l.lowerExpr(arg)
			}

			var retType sema.Type = sema.TypeVoid
			if len(targetMethod.ReturnTypes) == 1 {
				retType = targetMethod.ReturnTypes[0]
			} else if len(targetMethod.ReturnTypes) > 1 {
				retType = &sema.TupleType{Types: targetMethod.ReturnTypes}
			}

			var dst *hir.Reg = nil
			if retType != sema.TypeVoid {
				dst = l.nextReg(retType)
			}

			ifaceVal := l.nextReg(iface)
			l.emit(&hir.InstrLoad{Dst: ifaceVal, Ptr: objPtr})

			l.emit(&hir.InstrCallIface{
				Dst:         dst,
				IfaceVal:    ifaceVal,
				MethodIndex: methodIdx,
				MethodName:  mem.Field.Value,
				Args:        args,
			})
			return dst
		}

		st, sName := l.findStruct(objType)
		if st != nil {
			targetFnName, targetFn, finalRecv, found := l.resolveMethodPath(st, sName, objPtr, mem.Field.Value)
			if found && targetFn != nil {
				recvArg := finalRecv
				// 値レシーバの場合はポインタではなく構造体データを直接ロードして渡す
				if !l.isPointerReceiver(targetFnName) {
					if ptrType, ok := finalRecv.Type().(*sema.PointerType); ok {
						loaded := l.nextReg(ptrType.Base)
						l.emit(&hir.InstrLoad{Dst: loaded, Ptr: finalRecv})
						recvArg = loaded
					}
				}

				args := []hir.Value{recvArg}
				for _, arg := range call.Args {
					args = append(args, l.lowerExpr(arg))
				}

				var retType sema.Type = sema.TypeVoid
				if len(targetFn.ReturnTypes) == 1 {
					retType = targetFn.ReturnTypes[0]
				} else if len(targetFn.ReturnTypes) > 1 {
					retType = &sema.TupleType{Types: targetFn.ReturnTypes}
				}

				var dst *hir.Reg = nil
				if retType != sema.TypeVoid {
					dst = l.nextReg(retType)
				}
				l.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: targetFnName, Args: args})
				return dst
			}
		}
	}

	if fnId, ok := call.Function.(*ast.Identifier); ok {
		_, isLocal := l.symbols[fnId.Value]
		_, isGlobal := l.semaCtx.Globals[fnId.Value]
		if !isLocal && !isGlobal {
			targetFn, canonicalName := l.semaCtx.LookupFunction(fnId.Value)
			if targetFn != nil {
				args := make([]hir.Value, len(call.Args))
				for i, arg := range call.Args {
					val := l.lowerExpr(arg)
					// C-ABI Variadic Promotion (bool/byte -> i64, float32 -> double)
					if targetFn.IsVariadic && i >= len(targetFn.ParamTypes) {
						if val.Type() == sema.TypeBool || val.Type().LLVMType() == "i1" {
							extReg := l.nextReg(sema.TypeInt)
							l.emit(&hir.InstrCast{Dst: extReg, Val: val, ToType: sema.TypeInt})
							val = extReg
						} else if val.Type() == sema.TypeByte || val.Type().LLVMType() == "i8" {
							extReg := l.nextReg(sema.TypeInt)
							l.emit(&hir.InstrCast{Dst: extReg, Val: val, ToType: sema.TypeInt})
							val = extReg
						} else if val.Type() == sema.TypeFloat32 || val.Type().LLVMType() == "float" {
							extReg := l.nextReg(sema.TypeFloat64)
							l.emit(&hir.InstrCast{Dst: extReg, Val: val, ToType: sema.TypeFloat64})
							val = extReg
						}
					}
					args[i] = val
				}

				var retType sema.Type = sema.TypeVoid
				if len(targetFn.ReturnTypes) == 1 {
					retType = targetFn.ReturnTypes[0]
				} else if len(targetFn.ReturnTypes) > 1 {
					retType = &sema.TupleType{Types: targetFn.ReturnTypes}
				}

				var dst *hir.Reg = nil
				if retType != sema.TypeVoid {
					dst = l.nextReg(retType)
				}
				l.emit(&hir.InstrCallStatic{Dst: dst, CalleeName: canonicalName, Args: args})
				return dst
			}
		}
	}

	fnFatPtr := l.lowerExpr(call.Function)
	fnPtrReg := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	envPtrReg := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	l.emit(&hir.InstrExtractValue{Dst: fnPtrReg, Agg: fnFatPtr, Index: 0})
	l.emit(&hir.InstrExtractValue{Dst: envPtrReg, Agg: fnFatPtr, Index: 1})

	args := make([]hir.Value, len(call.Args))
	for i, arg := range call.Args {
		args[i] = l.lowerExpr(arg)
	}

	ft := fnFatPtr.Type().(*sema.FuncType)
	var retType sema.Type = sema.TypeVoid
	if len(ft.ReturnTypes) == 1 {
		retType = ft.ReturnTypes[0]
	} else if len(ft.ReturnTypes) > 1 {
		retType = &sema.TupleType{Types: ft.ReturnTypes}
	}

	var dst *hir.Reg = nil
	if retType != sema.TypeVoid {
		dst = l.nextReg(retType)
	}
	l.emit(&hir.InstrCallIndirect{
		Dst:    dst,
		FnPtr:  fnPtrReg,
		EnvPtr: envPtrReg,
		Args:   args,
	})
	return dst
}

func (l *Lowerer) isPointerReceiver(targetFnName string) bool {
	for _, decl := range l.prog.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Receiver != nil {
			rType := l.semaCtx.ResolveType(fnDecl.Receiver.Type)
			if rType != nil {
				rName := strings.TrimPrefix(rType.TypeName(), "*")
				if rName+"_"+fnDecl.Name.Value == targetFnName {
					_, isPtr := rType.(*sema.PointerType)
					return isPtr
				}
			}
		}
	}
	return true
}

func (l *Lowerer) lowerAppend(call *ast.CallExpr) hir.Value {
	sliceVal := l.lowerExpr(call.Args[0])
	slType := sliceVal.Type().(*sema.SliceType)
	elemSize := slType.Elem.Size()
	if elemSize <= 0 {
		elemSize = 1
	}

	oldRawBytePtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	oldLen := l.nextReg(sema.TypeInt)
	oldCap := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrExtractValue{Dst: oldRawBytePtr, Agg: sliceVal, Index: 0})
	l.emit(&hir.InstrExtractValue{Dst: oldLen, Agg: sliceVal, Index: 1})
	l.emit(&hir.InstrExtractValue{Dst: oldCap, Agg: sliceVal, Index: 2})

	oldTypedPtr := l.nextReg(&sema.PointerType{Base: slType.Elem})
	l.emit(&hir.InstrCast{Dst: oldTypedPtr, Val: oldRawBytePtr, ToType: &sema.PointerType{Base: slType.Elem}})

	numElems := len(call.Args) - 1
	reqCap := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrBinary{Dst: reqCap, Op: hir.OpAdd, L: oldLen, R: &hir.ConstInt{Val: int64(numElems), Typ: sema.TypeInt}})

	growCond := l.nextReg(sema.TypeBool)
	l.emit(&hir.InstrBinary{Dst: growCond, Op: hir.OpGt, L: reqCap, R: oldCap})

	growBB := l.newBlock("append.grow")
	noGrowBB := l.newBlock("append.nogrow")
	storeBB := l.newBlock("append.store")

	finalPtrAlloca := l.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: slType.Elem}}, "finalPtr")
	finalCapAlloca := l.nextReg(&sema.PointerType{Base: sema.TypeInt}, "finalCap")
	l.emit(&hir.InstrAlloca{Dst: finalPtrAlloca, AllocType: &sema.PointerType{Base: slType.Elem}})
	l.emit(&hir.InstrAlloca{Dst: finalCapAlloca, AllocType: sema.TypeInt})

	l.terminate(&hir.InstrBranch{Cond: growCond, ThenTarget: growBB.Label, ElseTarget: noGrowBB.Label})

	l.setBlock(noGrowBB)
	l.emit(&hir.InstrStore{Val: oldTypedPtr, Ptr: finalPtrAlloca})
	l.emit(&hir.InstrStore{Val: oldCap, Ptr: finalCapAlloca})
	l.terminate(&hir.InstrJump{Target: storeBB.Label})

	l.setBlock(growBB)
	doubleCap := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrBinary{Dst: doubleCap, Op: hir.OpMul, L: oldCap, R: &hir.ConstInt{Val: 2, Typ: sema.TypeInt}})
	newCap := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrBinary{Dst: newCap, Op: hir.OpAdd, L: doubleCap, R: reqCap})
	newBytes := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrBinary{Dst: newBytes, Op: hir.OpMul, L: newCap, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})

	newRawPtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	l.emit(&hir.InstrHeapAlloc{Dst: newRawPtr, Size: newBytes, AllocType: sema.TypeByte})

	newTypedPtr := l.nextReg(&sema.PointerType{Base: slType.Elem})
	l.emit(&hir.InstrCast{Dst: newTypedPtr, Val: newRawPtr, ToType: &sema.PointerType{Base: slType.Elem}})

	oldBytes := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrBinary{Dst: oldBytes, Op: hir.OpMul, L: oldLen, R: &hir.ConstInt{Val: int64(elemSize), Typ: sema.TypeInt}})

	memcpyTmp := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	l.emit(&hir.InstrCallStatic{Dst: memcpyTmp, CalleeName: "memcpy", Args: []hir.Value{newRawPtr, oldRawBytePtr, oldBytes}})

	l.emit(&hir.InstrStore{Val: newTypedPtr, Ptr: finalPtrAlloca})
	l.emit(&hir.InstrStore{Val: newCap, Ptr: finalCapAlloca})
	l.terminate(&hir.InstrJump{Target: storeBB.Label})

	l.setBlock(storeBB)
	resPtr := l.nextReg(&sema.PointerType{Base: slType.Elem})
	resCap := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrLoad{Dst: resPtr, Ptr: finalPtrAlloca})
	l.emit(&hir.InstrLoad{Dst: resCap, Ptr: finalCapAlloca})

	for i := 0; i < numElems; i++ {
		elVal := l.lowerExpr(call.Args[1+i])
		offsetIdx := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrBinary{Dst: offsetIdx, Op: hir.OpAdd, L: oldLen, R: &hir.ConstInt{Val: int64(i), Typ: sema.TypeInt}})
		destPtr := l.nextReg(&sema.PointerType{Base: slType.Elem})
		l.emit(&hir.InstrGetElemPtr{Dst: destPtr, BasePtr: resPtr, Index: offsetIdx})
		l.emit(&hir.InstrStore{Val: elVal, Ptr: destPtr})
	}

	resBytePtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	l.emit(&hir.InstrCast{Dst: resBytePtr, Val: resPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})

	t1 := l.nextReg(slType)
	l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(slType), Val: resBytePtr, Index: 0})
	t2 := l.nextReg(slType)
	l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: reqCap, Index: 1})
	t3 := l.nextReg(slType)
	l.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: resCap, Index: 2})
	return t3
}

// -------------------------------------------------------------
// クロージャ (FuncLit) の Lowering
// -------------------------------------------------------------

func (l *Lowerer) lowerFuncLit(fl *ast.FuncLit) hir.Value {
	l.anonFuncCount++
	anonName := fmt.Sprintf("__anon_func_%d", l.anonFuncCount)

	ft := l.semaCtx.InferExprType(fl, nil).(*sema.FuncType)
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

	captures := sema.ScanCapturesFromLit(fl)

	prevFunc := l.curFunc
	prevBlock := l.curBlock
	prevSymbols := l.symbols
	prevTypes := l.symbolTypes
	l.curFunc = anonFn
	l.symbols = make(map[string]hir.Value)
	l.symbolTypes = make(map[string]sema.Type)

	anonEntry := &hir.BasicBlock{Label: "entry", Instructions: []hir.Instruction{}}
	l.setBlock(anonEntry)

	// 1. キャプチャ変数の展開 (環境ポインタからの復元)
	if len(captures) > 0 {
		envTyped := l.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		l.emit(&hir.InstrCast{Dst: envTyped, Val: envParamReg, ToType: &sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}})

		for idx, name := range captures {
			symType := prevTypes[name]
			slot := l.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
			l.emit(&hir.InstrGetElemPtr{Dst: slot, BasePtr: envTyped, Index: &hir.ConstInt{Val: int64(idx), Typ: sema.TypeInt}})
			rawPtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
			l.emit(&hir.InstrLoad{Dst: rawPtr, Ptr: slot})
			typedPtr := l.nextReg(&sema.PointerType{Base: symType})
			l.emit(&hir.InstrCast{Dst: typedPtr, Val: rawPtr, ToType: &sema.PointerType{Base: symType}})
			l.symbols[name] = typedPtr
			l.symbolTypes[name] = symType
		}
	}

	// 2. 仮引数の展開
	for _, p := range fl.Params {
		pType := l.semaCtx.ResolveType(p.Type)
		pReg := l.nextReg(pType, p.Name.Value+"_arg")
		anonFn.Params = append(anonFn.Params, pReg)
		allocaReg := l.nextReg(&sema.PointerType{Base: pType}, p.Name.Value)
		l.emit(&hir.InstrAlloca{Dst: allocaReg, AllocType: pType})
		l.emit(&hir.InstrStore{Val: pReg, Ptr: allocaReg})
		l.symbols[p.Name.Value] = allocaReg
		l.symbolTypes[p.Name.Value] = pType
	}

	for _, s := range fl.Body.Statements {
		l.lowerStmt(s)
	}
	if l.curBlock.Terminator == nil {
		l.terminate(&hir.InstrReturn{Vals: []hir.Value{}})
	}

	l.hirProg.Functions = append(l.hirProg.Functions, anonFn)

	l.curFunc = prevFunc
	l.curBlock = prevBlock
	l.symbols = prevSymbols
	l.symbolTypes = prevTypes

	// 3. 親関数側での環境構築 (ヒープ確保とポインタ格納)
	var envVal hir.Value = &hir.ConstNil{Typ: &sema.PointerType{Base: sema.TypeByte}}
	if len(captures) > 0 {
		envSize := len(captures) * 8
		envRaw := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
		l.emit(&hir.InstrHeapAlloc{Dst: envRaw, Size: &hir.ConstInt{Val: int64(envSize), Typ: sema.TypeInt}, AllocType: sema.TypeByte})

		envTyped := l.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
		l.emit(&hir.InstrCast{Dst: envTyped, Val: envRaw, ToType: &sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}}})

		for idx, name := range captures {
			symPtr := l.symbols[name]
			symRaw := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
			l.emit(&hir.InstrCast{Dst: symRaw, Val: symPtr, ToType: &sema.PointerType{Base: sema.TypeByte}})
			slot := l.nextReg(&sema.PointerType{Base: &sema.PointerType{Base: sema.TypeByte}})
			l.emit(&hir.InstrGetElemPtr{Dst: slot, BasePtr: envTyped, Index: &hir.ConstInt{Val: int64(idx), Typ: sema.TypeInt}})
			l.emit(&hir.InstrStore{Val: symRaw, Ptr: slot})
		}
		envVal = envRaw
	}

	fatType := ft
	t1 := l.nextReg(fatType)
	fnGlobal := &hir.GlobalVar{Name: anonName, Typ: &sema.PointerType{Base: sema.TypeByte}}
	l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(fatType), Val: fnGlobal, Index: 0})
	t2 := l.nextReg(fatType)
	l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: envVal, Index: 1})
	return t2
}

// -------------------------------------------------------------
// 構造体フィールド・メソッドの解決
// -------------------------------------------------------------

func (l *Lowerer) findStructByName(name string) (*sema.StructType, string) {
	if st, canonical := l.semaCtx.LookupStruct(name); st != nil {
		return st, canonical
	}
	return nil, ""
}

func (l *Lowerer) findStruct(t sema.Type) (*sema.StructType, string) {
	if t == nil {
		return nil, ""
	}
	name := strings.TrimPrefix(t.TypeName(), "*")
	return l.findStructByName(name)
}

func (l *Lowerer) resolveFieldPath(st *sema.StructType, sName string, curPtr hir.Value, fieldName string) (hir.Value, sema.Type, string, bool) {
	if st == nil {
		return nil, nil, "", false
	}
	for i, f := range st.Fields {
		if f.Name == fieldName {
			fieldPtr := l.nextReg(&sema.PointerType{Base: f.Type})
			l.emit(&hir.InstrGetFieldPtr{Dst: fieldPtr, BasePtr: curPtr, FieldIndex: i, FieldName: f.Name})
			return fieldPtr, f.Type, sName, true
		}
	}
	for i, f := range st.Fields {
		if f.IsEmbedded {
			embTypeName := strings.TrimPrefix(f.Type.TypeName(), "*")
			embSt, embStructName := l.findStructByName(embTypeName)
			if embSt != nil {
				gepReg := l.nextReg(&sema.PointerType{Base: f.Type})
				l.emit(&hir.InstrGetFieldPtr{Dst: gepReg, BasePtr: curPtr, FieldIndex: i, FieldName: f.Name})
				nextPtr := hir.Value(gepReg)
				if _, isPtr := f.Type.(*sema.PointerType); isPtr {
					loadReg := l.nextReg(f.Type)
					l.emit(&hir.InstrLoad{Dst: loadReg, Ptr: gepReg})
					nextPtr = loadReg
				}
				if finalGep, finalType, sNameFound, found := l.resolveFieldPath(embSt, embStructName, nextPtr, fieldName); found {
					return finalGep, finalType, sNameFound, true
				}
			}
		}
	}
	return nil, nil, "", false
}

func (l *Lowerer) resolveMethodPath(st *sema.StructType, sName string, curPtr hir.Value, methodName string) (string, *sema.FuncType, hir.Value, bool) {
	if st == nil {
		return "", nil, nil, false
	}
	directName := sName + "_" + methodName
	if fn, canonicalName := l.semaCtx.LookupFunction(directName); fn != nil {
		return canonicalName, fn, curPtr, true
	}
	for i, f := range st.Fields {
		if f.IsEmbedded {
			embTypeName := strings.TrimPrefix(f.Type.TypeName(), "*")
			embSt, embStructName := l.findStructByName(embTypeName)
			if embSt != nil {
				fieldPtr := l.nextReg(&sema.PointerType{Base: f.Type})
				if curPtr != nil {
					l.emit(&hir.InstrGetFieldPtr{Dst: fieldPtr, BasePtr: curPtr, FieldIndex: i, FieldName: f.Name})
				}
				nextPtr := hir.Value(fieldPtr)
				if _, isPtr := f.Type.(*sema.PointerType); isPtr {
					loadReg := l.nextReg(f.Type)
					if curPtr != nil {
						l.emit(&hir.InstrLoad{Dst: loadReg, Ptr: fieldPtr})
					}
					nextPtr = loadReg
				}
				if targetName, fnMeta, finalPtr, found := l.resolveMethodPath(embSt, embStructName, nextPtr, methodName); found {
					return targetName, fnMeta, finalPtr, true
				}
			}
		}
	}
	return "", nil, nil, false
}

func (l *Lowerer) getOrCreateItab(concreteType sema.Type, iface *sema.InterfaceType) *hir.ItabDef {
	sName := strings.TrimPrefix(concreteType.TypeName(), "*")
	ifName := iface.Name
	if ifName == "" {
		ifName = "anon_iface"
	}
	key := fmt.Sprintf("%s_%s", sName, ifName)
	if existing, ok := l.itabs[key]; ok {
		return existing
	}

	typeID := l.semaCtx.GetTypeID(concreteType)
	globalName := fmt.Sprintf("__itab_%s_%s", sName, ifName)
	itabStructName := fmt.Sprintf("__itab_%s", ifName)

	methods := []hir.ItabMethodEntry{}
	st, _ := l.findStruct(concreteType)

	for _, m := range iface.Methods {
		fnName := fmt.Sprintf("%s_%s", sName, m.Name)
		actualFn, canonical := l.semaCtx.LookupFunction(fnName)
		if actualFn == nil && st != nil {
			if promoTarget, _, _, found := l.resolveMethodPath(st, sName, nil, m.Name); found {
				fnName = promoTarget
			}
		} else if actualFn != nil {
			fnName = canonical
		}
		methods = append(methods, hir.ItabMethodEntry{
			MethodName:   m.Name,
			TargetFnName: fnName,
			MethodType:   m,
		})
	}

	def := &hir.ItabDef{
		GlobalName:     globalName,
		ConcreteType:   concreteType,
		InterfaceType:  iface,
		TypeID:         typeID,
		ItabStructName: itabStructName,
		Methods:        methods,
	}
	l.itabs[key] = def
	l.hirProg.Itabs = append(l.hirProg.Itabs, def)
	return def
}
