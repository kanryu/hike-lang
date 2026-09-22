package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

// basicBlock is the temporary, lowering-only block representation used while
// translating AST statements. Structured HIR is assembled separately and
// backends derive their own CFG when needed.
type basicBlock struct {
	Label        string
	Instructions []hir.Instruction
	Terminator   hir.Terminator
}

type loopContext struct {
	breakBlock        *basicBlock
	continueBlock     *basicBlock
	structured        bool
	breakLabel        string
	continueLabel     string
	breakControlID    int
	continueControlID int
	breakTarget       int
	continueTarget    int
}

type structuredFrame struct {
	label string
}

// LowererはIR変換全体を統括し、共通のコンパイル状態とサブローワーを保持する
type Lowerer struct {
	prog             *ast.Program
	semaCtx          *sema.Context
	hirProg          *hir.Program
	curFunc          *hir.Function
	curBlock         *basicBlock
	legacyBlocks     []*basicBlock
	regCount         int
	blockCount       int
	anonFuncCount    int
	stringPool       map[string]*hir.ConstString
	symbols          map[string]hir.Value
	symbolTypes      map[string]sema.Type
	loopStack        []loopContext
	deferStack       []*ast.CallExpr
	structuredRoot   hir.ControlBody
	structuredStack  []*hir.ControlBody
	structuredFrames []structuredFrame
	itabs            map[string]*hir.ItabDef
	escapedVars      map[string]bool
	is32Bit          bool // Compilerから伝播される32bitターゲットフラグ
	recordLocations  bool
	regionMode       bool
	sourceFile       string
	sourceLoc        hir.SourceLocation
	module           string
	// globalInitRemaining is consumed while synthetic global initializer
	// statements are lowered at the beginning of main.
	globalInitRemaining int
	loweringGlobalInit  bool

	// 分割されたサブローワー
	Stmt *StmtLowerer
	Expr *ExprLowerer
	Call *CallLowerer
}

func heapAllocDst(a *hir.InstrHeapAlloc) *hir.Reg {
	return a.Dst
}

func heapAllocKeepOnHeap(a *hir.InstrHeapAlloc) bool {
	return a.KeepOnHeap
}

func setHeapAllocKeepOnHeap(a *hir.InstrHeapAlloc, keep bool) {
	a.KeepOnHeap = keep
}

func heapAllocSize(a *hir.InstrHeapAlloc) hir.Value {
	return a.Size
}

func heapAllocType(a *hir.InstrHeapAlloc) sema.Type {
	return a.AllocType
}

// SetRegionMode enables arena allocation for compiler-generated heap values.
func (l *Lowerer) SetRegionMode(enabled bool) { l.regionMode = enabled }

func (l *Lowerer) registerPanicSite() int {
	if l.curFunc == nil {
		return -1
	}
	id := len(l.curFunc.PanicSites)
	l.curFunc.PanicSites = append(l.curFunc.PanicSites, hir.PanicSite{
		ID:       id,
		Function: l.curFunc.Name,
		Location: l.sourceLoc,
	})
	return id
}

// SetRecordLocations controls optional source-location bookkeeping.
func (l *Lowerer) SetRecordLocations(enabled bool) { l.recordLocations = enabled }

func New(prog *ast.Program, semaCtx *sema.Context) *Lowerer {
	l := &Lowerer{
		prog:    prog,
		semaCtx: semaCtx,
		// Go-Hike's wasm32 runtime currently cannot safely hash interface keys.
		// Keep source locations disabled for the self-hosted path and avoid
		// allocating the interface-keyed map there.
		hirProg:          &hir.Program{ModuleName: astProgramPackage(prog)},
		module:           astProgramPackage(prog),
		stringPool:       make(map[string]*hir.ConstString),
		symbols:          make(map[string]hir.Value),
		symbolTypes:      make(map[string]sema.Type),
		loopStack:        []loopContext{},
		deferStack:       []*ast.CallExpr{},
		structuredRoot:   hir.ControlBody{},
		structuredStack:  []*hir.ControlBody{},
		structuredFrames: []structuredFrame{},
		itabs:            make(map[string]*hir.ItabDef),
		escapedVars:      make(map[string]bool),
		is32Bit:          false,
		recordLocations:  true,
	}

	// 各サブローワーの初期化
	l.Stmt = NewStmtLowerer(l)
	l.Expr = NewExprLowerer(l)
	l.Call = NewCallLowerer(l)

	return l
}

