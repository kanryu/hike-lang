package hir

import (
	"fmt"
	"strings"

	"hikec-go/pkg/sema"
)

type Value interface {
	Type() sema.Type
	String() string
}

type Reg struct {
	ID   int
	Typ  sema.Type
	Name string
}

func (r *Reg) Type() sema.Type { return r.Typ }
func (r *Reg) String() string {
	if r.Name != "" {
		return fmt.Sprintf("%%%s", r.Name)
	}
	return fmt.Sprintf("%%v%d", r.ID)
}

type ConstZero struct {
	Typ sema.Type
}

func (c *ConstZero) Type() sema.Type { return c.Typ }
func (c *ConstZero) String() string  { return "zeroinitializer" }

type ConstInt struct {
	Val int64
	Typ sema.Type
}

func (c *ConstInt) Type() sema.Type { return c.Typ }
func (c *ConstInt) String() string  { return fmt.Sprintf("%d", c.Val) }

type ConstFloat struct {
	Val float64
	Typ sema.Type
}

func (c *ConstFloat) Type() sema.Type { return c.Typ }
func (c *ConstFloat) String() string {
	s := fmt.Sprintf("%f", c.Val)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

type ConstBool struct {
	Val bool
	Typ sema.Type
}

func (c *ConstBool) Type() sema.Type { return c.Typ }
func (c *ConstBool) String() string  { return fmt.Sprintf("%t", c.Val) }

type ConstString struct {
	Label  string
	Raw    string
	Length int
	Typ    sema.Type
}

func (c *ConstString) Type() sema.Type { return c.Typ }
func (c *ConstString) String() string  { return fmt.Sprintf("@%s", c.Label) }

type ConstNil struct {
	Typ sema.Type
}

func (c *ConstNil) Type() sema.Type { return c.Typ }
func (c *ConstNil) String() string  { return "nil" }

type GlobalVar struct {
	Name string
	Typ  sema.Type
}

func (g *GlobalVar) Type() sema.Type { return g.Typ }
func (g *GlobalVar) String() string  { return fmt.Sprintf("@%s", g.Name) }

type Opcode int

const (
	OpAdd Opcode = iota
	OpSub
	OpMul
	OpDiv
	OpRem
	OpAnd
	OpOr
	OpXor
	OpShl
	OpShr
	OpEq
	OpNeq
	OpLt
	OpLe
	OpGt
	OpGe
	OpNeg
	OpNot
)

func (op Opcode) String() string {
	switch op {
	case OpAdd:
		return "add"
	case OpSub:
		return "sub"
	case OpMul:
		return "mul"
	case OpDiv:
		return "div"
	case OpRem:
		return "rem"
	case OpAnd:
		return "and"
	case OpOr:
		return "or"
	case OpXor:
		return "xor"
	case OpShl:
		return "shl"
	case OpShr:
		return "shr"
	case OpEq:
		return "eq"
	case OpNeq:
		return "neq"
	case OpLt:
		return "lt"
	case OpLe:
		return "le"
	case OpGt:
		return "gt"
	case OpGe:
		return "ge"
	case OpNeg:
		return "neg"
	case OpNot:
		return "not"
	default:
		return "unknown_op"
	}
}

type Instruction interface {
	String() string
	Result() *Reg
}

type InstrAlloca struct {
	Dst       *Reg
	AllocType sema.Type
}

func (i *InstrAlloca) Result() *Reg { return i.Dst }
func (i *InstrAlloca) String() string {
	return fmt.Sprintf("  %s = alloca %s", i.Dst, i.AllocType.TypeName())
}

type InstrAllocaDynamic struct {
	Dst       *Reg
	Size      Value
	AllocType sema.Type
}

func (i *InstrAllocaDynamic) Result() *Reg { return i.Dst }
func (i *InstrAllocaDynamic) String() string {
	return fmt.Sprintf("  %s = alloca_dyn %s, %s", i.Dst, i.AllocType.TypeName(), i.Size)
}

type InstrHeapAlloc struct {
	Dst       *Reg
	Size      Value
	AllocType sema.Type
}

func (i *InstrHeapAlloc) Result() *Reg { return i.Dst }
func (i *InstrHeapAlloc) String() string {
	return fmt.Sprintf("  %s = heapalloc %s, %s", i.Dst, i.AllocType.TypeName(), i.Size)
}

type InstrLoad struct {
	Dst *Reg
	Ptr Value
}

func (i *InstrLoad) Result() *Reg { return i.Dst }
func (i *InstrLoad) String() string {
	return fmt.Sprintf("  %s = load %s, %s", i.Dst, i.Dst.Typ.TypeName(), i.Ptr)
}

type InstrStore struct {
	Val Value
	Ptr Value
}

func (i *InstrStore) Result() *Reg { return nil }
func (i *InstrStore) String() string {
	return fmt.Sprintf("  store %s %s, %s", i.Val.Type().TypeName(), i.Val, i.Ptr)
}

type InstrBinary struct {
	Dst *Reg
	Op  Opcode
	L   Value
	R   Value
}

func (i *InstrBinary) Result() *Reg { return i.Dst }
func (i *InstrBinary) String() string {
	return fmt.Sprintf("  %s = %s %s, %s", i.Dst, i.Op, i.L, i.R)
}

type InstrUnary struct {
	Dst *Reg
	Op  Opcode
	Val Value
}

func (i *InstrUnary) Result() *Reg { return i.Dst }
func (i *InstrUnary) String() string {
	return fmt.Sprintf("  %s = %s %s", i.Dst, i.Op, i.Val)
}

type InstrCast struct {
	Dst    *Reg
	Val    Value
	ToType sema.Type
}

func (i *InstrCast) Result() *Reg { return i.Dst }
func (i *InstrCast) String() string {
	return fmt.Sprintf("  %s = cast %s %s to %s", i.Dst, i.Val.Type().TypeName(), i.Val, i.ToType.TypeName())
}

type InstrBoxInterface struct {
	Dst      *Reg
	Val      Value
	Iface    *sema.InterfaceType
	ItabName string
}

func (i *InstrBoxInterface) Result() *Reg { return i.Dst }
func (i *InstrBoxInterface) String() string {
	return fmt.Sprintf("  %s = box_iface %s %s to %s (itab: %s)",
		i.Dst, i.Val.Type().TypeName(), i.Val, i.Iface.TypeName(), i.ItabName)
}

type InstrGetFieldPtr struct {
	Dst        *Reg
	BasePtr    Value
	FieldIndex int
	FieldName  string
}

func (i *InstrGetFieldPtr) Result() *Reg { return i.Dst }
func (i *InstrGetFieldPtr) String() string {
	return fmt.Sprintf("  %s = getfieldptr %s, %d ; .%s", i.Dst, i.BasePtr, i.FieldIndex, i.FieldName)
}

type InstrGetElemPtr struct {
	Dst     *Reg
	BasePtr Value
	Index   Value
}

func (i *InstrGetElemPtr) Result() *Reg { return i.Dst }
func (i *InstrGetElemPtr) String() string {
	return fmt.Sprintf("  %s = getelemptr %s, %s", i.Dst, i.BasePtr, i.Index)
}

type InstrCallStatic struct {
	Dst        *Reg
	CalleeName string
	Args       []Value
}

func (i *InstrCallStatic) Result() *Reg { return i.Dst }
func (i *InstrCallStatic) String() string {
	args := make([]string, len(i.Args))
	for idx, a := range i.Args {
		args[idx] = fmt.Sprintf("%s %s", a.Type().TypeName(), a)
	}
	if i.Dst != nil {
		return fmt.Sprintf("  %s = call @%s(%s)", i.Dst, i.CalleeName, strings.Join(args, ", "))
	}
	return fmt.Sprintf("  call @%s(%s)", i.CalleeName, strings.Join(args, ", "))
}

type InstrCallIndirect struct {
	Dst    *Reg
	FnPtr  Value
	EnvPtr Value
	Args   []Value
}

func (i *InstrCallIndirect) Result() *Reg { return i.Dst }
func (i *InstrCallIndirect) String() string {
	args := make([]string, len(i.Args))
	for idx, a := range i.Args {
		args[idx] = fmt.Sprintf("%s %s", a.Type().TypeName(), a)
	}
	envStr := "nil"
	if i.EnvPtr != nil {
		envStr = i.EnvPtr.String()
	}
	if i.Dst != nil {
		return fmt.Sprintf("  %s = call_indirect %s, env: %s, (%s)", i.Dst, i.FnPtr, envStr, strings.Join(args, ", "))
	}
	return fmt.Sprintf("  call_indirect %s, env: %s, (%s)", i.FnPtr, envStr, strings.Join(args, ", "))
}

type InstrCallIface struct {
	Dst         *Reg
	IfaceVal    Value
	MethodIndex int
	MethodName  string
	Args        []Value
}

func (i *InstrCallIface) Result() *Reg { return i.Dst }
func (i *InstrCallIface) String() string {
	args := make([]string, len(i.Args))
	for idx, a := range i.Args {
		args[idx] = fmt.Sprintf("%s %s", a.Type().TypeName(), a)
	}
	if i.Dst != nil {
		return fmt.Sprintf("  %s = call_iface %s.#%d (%s), (%s)", i.Dst, i.IfaceVal, i.MethodIndex, i.MethodName, strings.Join(args, ", "))
	}
	return fmt.Sprintf("  call_iface %s.#%d (%s), (%s)", i.IfaceVal, i.MethodIndex, i.MethodName, strings.Join(args, ", "))
}

type InstrExtractValue struct {
	Dst   *Reg
	Agg   Value
	Index int
}

func (i *InstrExtractValue) Result() *Reg { return i.Dst }
func (i *InstrExtractValue) String() string {
	return fmt.Sprintf("  %s = extractvalue %s, %d", i.Dst, i.Agg, i.Index)
}

type InstrInsertValue struct {
	Dst   *Reg
	Agg   Value
	Val   Value
	Index int
}

func (i *InstrInsertValue) Result() *Reg { return i.Dst }
func (i *InstrInsertValue) String() string {
	return fmt.Sprintf("  %s = insertvalue %s, %s, %d", i.Dst, i.Agg, i.Val, i.Index)
}

type Terminator interface {
	Instruction
	Successors() []string
}

type InstrJump struct {
	Target string
}

func (i *InstrJump) Result() *Reg         { return nil }
func (i *InstrJump) Successors() []string { return []string{i.Target} }
func (i *InstrJump) String() string       { return fmt.Sprintf("  jump label %%%s", i.Target) }

type InstrBranch struct {
	Cond       Value
	ThenTarget string
	ElseTarget string
}

func (i *InstrBranch) Result() *Reg         { return nil }
func (i *InstrBranch) Successors() []string { return []string{i.ThenTarget, i.ElseTarget} }
func (i *InstrBranch) String() string {
	return fmt.Sprintf("  branch %s, label %%%s, label %%%s", i.Cond, i.ThenTarget, i.ElseTarget)
}

type InstrReturn struct {
	Vals []Value
}

func (i *InstrReturn) Result() *Reg         { return nil }
func (i *InstrReturn) Successors() []string { return nil }
func (i *InstrReturn) String() string {
	if len(i.Vals) == 0 {
		return "  ret void"
	}
	vStrs := make([]string, len(i.Vals))
	for idx, v := range i.Vals {
		vStrs[idx] = fmt.Sprintf("%s %s", v.Type().TypeName(), v)
	}
	return fmt.Sprintf("  ret %s", strings.Join(vStrs, ", "))
}

type BasicBlock struct {
	Label        string
	Instructions []Instruction
	Terminator   Terminator
}

func (b *BasicBlock) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s:\n", b.Label))
	for _, inst := range b.Instructions {
		sb.WriteString(inst.String())
		sb.WriteString("\n")
	}
	if b.Terminator != nil {
		sb.WriteString(b.Terminator.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

type ItabMethodEntry struct {
	MethodName   string
	TargetFnName string
	MethodType   sema.Method
}

type ItabDef struct {
	GlobalName     string
	ConcreteType   sema.Type
	InterfaceType  *sema.InterfaceType
	TypeID         int64
	ItabStructName string
	Methods        []ItabMethodEntry
}

type Function struct {
	Name        string
	Params      []*Reg
	ReturnTypes []sema.Type
	Blocks      []*BasicBlock
	IsVariadic  bool
	IsExtern    bool
}

func (f *Function) String() string {
	var sb strings.Builder
	retTypeStr := "void"
	if len(f.ReturnTypes) == 1 {
		retTypeStr = f.ReturnTypes[0].TypeName()
	} else if len(f.ReturnTypes) > 1 {
		types := make([]string, len(f.ReturnTypes))
		for idx, rt := range f.ReturnTypes {
			types[idx] = rt.TypeName()
		}
		retTypeStr = fmt.Sprintf("(%s)", strings.Join(types, ", "))
	}

	params := make([]string, len(f.Params))
	for idx, p := range f.Params {
		params[idx] = fmt.Sprintf("%s %s", p.Typ.TypeName(), p)
	}
	if f.IsVariadic {
		params = append(params, "...")
	}

	if f.IsExtern {
		sb.WriteString(fmt.Sprintf("extern func @%s(%s) %s\n", f.Name, strings.Join(params, ", "), retTypeStr))
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("func @%s(%s) %s {\n", f.Name, strings.Join(params, ", "), retTypeStr))
	for _, bb := range f.Blocks {
		sb.WriteString(bb.String())
	}
	sb.WriteString("}\n")
	return sb.String()
}

type Program struct {
	ModuleName      string
	StringConstants []*ConstString
	Globals         []*GlobalVar
	Itabs           []*ItabDef
	Functions       []*Function
}

func (p *Program) Dump() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("; Module: %s\n\n", p.ModuleName))

	if len(p.StringConstants) > 0 {
		sb.WriteString("; --- String Constants ---\n")
		for _, sc := range p.StringConstants {
			sb.WriteString(fmt.Sprintf("@%s = constant [%d bytes] %q\n", sc.Label, sc.Length, sc.Raw))
		}
		sb.WriteString("\n")
	}

	if len(p.Globals) > 0 {
		sb.WriteString("; --- Globals ---\n")
		for _, g := range p.Globals {
			sb.WriteString(fmt.Sprintf("@%s = global %s\n", g.Name, g.Typ.TypeName()))
		}
		sb.WriteString("\n")
	}

	if len(p.Itabs) > 0 {
		sb.WriteString("; --- Itabs ---\n")
		for _, itab := range p.Itabs {
			sb.WriteString(fmt.Sprintf("@%s = itab %s for %s (id: %d)\n",
				itab.GlobalName, itab.ConcreteType.TypeName(), itab.InterfaceType.TypeName(), itab.TypeID))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("; --- Functions ---\n")
	for _, fn := range p.Functions {
		sb.WriteString(fn.String())
		sb.WriteString("\n")
	}

	return sb.String()
}
