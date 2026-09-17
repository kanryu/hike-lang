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

// LowererはIR変換全体を統括し、共通のコンパイル状態とサブローワーを保持する
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
	escapedVars   map[string]bool
	is32Bit       bool // Compilerから伝播される32bitターゲットフラグ
	regionMode    bool

	// 分割されたサブローワー
	Stmt *StmtLowerer
	Expr *ExprLowerer
	Call *CallLowerer
}

// SetRegionMode enables arena allocation for compiler-generated heap values.
func (l *Lowerer) SetRegionMode(enabled bool) { l.regionMode = enabled }

func New(prog *ast.Program, semaCtx *sema.Context) *Lowerer {
	l := &Lowerer{
		prog:        prog,
		semaCtx:     semaCtx,
		hirProg:     &hir.Program{ModuleName: prog.Package},
		stringPool:  make(map[string]*hir.ConstString),
		symbols:     make(map[string]hir.Value),
		symbolTypes: make(map[string]sema.Type),
		loopStack:   []loopContext{},
		deferStack:  []*ast.CallExpr{},
		itabs:       make(map[string]*hir.ItabDef),
		escapedVars: make(map[string]bool),
		is32Bit:     false,
	}

	// 各サブローワーの初期化
	l.Stmt = NewStmtLowerer(l)
	l.Expr = NewExprLowerer(l)
	l.Call = NewCallLowerer(l)

	return l
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
				Token: token.Token{Type: token.ASSIGN, Literal: "="},
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
				newStmts := make([]ast.Statement, 0, len(globalInits)+len(d.Body.Statements))
				newStmts = append(newStmts, globalInits...)
				newStmts = append(newStmts, d.Body.Statements...)
				d.Body.Statements = newStmts
				globalInits = nil
			}
			l.Call.LowerFunc(d)
			l.finishRegionFunction()

		case *ast.CFuncDecl:
			l.Call.LowerCFunc(d)
			l.finishRegionFunction()

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
		if fn.IsExtern && !fn.IsCFunc {
			irName := fn.Name
			if fn.IRName != "" {
				irName = fn.IRName
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
				Blocks:      nil,
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
	if !l.regionMode || l.curFunc == nil || l.curFunc.IsExtern || len(l.curFunc.Blocks) == 0 {
		return
	}
	region := l.nextReg(&sema.PointerType{Base: sema.TypeByte}, "region")
	first := l.curFunc.Blocks[0]
	first.Instructions = append([]hir.Instruction{&hir.InstrRegionBegin{Dst: region}}, first.Instructions...)
	for _, bb := range l.curFunc.Blocks {
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
			if a, ok := inst.(*hir.InstrHeapAlloc); ok && returned[a.Dst] {
				a.KeepOnHeap = true
			}
			if a, ok := inst.(*hir.InstrHeapAlloc); ok && !a.KeepOnHeap {
				bb.Instructions[n] = &hir.InstrRegionAlloc{Dst: a.Dst, Region: region, Size: a.Size, AllocType: a.AllocType}
			}
		}
		if bb.Terminator != nil {
			if _, ok := bb.Terminator.(*hir.InstrReturn); ok {
				bb.Instructions = append(bb.Instructions, &hir.InstrRegionEnd{Region: region})
			}
		}
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

// stringParts exposes the two fields of the length-aware string value to
// operations that still use the NUL-terminated C runtime ABI.
func (l *Lowerer) stringParts(value hir.Value) (hir.Value, hir.Value) {
	base, offset, length32 := l.stringViewParts(value)
	ptr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
	l.emit(&hir.InstrGetElemPtr{Dst: ptr, BasePtr: base, Index: offset})
	length := hir.Value(length32)
	if sema.TypeInt.LLVMType() != sema.TypeInt32.LLVMType() {
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
	if offset.Type().LLVMType() != sema.TypeInt32.LLVMType() {
		offset32 := l.nextReg(sema.TypeInt32)
		l.emit(&hir.InstrCast{Dst: offset32, Val: offset, ToType: sema.TypeInt32})
		offset = offset32
	}
	l.emit(&hir.InstrInsertValue{Dst: t2, Agg: t1, Val: offset, Index: 1})
	var length32Val hir.Value
	if length.Type().LLVMType() != sema.TypeInt32.LLVMType() {
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
	return t == sema.TypeString || (t != nil && t.TypeName() == "string")
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
	if t == sema.TypeString || t.TypeName() == "string" {
		return l.getStringConst("")
	}
	if t == sema.TypeCString || t.TypeName() == "cstring" {
		return &hir.ConstNil{Typ: t}
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
	if _, isFunc := t.(*sema.FuncType); isFunc {
		return &hir.ConstZero{Typ: t}
	}
	if strings.HasPrefix(t.LLVMType(), "{") || strings.HasPrefix(t.LLVMType(), "[") || strings.HasPrefix(t.LLVMType(), "%struct.") {
		return &hir.ConstZero{Typ: t}
	}
	return &hir.ConstInt{Val: 0, Typ: t}
}

func (l *Lowerer) emitValueCoerce(val hir.Value, targetType sema.Type) hir.Value {
	if val == nil || targetType == nil {
		return val
	}
	if val.Type() == targetType || val.Type().TypeName() == targetType.TypeName() {
		return val
	}

	// string -> cstring
	if (val.Type() == sema.TypeString || val.Type().TypeName() == "string") && targetType == sema.TypeCString {
		return l.Call.lowerStringToCString(val)
	}

	// cstring -> string
	if (val.Type() == sema.TypeCString || val.Type().TypeName() == "cstring") && targetType == sema.TypeString {
		return l.Call.lowerCStringToString(val)
	}

	if val.Type().LLVMType() == targetType.LLVMType() {
		return val
	}

	if iface, ok := targetType.(*sema.InterfaceType); ok {
		if isNilValue(val) {
			return l.defaultConstValue(iface)
		}

		// 1. 既にインターフェース型である値の変換
		if srcIface, isSrcIface := val.Type().(*sema.InterfaceType); isSrcIface {
			if iface.IsAny() {
				dataPtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
				l.emit(&hir.InstrExtractValue{Dst: dataPtr, Agg: val, Index: 0})
				typeIDReg := l.nextReg(sema.TypeInt)
				if !srcIface.IsAny() {
					itabPtr := l.nextReg(&sema.PointerType{Base: sema.TypeByte})
					l.emit(&hir.InstrExtractValue{Dst: itabPtr, Agg: val, Index: 1})
					typeIDPtr := l.nextReg(&sema.PointerType{Base: sema.TypeInt})
					l.emit(&hir.InstrCast{Dst: typeIDPtr, Val: itabPtr, ToType: &sema.PointerType{Base: sema.TypeInt}})
					l.emit(&hir.InstrLoad{Dst: typeIDReg, Ptr: typeIDPtr})
				} else {
					l.emit(&hir.InstrExtractValue{Dst: typeIDReg, Agg: val, Index: 1})
				}

				t1 := l.nextReg(iface)
				l.emit(&hir.InstrInsertValue{Dst: t1, Agg: l.defaultConstValue(iface), Val: dataPtr, Index: 0})
				dst := l.nextReg(iface)
				l.emit(&hir.InstrInsertValue{Dst: dst, Agg: t1, Val: typeIDReg, Index: 1})
				return dst
			}
			if srcIface.TypeName() == iface.TypeName() {
				return val
			}
		}

		itabName := ""
		if !iface.IsAny() {
			itabDef := l.Call.GetOrCreateItab(val.Type(), iface)
			itabName = itabDef.GlobalName
		}
		dst := l.nextReg(iface)
		l.emit(&hir.InstrBoxInterface{Dst: dst, Val: val, Iface: iface, ItabName: itabName})
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
	if fromType == sema.TypeString || (fromType != nil && fromType.TypeName() == "string") {
		ptr, _ := l.stringParts(v)
		fromType = ptr.Type()
		v = ptr
	}
	if fromType.LLVMType() == sema.TypeInt.LLVMType() {
		return v
	}
	dst := l.nextReg(sema.TypeInt)
	l.emit(&hir.InstrCast{Dst: dst, Val: v, ToType: sema.TypeInt})
	return dst
}

func (l *Lowerer) coerceFromI64(v hir.Value, toType sema.Type) hir.Value {
	if toType == sema.TypeString || (toType != nil && toType.TypeName() == "string") {
		ptr := l.nextReg(sema.TypeCString)
		l.emit(&hir.InstrCast{Dst: ptr, Val: v, ToType: sema.TypeCString})
		return l.Call.lowerCStringToString(ptr)
	}
	if toType.LLVMType() == sema.TypeInt.LLVMType() {
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
	if _, ok := v.(*hir.ConstNil); ok {
		return true
	}
	if cz, ok := v.(*hir.ConstZero); ok && strings.HasSuffix(cz.Typ.LLVMType(), "*") {
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
	name := strings.TrimPrefix(t.TypeName(), "*")
	return l.findStructByName(name)
}