func astProgramPackage(prog *ast.Program) string { return prog.Package }

func (l *Lowerer) moduleName() string                   { return l.module }
func semaFuncExtern(fn *sema.FuncType) bool             { return fn.IsExtern }
func semaFuncCFunc(fn *sema.FuncType) bool              { return fn.IsCFunc }
func semaFuncName(fn *sema.FuncType) string             { return fn.Name }
func semaFuncIRName(fn *sema.FuncType) string           { return fn.IRName }
func semaFuncIsVariadic(fn *sema.FuncType) bool         { return fn.IsVariadic }
func semaFuncVariadicElem(fn *sema.FuncType) sema.Type  { return fn.VariadicElem }
func semaFuncCFuncAst(fn *sema.FuncType) *ast.CFuncDecl { return fn.CFuncAst }
func semaFuncCFuncTarget(fn *sema.FuncType) string      { return fn.CFuncTarget }
func semaTypeName(typ sema.Type) string {
	if typ == nil {
		return ""
	}
	switch t := typ.(type) {
	case *sema.BasicType:
		return t.Name
	case *sema.TypeParamType:
		return t.Name
	case *sema.ConstValueType:
		return fmt.Sprintf("const<%d>", t.Value)
	case *sema.PointerType:
		return "*" + semaTypeName(t.Base)
	case *sema.SliceType:
		return "[]" + semaTypeName(t.Elem)
	case *sema.ArrayType:
		return fmt.Sprintf("[%d]%s", t.Len, semaTypeName(t.Elem))
	case *sema.StructType:
		return t.Name
	case *sema.InterfaceType:
		if t.Name != "" {
			return t.Name
		}
		return "interface"
	case *sema.FuncType:
		return "func"
	case *sema.TupleType:
		return "tuple"
	case *sema.MapType:
		return "map[" + semaTypeName(t.Key) + "]" + semaTypeName(t.Value)
	case *sema.ChanType:
		return "chan " + semaTypeName(t.Elem)
	case *sema.FutureType:
		return "future"
	}
	return ""
}
func semaInterfaceName(iface *sema.InterfaceType) string { return iface.Name }
func astIDValue(id *ast.Identifier) string {
	if id == nil {
		return ""
	}
	return id.Value
}

// Set32Bitはターゲットが32bit (wasm32等) であるかを設定します
func (l *Lowerer) Set32Bit(is32 bool) {
	l.is32Bit = is32
}

// BuiltinNameはターゲットアーキテクチャ（32bit/64bit）に応じた組み込み関数名を解決します
func (l *Lowerer) BuiltinName(baseName string) string {
	if l.is32Bit {
		return baseName + "32"
	}
	return baseName
}

// -----------------------------------------------------------------------------
// IR生成エントリポイント
// -----------------------------------------------------------------------------

func (l *Lowerer) Lower() *hir.Program {
	// 1. グローバル変数の登録
	for name, typ := range l.semaCtx.Globals {
		l.hirProg.Globals = append(l.hirProg.Globals, &hir.GlobalVar{Name: name, Typ: typ})
	}

	// グローバル変数の初期化式（var x = expr）を代入文として収集
	var globalInits []ast.Statement
	for _, decl := range l.prog.Decls {
		if vd, ok := decl.(*ast.VarDecl); ok && vd.Value != nil {
			globalInits = append(globalInits, &ast.AssignStmt{
				Token: vd.Token,
				Left:  []ast.Expression{vd.Name},
				Right: []ast.Expression{vd.Value},
			})
		}
	}

	// 2. トップレベル宣言の変換
	for _, decl := range l.prog.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if sema.IsGenericFuncDecl(d) {
				continue
			}
			// main関数の先頭にグローバル変数の初期化文を差し込む
			if d.Name != nil && d.Name.Value == "main" && len(globalInits) > 0 && d.Body != nil {
				l.globalInitRemaining = len(globalInits)
				newStmts := make([]ast.Statement, 0, len(globalInits)+len(d.Body.Statements))
				newStmts = append(newStmts, globalInits...)
				newStmts = append(newStmts, d.Body.Statements...)
				d.Body.Statements = newStmts
				globalInits = nil
			}
			l.sourceFile = d.Filename
			l.Call.LowerFunc(d)
			l.finishRegionFunction()
			l.discardStructuredCFG()

		case *ast.CFuncDecl:
			l.Call.LowerCFunc(d)
			l.finishRegionFunction()
			l.discardStructuredCFG()

		case *ast.ExternFuncDecl:
			l.Call.LowerExternFunc(d)

		case *ast.JFuncDecl:
			// WASMモード用インラインJS関数
		}
	}

	// 3. ランタイム内部や標準ライブラリで登録された未定義の外部extern関数の補完登録
	definedNames := make(map[string]bool)
	for _, fn := range l.hirProg.Functions {
		definedNames[fn.Name] = true
	}

	for _, fn := range l.semaCtx.Functions {
		if semaFuncExtern(fn) && !semaFuncCFunc(fn) {
			irName := semaFuncName(fn)
			if semaFuncIRName(fn) != "" {
				irName = semaFuncIRName(fn)
			}
			if definedNames[irName] {
				continue
			}
			definedNames[irName] = true

			params := []*hir.Reg{}
			for i, pt := range fn.ParamTypes {
				params = append(params, &hir.Reg{ID: i + 1, Typ: pt})
			}
			l.hirProg.Functions = append(l.hirProg.Functions, &hir.Function{
				Name:        irName,
				Params:      params,
				ReturnTypes: fn.ReturnTypes,
				IsVariadic:  fn.IsVariadic,
				IsExtern:    true,
			})
		}
	}

	return l.hirProg
}

