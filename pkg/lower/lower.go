package lower

import (
	"fmt"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

type loopContext struct {
	breakBlock    *hir.BasicBlock
	continueBlock *hir.BasicBlock
}

// Lowerer はIR変換全体を統括し、共通のコンパイル状態とサブローワーを保持する
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

	// 分割されたサブローワー
	Stmt *StmtLowerer
	Expr *ExprLowerer
	Call *CallLowerer
}

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
	}

	// 各サブローワーの初期化
	l.Stmt = NewStmtLowerer(l)
	l.Expr = NewExprLowerer(l)
	l.Call = NewCallLowerer(l)

	return l
}

// -----------------------------------------------------------------------------
// IR生成エントリポイント
// -----------------------------------------------------------------------------

func (l *Lowerer) Lower() *hir.Program {
	// グローバル変数の登録
	for name, typ := range l.semaCtx.Globals {
		l.hirProg.Globals = append(l.hirProg.Globals, &hir.GlobalVar{Name: name, Typ: typ})
	}

	// 外部 extern 関数の登録
	for _, fn := range l.semaCtx.Functions {
		if fn.IsExtern && !fn.IsCFunc {
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

	// トップレベル宣言の変換
	for _, decl := range l.prog.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok && fnDecl.Body != nil {
			if sema.IsGenericFuncDecl(fnDecl) {
				continue
			}
			l.Call.LowerFunc(fnDecl)
		} else if cfnDecl, ok := decl.(*ast.CFuncDecl); ok {
			l.Call.LowerCFunc(cfnDecl)
		}
	}

	return l.hirProg
}

// -----------------------------------------------------------------------------
// レジスタ・ブロック・命令出力ユーティリティ
// -----------------------------------------------------------------------------

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

// -----------------------------------------------------------------------------
// 型変換・デフォルト値ユーティリティ
// -----------------------------------------------------------------------------

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
	if _, isFunc := t.(*sema.FuncType); isFunc {
		return &hir.ConstZero{Typ: t}
	}
	if strings.HasPrefix(t.LLVMType(), "{") || strings.HasPrefix(t.LLVMType(), "[") || strings.HasPrefix(t.LLVMType(), "%struct.") {
		return &hir.ConstZero{Typ: t}
	}
	return &hir.ConstInt{Val: 0, Typ: t}
}

func (l *Lowerer) emitValueCoerce(val hir.Value, targetType sema.Type) hir.Value {
	if val.Type().LLVMType() == targetType.LLVMType() {
		return val
	}
	if iface, ok := targetType.(*sema.InterfaceType); ok {
		if isNilValue(val) {
			return l.defaultConstValue(iface)
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

// -----------------------------------------------------------------------------
// 構造体検索ユーティリティ
// -----------------------------------------------------------------------------

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
