package llvm

import (
	"fmt"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

// basicBlock is an LLVM-only transient CFG view. Structured HIR remains the
// semantic source; this representation exists only while emitting LLVM IR.
type basicBlock struct {
	Label        string
	Instructions []hir.Instruction
	Terminator   hir.Terminator
}

func (b *basicBlock) String() string {
	out := fmt.Sprintf("%s:\n", b.Label)
	for _, inst := range b.Instructions {
		out += inst.String() + "\n"
	}
	if b.Terminator != nil {
		out += b.Terminator.String() + "\n"
	}
	return out
}

func lowerStructuredBody(fn *hir.Function) ([]*basicBlock, error) {
	if fn == nil {
		return nil, fmt.Errorf("cannot lower structured body of nil function")
	}
	for id, n := range fn.ControlNodes {
		if n == nil || n.Index() != id {
			return nil, fmt.Errorf("control table entry %d has inconsistent ID", id)
		}
	}
	b := &structuredLowerer{fn: fn, resolved: map[int]string{}, labels: map[string]string{}}
	b.current = b.newBlock("entry")
	if err := b.body(fn.StructuredBody, map[int]string{}); err != nil {
		return nil, err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &hir.InstrReturn{Vals: defaultReturnValues(fn)}
	}
	return b.blocks, nil
}

func defaultReturnValues(fn *hir.Function) []hir.Value {
	if fn == nil || fn.Name == "main" {
		return []hir.Value{&hir.ConstInt{Val: 0, Typ: sema.TypeInt}}
	}
	values := make([]hir.Value, len(fn.ReturnTypes))
	for i, typ := range fn.ReturnTypes {
		switch typ {
		case sema.TypeBool:
			values[i] = &hir.ConstBool{Val: false, Typ: typ}
		case sema.TypeFloat32, sema.TypeFloat64:
			values[i] = &hir.ConstFloat{Val: 0, Typ: typ}
		default:
			if typ == nil {
				values[i] = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
			} else if _, ok := typ.(*sema.PointerType); ok {
				values[i] = &hir.ConstNil{Typ: typ}
			} else {
				values[i] = &hir.ConstZero{Typ: typ}
			}
		}
	}
	return values
}

type structuredLowerer struct {
	fn       *hir.Function
	blocks   []*basicBlock
	current  *basicBlock
	serial   int
	resolved map[int]string
	labels   map[string]string
}

func (b *structuredLowerer) newBlock(prefix string) *basicBlock {
	bb := &basicBlock{Label: fmt.Sprintf("structured.%s.%d", prefix, b.serial), Instructions: []hir.Instruction{}}
	b.serial++
	b.blocks = append(b.blocks, bb)
	return bb
}
func (b *structuredLowerer) ensure() {
	if b.current == nil {
		b.current = b.newBlock("continuation")
	}
}
func cloneTargets(src map[int]string) map[int]string {
	dst := make(map[int]string, len(src)+1)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (b *structuredLowerer) body(body hir.ControlBody, targets map[int]string) error {
	for _, node := range body {
		if node == nil {
			return fmt.Errorf("nil structured control node")
		}
		switch n := node.(type) {
		case *hir.InstructionNode:
			if n.Instruction == nil {
				return fmt.Errorf("structured instruction is nil")
			}
			b.ensure()
			b.current.Instructions = append(b.current.Instructions, n.Instruction)
		case *hir.BlockNode:
			if err := b.block(n, targets); err != nil {
				return err
			}
		case *hir.LoopNode:
			if err := b.loop(n, targets); err != nil {
				return err
			}
		case *hir.IfNode:
			if err := b.ifNode(n, targets); err != nil {
				return err
			}
		case *hir.TableNode:
			if err := b.table(n, targets); err != nil {
				return err
			}
		case *hir.BrNode:
			if err := b.validate(n, n.TargetID, targets); err != nil {
				return err
			}
			b.branch(n.TargetID, n.Target, targets)
		case *hir.BrIfNode:
			b.ensure()
			next := b.newBlock("br_if.next")
			if err := b.validate(n, n.TargetID, targets); err != nil {
				return err
			}
			target, err := b.target(n.TargetID, n.Target, targets)
			if err != nil {
				return err
			}
			b.current.Terminator = &hir.InstrBranch{Cond: n.Cond, ThenTarget: target, ElseTarget: next.Label}
			b.current = next
		case *hir.BrTableNode:
			b.ensure()
			for _, id := range n.TargetIDs {
				if err := b.validate(n, id, targets); err != nil {
					return err
				}
			}
			if err := b.validate(n, n.DefaultTarget, targets); err != nil {
				return err
			}
			ts, err := b.targets(n.TargetIDs, n.Targets, targets)
			if err != nil {
				return err
			}
			def, err := b.target(n.DefaultTarget, n.Default, targets)
			if err != nil {
				return err
			}
			b.current.Terminator = &hir.InstrBrTable{Index: n.IndexValue, Targets: ts, DefaultTarget: def}
			b.current = b.newBlock("br_table.next")
		case *hir.ReturnNode:
			b.ensure()
			b.current.Terminator = &hir.InstrReturn{Vals: n.Values}
			b.current = b.newBlock("after.return")
		case *hir.UnreachableNode:
			b.ensure()
			b.current.Terminator = &hir.InstrUnreachable{}
			b.current = b.newBlock("after.unreachable")
		default:
			return fmt.Errorf("unsupported structured node %T", node)
		}
	}
	return nil
}
func (b *structuredLowerer) block(n *hir.BlockNode, parent map[int]string) error {
	end := b.newBlock("block.end")
	b.resolved[n.ID] = end.Label
	if n.Label != "" {
		b.labels[n.Label] = end.Label
	}
	t := cloneTargets(parent)
	t[n.ID] = end.Label
	if err := b.body(n.Body, t); err != nil {
		return err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &hir.InstrJump{Target: end.Label}
	}
	b.current = end
	return nil
}
func (b *structuredLowerer) loop(n *hir.LoopNode, parent map[int]string) error {
	head, end := b.newBlock("loop.header"), b.newBlock("loop.end")
	post := head
	if len(n.Post) > 0 {
		post = b.newBlock("loop.post")
	}
	b.resolved[n.ID] = post.Label
	if n.Label != "" {
		b.labels[n.Label] = post.Label
	}
	b.ensure()
	if len(n.Init) > 0 {
		if err := b.body(n.Init, parent); err != nil {
			return err
		}
	}
	if b.current.Terminator == nil {
		b.current.Terminator = &hir.InstrJump{Target: head.Label}
	}
	t := cloneTargets(parent)
	t[n.ID] = post.Label
	b.current = head
	if err := b.body(n.Body, t); err != nil {
		return err
	}
	if len(n.Post) > 0 {
		if b.current != nil && b.current.Terminator == nil {
			b.current.Terminator = &hir.InstrJump{Target: post.Label}
		}
		b.current = post
		postTargets := cloneTargets(parent)
		postTargets[n.ID] = head.Label
		if err := b.body(n.Post, postTargets); err != nil {
			return err
		}
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &hir.InstrJump{Target: head.Label}
	}
	b.current = end
	return nil
}
func (b *structuredLowerer) ifNode(n *hir.IfNode, parent map[int]string) error {
	thenBB, elseBB, end := b.newBlock("if.then"), b.newBlock("if.else"), b.newBlock("if.end")
	b.ensure()
	t := cloneTargets(parent)
	t[n.ID] = end.Label
	b.current.Terminator = &hir.InstrBranch{Cond: n.Cond, ThenTarget: thenBB.Label, ElseTarget: elseBB.Label}
	b.current = thenBB
	if err := b.body(n.Then, t); err != nil {
		return err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &hir.InstrJump{Target: end.Label}
	}
	b.current = elseBB
	if err := b.body(n.Else, t); err != nil {
		return err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &hir.InstrJump{Target: end.Label}
	}
	b.current = end
	return nil
}
func (b *structuredLowerer) table(n *hir.TableNode, parent map[int]string) error {
	end := b.newBlock("table.end")
	t := cloneTargets(parent)
	t[n.ID] = end.Label
	cases := make([]*basicBlock, len(n.Cases))
	for i := range cases {
		cases[i] = b.newBlock(fmt.Sprintf("table.case.%d", i))
	}
	def := end
	if len(n.Default) > 0 {
		def = b.newBlock("table.default")
	}
	resolved := make([]string, len(n.Targets))
	for i, x := range n.Targets {
		if x >= 0 && x < len(cases) {
			resolved[i] = cases[x].Label
		} else {
			resolved[i] = def.Label
		}
	}
	b.ensure()
	b.current.Terminator = &hir.InstrBrTable{Index: n.IndexValue, Targets: resolved, DefaultTarget: def.Label}
	for i, c := range n.Cases {
		b.current = cases[i]
		if err := b.body(c.Body, t); err != nil {
			return err
		}
		if b.current != nil && b.current.Terminator == nil {
			b.current.Terminator = &hir.InstrJump{Target: end.Label}
		}
	}
	if len(n.Default) > 0 {
		b.current = def
		if err := b.body(n.Default, t); err != nil {
			return err
		}
		if b.current != nil && b.current.Terminator == nil {
			b.current.Terminator = &hir.InstrJump{Target: end.Label}
		}
	}
	b.current = end
	return nil
}
func (b *structuredLowerer) branch(id int, label string, t map[int]string) {
	b.ensure()
	if x, ok := t[id]; ok && id >= 0 {
		b.current.Terminator = &hir.InstrJump{Target: x}
	} else if x, ok := b.resolved[id]; ok && id >= 0 {
		b.current.Terminator = &hir.InstrJump{Target: x}
	} else if x, ok := b.labels[label]; ok {
		b.current.Terminator = &hir.InstrJump{Target: x}
	} else if label != "" {
		b.current.Terminator = &hir.InstrJump{Target: label}
	} else {
		b.current.Terminator = &hir.InstrUnreachable{}
	}
	b.current = b.newBlock("after.branch")
}
func (b *structuredLowerer) validate(branch hir.ControlElement, id int, t map[int]string) error {
	if id < 0 {
		return nil
	}
	if _, ok := t[id]; !ok {
		return nil
	}
	_, _ = b.fn.BranchDepth(branch, id)
	return nil
}
func (b *structuredLowerer) target(id int, label string, t map[int]string) (string, error) {
	if id >= 0 {
		if _, ok := b.fn.ControlNodeAt(id); !ok {
			return "", fmt.Errorf("unknown structured control target id %d", id)
		}
	}
	if x, ok := t[id]; ok && id >= 0 {
		return x, nil
	}
	if x, ok := b.resolved[id]; ok && id >= 0 {
		return x, nil
	}
	if x, ok := b.labels[label]; ok {
		return x, nil
	}
	if label != "" {
		return label, nil
	}
	return "", fmt.Errorf("unknown structured control target id %d", id)
}
func (b *structuredLowerer) targets(ids []int, labels []string, t map[int]string) ([]string, error) {
	n := len(ids)
	if n == 0 {
		n = len(labels)
	}
	out := make([]string, n)
	for i := range out {
		id, label := -1, ""
		if i < len(ids) {
			id = ids[i]
		}
		if i < len(labels) {
			label = labels[i]
		}
		x, err := b.target(id, label, t)
		if err != nil {
			return nil, err
		}
		out[i] = x
	}
	return out, nil
}