// finishRegionFunction performs the conservative first region inference pass.
// All compiler-generated allocations in one lexical function region are grouped
// together. Explicit malloc/free calls remain untouched, and future escape
// analysis can promote individual allocations back to the heap here.
func (l *Lowerer) finishRegionFunction() {
	if l.curFunc == nil || l.curFunc.IsExtern || len(l.legacyBlocks) == 0 {
		return
	}
	if !l.regionMode {
		return
	}
	region := l.nextReg(&sema.PointerType{Base: sema.TypeByte}, "region")
	first := l.legacyBlocks[0]
	regionBegin := &hir.InstrRegionBegin{Dst: region}
	first.Instructions = append([]hir.Instruction{regionBegin}, first.Instructions...)
	l.recordFunctionLocation(regionBegin, l.curFunc)
	for _, bb := range l.legacyBlocks {
		// A value returned from this function outlives its region. Promote the
		// allocation back to the ordinary heap before rewriting instructions.
		returned := make(map[*hir.Reg]bool)
		if ret, ok := bb.Terminator.(*hir.InstrReturn); ok {
			for _, val := range ret.Vals {
				if reg, ok := val.(*hir.Reg); ok {
					returned[reg] = true
				}
			}
		}
		for n, inst := range bb.Instructions {
			if a, ok := inst.(*hir.InstrHeapAlloc); ok && returned[heapAllocDst(a)] {
				setHeapAllocKeepOnHeap(a, true)
			}
			if a, ok := inst.(*hir.InstrHeapAlloc); ok && !heapAllocKeepOnHeap(a) {
				replacement := &hir.InstrRegionAlloc{Dst: heapAllocDst(a), Region: region, Size: heapAllocSize(a), AllocType: heapAllocType(a)}
				bb.Instructions[n] = replacement
				if loc, exists := l.hirProg.InstructionLocations[hir.InstructionKey(a)]; exists {
					l.hirProg.InstructionLocations[hir.InstructionKey(replacement)] = loc
				}
			}
		}
		if bb.Terminator != nil {
			if _, ok := bb.Terminator.(*hir.InstrReturn); ok {
				regionEnd := &hir.InstrRegionEnd{Region: region}
				bb.Instructions = append(bb.Instructions, regionEnd)
				l.recordFunctionLocation(regionEnd, l.curFunc)
			}
		}
	}
}

// discardStructuredCFG drops the lowerer's temporary CFG only after all
// function-level passes (notably region rewriting) have completed. Structured
// HIR remains the authoritative representation for migrated functions.
func (l *Lowerer) discardStructuredCFG() {
	l.legacyBlocks = nil
}

func (l *Lowerer) recordFunctionLocation(instr hir.Instruction, fn *hir.Function) {
	if l.recordLocations && instr != nil && fn != nil && fn.Location.Line > 0 {
		if l.hirProg.InstructionLocations == nil {
			l.hirProg.InstructionLocations = make(map[string]hir.SourceLocation)
		}
		l.hirProg.InstructionLocations[hir.InstructionKey(instr)] = fn.Location
	}
}

// -------------------------------------------------------------
// レジスタ・ブロック・命令出力ユーティリティ
// -------------------------------------------------------------

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

func (l *Lowerer) newBlock(labelPrefix string) *basicBlock {
	l.blockCount++
	return &basicBlock{
		Label:        fmt.Sprintf("%s.%d", labelPrefix, l.blockCount),
		Instructions: []hir.Instruction{},
	}
}

func (l *Lowerer) setBlock(bb *basicBlock) {
	l.curBlock = bb
	if l.curFunc != nil {
		l.legacyBlocks = append(l.legacyBlocks, bb)
	}
}

func (l *Lowerer) emit(instr hir.Instruction) {
	if l.curBlock != nil {
		l.curBlock.Instructions = append(l.curBlock.Instructions, instr)
		l.recordLocation(instr)
	}
	if len(l.structuredStack) > 0 {
		current := l.structuredDestination()
		*current = append(*current, &hir.InstructionNode{Instruction: instr})
	}
}

func (l *Lowerer) terminate(term hir.Terminator) {
	if l.curBlock != nil && l.curBlock.Terminator == nil {
		l.curBlock.Terminator = term
		l.recordLocation(term)
	}
	if len(l.structuredStack) > 0 {
		current := l.structuredDestination()
		switch t := term.(type) {
		case *hir.InstrReturn:
			*current = append(*current, &hir.ReturnNode{Values: t.Vals})
		case *hir.InstrUnreachable:
			*current = append(*current, &hir.UnreachableNode{})
		case *hir.InstrPanic:
			*current = append(*current, &hir.PanicNode{Value: t.Value, Cause: t.Cause, SiteID: t.SiteID})
		}
	}
}

func (l *Lowerer) resetStructuredState() {
	l.structuredRoot = hir.ControlBody{}
	l.structuredStack = []*hir.ControlBody{&l.structuredRoot}
	l.structuredFrames = []structuredFrame{}
}

func (l *Lowerer) initFunctionControl(fn *hir.Function) {
	if fn == nil {
		return
	}
	root := &hir.BlockNode{Label: fn.Name + ".control.root"}
	exit := &hir.BlockNode{
		Label:                   fn.Name + ".control.exit",
		FunctionExit:            true,
		ContinuationPlaceholder: true,
		ReplacedByID:            -1,
		ContinuationOf:          root,
	}
	l.registerControlElementAt(root, 0)
	fn.ControlExit = exit
	fn.ControlRoot = root
	l.registerControlElementAt(exit, 1)
	root.SetControlLinks(exit.Index(), exit.Index(), -1)
	root.Body = hir.ControlBody{exit}
	l.structuredRoot = hir.ControlBody{root}
	l.structuredStack = []*hir.ControlBody{&root.Body}
}

func (l *Lowerer) appendExistingStructuredNode(node hir.ControlNode) {
	if node == nil || len(l.structuredStack) == 0 {
		return
	}
	current := l.structuredStack[len(l.structuredStack)-1]
	*current = append(*current, node)
}

func (l *Lowerer) pushStructuredBody(body *hir.ControlBody) {
	l.structuredStack = append(l.structuredStack, body)
}

func (l *Lowerer) popStructuredBody() {
	if len(l.structuredStack) > 1 {
		l.structuredStack = l.structuredStack[:len(l.structuredStack)-1]
	}
}

func (l *Lowerer) pushStructuredFrame(label string) {
	l.structuredFrames = append(l.structuredFrames, structuredFrame{label: label})
}

func (l *Lowerer) popStructuredFrame() {
	if len(l.structuredFrames) > 0 {
		l.structuredFrames = l.structuredFrames[:len(l.structuredFrames)-1]
	}
}

func (l *Lowerer) structuredDepth(label string) (uint32, bool) {
	for i := len(l.structuredFrames) - 1; i >= 0; i-- {
		if l.structuredFrames[i].label == label {
			return uint32(len(l.structuredFrames) - 1 - i), true
		}
	}
	return 0, false
}

func (l *Lowerer) appendStructuredNode(node hir.ControlNode) {
	if node == nil || len(l.structuredStack) == 0 {
		return
	}
	current := l.structuredStack[len(l.structuredStack)-1]
	l.appendStructuredNodeTo(current, node)
}

func (l *Lowerer) appendStructuredNodeTo(body *hir.ControlBody, node hir.ControlNode) {
	if body == nil || node == nil {
		return
	}
	if element, ok := node.(hir.ControlElement); ok {
		l.registerControlElement(element)
	}
	if idx := lastContinuationIndex(*body); idx >= 0 {
		last, ok := (*body)[idx].(*hir.BlockNode)
		if ok && last.ContinuationPlaceholder && len(last.Body) == 0 {
			if predecessor := last.ContinuationOf; predecessor != nil {
				predecessor.SetControlLinks(controlNodeIndex(node), predecessor.BreakTarget(), predecessor.ContinueTarget())
			}
			if last.FunctionExit {
				*body = append(*body, nil)
				copy((*body)[idx+1:], (*body)[idx:])
				(*body)[idx] = node
				return
			}
			last.ReplacedByID = controlNodeIndex(node)
			(*body)[idx] = node
			return
		}
		if ok && last.ContinuationPlaceholder {
			last.Body = append(last.Body, node)
			return
		}
	}
	if len(*body) > 0 {
		if last, ok := (*body)[len(*body)-1].(*hir.BlockNode); ok && last.FunctionExit {
			if predecessor := last.ContinuationOf; predecessor != nil {
				predecessor.SetControlLinks(controlNodeIndex(node), predecessor.BreakTarget(), predecessor.ContinueTarget())
			}
			*body = append(*body, nil)
			copy((*body)[len(*body)-1:], (*body)[len(*body)-2:])
			(*body)[len(*body)-2] = node
			return
		}
	}
	*body = append(*body, node)
}

func (l *Lowerer) structuredDestination() *hir.ControlBody {
	current := l.structuredStack[len(l.structuredStack)-1]
	if idx := lastContinuationIndex(*current); idx >= 0 {
		if last, ok := (*current)[idx].(*hir.BlockNode); ok && last.ContinuationPlaceholder {
			return &last.Body
		}
	}
	if l.curFunc != nil && l.curFunc.ControlRoot != nil && len(*current) > 0 {
		if last, ok := (*current)[len(*current)-1].(*hir.BlockNode); ok && last.FunctionExit {
			continuation := l.appendContinuationAfter(current, l.curFunc.ControlRoot, len(l.structuredFrames))
			return &continuation.Body
		}
	}
	if len(*current) > 0 {
		if predecessor, ok := (*current)[len(*current)-1].(hir.ControlElement); ok {
			continuation := l.appendContinuationAfter(current, predecessor, len(l.structuredFrames))
			if continuation != nil {
				return &continuation.Body
			}
		}
	}
	if continuation := l.appendInstructionBlock(current, len(l.structuredFrames)); continuation != nil {
		return &continuation.Body
	}
	return current
}

func (l *Lowerer) appendInstructionBlock(body *hir.ControlBody, depth int) *hir.BlockNode {
	if body == nil {
		return nil
	}
	l.blockCount++
	block := &hir.BlockNode{
		Label:                   fmt.Sprintf("control.instructions.%d", l.blockCount),
		ContinuationPlaceholder: true,
		ReplacedByID:            -1,
	}
	l.registerControlElementAt(block, depth)
	if len(*body) > 0 {
		if last, ok := (*body)[len(*body)-1].(*hir.BlockNode); ok && last.FunctionExit {
			*body = append(*body, nil)
			copy((*body)[len(*body)-1:], (*body)[len(*body)-2:])
			(*body)[len(*body)-2] = block
			return block
		}
	}
	*body = append(*body, block)
	return block
}

func lastContinuationIndex(body hir.ControlBody) int {
	if len(body) == 0 {
		return -1
	}
	idx := len(body) - 1
	if last, ok := body[idx].(*hir.BlockNode); ok && last.FunctionExit {
		idx--
	}
	return idx
}

func (l *Lowerer) appendContinuationAfter(body *hir.ControlBody, predecessor hir.ControlElement, depth int) *hir.BlockNode {
	if body == nil || predecessor == nil {
		return nil
	}
	l.blockCount++
	placeholder := &hir.BlockNode{
		Label:                   fmt.Sprintf("control.next.%d", l.blockCount),
		ContinuationPlaceholder: true,
		ContinuationOf:          predecessor,
		ReplacedByID:            -1,
	}
	l.registerControlElementAt(placeholder, depth)
	predecessor.SetControlLinks(placeholder.Index(), predecessor.BreakTarget(), predecessor.ContinueTarget())
	if len(*body) > 0 {
		if last, ok := (*body)[len(*body)-1].(*hir.BlockNode); ok && last.FunctionExit {
			*body = append(*body, nil)
			copy((*body)[len(*body)-1:], (*body)[len(*body)-2:])
			(*body)[len(*body)-2] = placeholder
			return placeholder
		}
	}
	*body = append(*body, placeholder)
	return placeholder
}

func controlNodeIndex(node hir.ControlNode) int {
	if element, ok := node.(hir.ControlElement); ok {
		return element.Index()
	}
	return -1
}

func (l *Lowerer) registerControlElement(element hir.ControlElement) int {
	return l.registerControlElementAt(element, len(l.structuredFrames))
}

func (l *Lowerer) registerControlElementAt(element hir.ControlElement, depth int) int {
	if l.curFunc == nil {
		return -1
	}
	id := len(l.curFunc.ControlNodes)
	element.SetControlPosition(id, depth+1)
	l.curFunc.ControlNodes = append(l.curFunc.ControlNodes, element)
	if l.curFunc.ControlExit != nil && element != l.curFunc.ControlExit && element.Next() < 0 {
		element.SetControlLinks(l.curFunc.ControlExit.Index(), element.BreakTarget(), element.ContinueTarget())
	}
	return id
}

func (l *Lowerer) recordLocation(instr hir.Instruction) {
	if !l.recordLocations || instr == nil || l.sourceLoc.Line <= 0 {
		return
	}
	if l.hirProg.InstructionLocations == nil {
		l.hirProg.InstructionLocations = make(map[string]hir.SourceLocation)
	}
	l.hirProg.InstructionLocations[hir.InstructionKey(instr)] = l.sourceLoc
}

func (l *Lowerer) setSourceLocation(filename string, line, column int) func() {
	previous := l.sourceLoc
	if filename == "" {
		filename = previous.Filename
	}
	l.sourceLoc = hir.SourceLocation{Filename: filename, Line: line, Column: column}
	return func() { l.sourceLoc = previous }
}

func (l *Lowerer) setTokenLocation(filename string, tok token.Token) func() {
	return l.setSourceLocation(filename, tok.Line, tok.Col)
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

// stringParts exposes the two fields of the length-aware string value to
// operations that still use the NUL-terminated C runtime ABI.
func (l *Lowerer) stringParts(value hir.Value) (hir.Value, hir.Value) {
	base, offset, length32 := l.stringViewParts(value)
	ptr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	l.emit(&hir.InstrGetElemPtr{Dst: ptr, BasePtr: base, Index: offset})
	length := hir.Value(length32)
	if sema.LLVMTypeOf(sema.TypeInt) != sema.LLVMTypeOf(sema.TypeInt32) {
		length64 := l.nextReg(sema.TypeInt)
		l.emit(&hir.InstrCast{Dst: length64, Val: length32, ToType: sema.TypeInt})
		length = length64
	}
	return ptr, length
}

// stringViewParts returns the backing payload pointer, byte offset, and the
// view length.  The first field always points at the payload (the two header
// words are immediately before it), even when the string itself is a slice.
func (l *Lowerer) stringViewParts(value hir.Value) (hir.Value, hir.Value, hir.Value) {
	base := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	offset := l.nextReg(sema.TypeInt32)
	length := l.nextReg(sema.TypeInt32)
	l.emit(&hir.InstrExtractValue{Dst: base, Agg: value, Index: 0})
	l.emit(&hir.InstrExtractValue{Dst: offset, Agg: value, Index: 1})
	l.emit(&hir.InstrExtractValue{Dst: length, Agg: value, Index: 2})
	return base, offset, length
}

func (l *Lowerer) makeString(ptr, length hir.Value) hir.Value {
	return l.makeStringView(ptr, &hir.ConstInt{Val: 0, Typ: sema.TypeInt32}, length)
}

func (l *Lowerer) makeStringView(base, offset, length hir.Value) hir.Value {
	t1 := l.nextReg(sema.TypeString)
	l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(sema.TypeString), Val: base, Index: 0})
	t2 := l.nextReg(sema.TypeString)
	if sema.LLVMTypeOf(offset.Type()) != sema.LLVMTypeOf(sema.TypeInt32) {
		offset32 := l.nextReg(sema.TypeInt32)
		l.emit(&hir.InstrCast{Dst: offset32, Val: offset, ToType: sema.TypeInt32})
		offset = offset32
	}
	l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: offset, Index: 1})
	var length32Val hir.Value
	if sema.LLVMTypeOf(length.Type()) != sema.LLVMTypeOf(sema.TypeInt32) {
		length32 := l.nextReg(sema.TypeInt32)
		l.emit(&hir.InstrCast{Dst: length32, Val: length, ToType: sema.TypeInt32})
		length32Val = length32
	} else {
		length32Val = length
	}
	t3 := l.nextReg(sema.TypeString)
	l.emit(&hir.InstrInsertValue{Dst: t3, Agg: t2, Val: length32Val, Index: 2})
	return t3
}

func (l *Lowerer) stringBase(value hir.Value) hir.Value {
	base := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	l.emit(&hir.InstrExtractValue{Dst: base, Agg: value, Index: 0})
	return base
}

func (l *Lowerer) retainString(value hir.Value) {
	base := l.stringBase(value)
	l.emit(&hir.InstrCallStatic{CalleeName: l.BuiltinName("__hike_string_retain"), Args: []hir.Value{base}})
}

func (l *Lowerer) releaseString(value hir.Value) {
	base := l.stringBase(value)
	l.emit(&hir.InstrCallStatic{CalleeName: l.BuiltinName("__hike_string_release"), Args: []hir.Value{base}})
}

func (l *Lowerer) isStringType(t sema.Type) bool {
	return t == sema.TypeString || semaTypeName(t) == "string"
}

// -------------------------------------------------------------
// 型変換・デフォルト値ユーティリティ
// -------------------------------------------------------------

func (l *Lowerer) defaultConstValue(t sema.Type) hir.Value {
	if t == nil {
		return &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
	}
	if t == sema.TypeBool {
		return &hir.ConstBool{Val: false, Typ: t}
	}
	if t == sema.TypeFloat64 || t == sema.TypeFloat32 {
		return &hir.ConstFloat{Val: 0.0, Typ: t}
	}
	if t == sema.TypeString || semaTypeName(t) == "string" {
		return l.getStringConst("")
	}
	if t == sema.TypeCString || semaTypeName(t) == "cstring" {
		return &hir.ConstNil{Typ: t}
	}
	if strings.HasSuffix(sema.LLVMTypeOf(t), "*") {
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
	if _, isFunc := t.(*sema.FuncType); isFunc {
		return &hir.ConstZero{Typ: t}
	}
	if strings.HasPrefix(sema.LLVMTypeOf(t), "{") || strings.HasPrefix(sema.LLVMTypeOf(t), "[") || strings.HasPrefix(sema.LLVMTypeOf(t), "%struct.") {
		return &hir.ConstZero{Typ: t}
	}
	return &hir.ConstInt{Val: 0, Typ: t}
}

func (l *Lowerer) emitValueCoerce(val hir.Value, targetType sema.Type) hir.Value {
	if val == nil || targetType == nil {
		return val
	}
	if reg, isReg := val.(*hir.Reg); isReg && reg == nil {
		panic(fmt.Sprintf("[Lower Error] cannot coerce a nil register to %s", semaTypeName(targetType)))
	}
	if val.Type() == targetType || semaTypeName(val.Type()) == semaTypeName(targetType) {
		return val
	}
	if targetType == sema.TypeString {
		if _, isPtr := val.Type().(*sema.PointerType); isPtr {
			return l.Call.lowerCStringToString(val)
		}
		if val.Type() == sema.TypeCString {
			return l.Call.lowerCStringToString(val)
		}
	}

	// string -> cstring
	if (val.Type() == sema.TypeString || semaTypeName(val.Type()) == "string") && targetType == sema.TypeCString {
		return l.Call.lowerStringToCString(val)
	}

	// cstring -> string
	if (val.Type() == sema.TypeCString || semaTypeName(val.Type()) == "cstring") && targetType == sema.TypeString {
		return l.Call.lowerCStringToString(val)
	}

	if sema.LLVMTypeOf(val.Type()) == sema.LLVMTypeOf(targetType) {
		return val
	}

	if iface, ok := targetType.(*sema.InterfaceType); ok {
		if isNilValue(val) {
			return l.defaultConstValue(iface)
		}

		// 1. 既にインターフェース型である値の変換
		if srcIface, isSrcIface := val.Type().(*sema.InterfaceType); isSrcIface {
			if iface.IsAny() {
				if srcIface.IsAny() {
					return val
				}
				// An interface value carries the dynamic data pointer and an itab.
				// Re-boxing it into any must recover the concrete TypeID from the
				// itab; the static interface TypeID is not the dynamic type.
				data := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
				itab := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
				l.emit(&hir.InstrExtractValue{Dst: data, Agg: val, Index: 0})
				l.emit(&hir.InstrExtractValue{Dst: itab, Agg: val, Index: 1})
				typeIDPtr := l.nextReg(&sema.PointerType{Base: sema.TypeInt32})
				l.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itab, ToType: typeIDPtr.Typ})
				typeID := l.nextReg(sema.TypeInt32)
				l.emit(&hir.InstrLoad{Dst: typeID, Ptr: typeIDPtr})
				t1 := l.nextReg(iface)
				l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(iface), Val: typeID, Index: 0})
				dst := l.nextReg(iface)
				l.emit(&hir.InstrInsertValue{Dst: dst, Agg: t1, Val: data, Index: 1})
				return dst
			}
			if semaTypeName(srcIface) == semaTypeName(iface) {
				return val
			}
		}

		itabName := ""
		typeID := int64(0)
		if !iface.IsAny() {
			itabDef := l.Call.GetOrCreateItab(val.Type(), iface)
			itabName = itabDef.GlobalName
		} else {
			typeID = val.Type().TypeID(l.semaCtx)
		}
		dst := l.nextReg(iface)
		l.emit(&hir.InstrBoxInterface{Dst: dst, Val: val, Iface: iface, ItabName: itabName, TypeID: typeID})
		return dst
	}
	if _, isFunc := targetType.(*sema.FuncType); isFunc && isNilValue(val) {
		return l.defaultConstValue(targetType)
	}
	dst := l.nextReg(targetType)
	l.emit(&hir.InstrCast{Dst: dst, Val: val, ToType: targetType})
	return dst
}

func (l *Lowerer) coerceToI64(v hir.Value, fromType sema.Type) hir.Value {
	if fromType == sema.TypeString || semaTypeName(fromType) == "string" {
		ptr, _ := l.stringParts(v)
		fromType = ptr.Type()
		v = ptr
	}
	if sema.LLVMTypeOf(fromType) == sema.LLVMTypeOf(sema.TypeInt) {
		return v
	}
	dst := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrCast{Dst: dst, Val: v, ToType: sema.TypeInt})
	return dst
}

func (l *Lowerer) coerceFromI64(v hir.Value, toType sema.Type) hir.Value {
	if toType == sema.TypeString || semaTypeName(toType) == "string" {
		if v != nil && v.Type() != nil && sema.LLVMTypeOf(v.Type()) == "i8*" {
			return l.Call.lowerCStringToString(v)
		}
		ptr := l.nextReg(sema.TypeCString)
		l.emit(&hir.InstrCast{Dst: ptr, Val: v, ToType: sema.TypeCString})
		return l.Call.lowerCStringToString(ptr)
	}
	if sema.LLVMTypeOf(toType) == sema.LLVMTypeOf(sema.TypeInt) {
		return v
	}
	dst := l.nextReg(toType)
	l.emit(&hir.InstrCast{Dst: dst, Val: v, ToType: toType})
	return dst
}

func (l *Lowerer) coerceToInt(v hir.Value, fromType sema.Type) hir.Value {
	return l.coerceToI64(v, fromType)
}

func (l *Lowerer) coerceFromInt(v hir.Value, toType sema.Type) hir.Value {
	return l.coerceFromI64(v, toType)
}

func isNilValue(v hir.Value) bool {
	if v == nil {
		return true
	}
	if r, ok := v.(*hir.Reg); ok && (r == nil || r.Typ == nil) {
		return true
	}
	if _, ok := v.(*hir.ConstNil); ok {
		return true
	}
	if cz, ok := v.(*hir.ConstZero); ok && strings.HasSuffix(sema.LLVMTypeOf(cz.Typ), "*") {
		return true
	}
	return v.String() == "nil" || v.String() == "null"
}

// -------------------------------------------------------------
// 構造体検索ユーティリティ
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
	name := strings.TrimPrefix(semaTypeName(t), "*")
	return l.findStructByName(name)
}
