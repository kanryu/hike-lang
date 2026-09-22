// Package structuredcfg contains a backend-only compatibility lowering from
// structured HIR to transient basic blocks. It is deliberately outside HIR;
// backends should prefer direct structured emission whenever possible.
package structuredcfg

import (
	"fmt"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

type BasicBlock struct {
	Label        string
	Instructions []hir.Instruction
	Terminator   hir.Terminator
}

func LowerStructuredBody(fn *hir.Function) ([]*BasicBlock, error) {
	if fn == nil {
		return nil, fmt.Errorf("cannot lower nil function")
	}
	for id, n := range fn.ControlNodes {
		if n == nil || n.Index() != id {
			return nil, fmt.Errorf("control table entry %d has inconsistent ID", id)
		}
	}
	l := &lowerer{fn: fn, resolved: map[int]string{}, labels: map[string]string{}}
	l.current = l.newBlock("entry")
	if err := l.body(fn.StructuredBody, map[int]string{}); err != nil {
		return nil, err
	}
	if l.current != nil && l.current.Terminator == nil {
		l.current.Terminator = &hir.InstrReturn{Vals: defaults(fn)}
	}
	return l.blocks, nil
}

func defaults(fn *hir.Function) []hir.Value {
	if fn == nil || fn.Name == "main" {
		return []hir.Value{&hir.ConstInt{Val: 0, Typ: sema.TypeInt}}
	}
	out := make([]hir.Value, len(fn.ReturnTypes))
	for i, t := range fn.ReturnTypes {
		switch t {
		case sema.TypeBool:
			out[i] = &hir.ConstBool{Val: false, Typ: t}
		case sema.TypeFloat32, sema.TypeFloat64:
			out[i] = &hir.ConstFloat{Val: 0, Typ: t}
		default:
			if t == nil {
				out[i] = &hir.ConstInt{Val: 0, Typ: sema.TypeInt}
			} else if _, ok := t.(*sema.PointerType); ok {
				out[i] = &hir.ConstNil{Typ: t}
			} else {
				out[i] = &hir.ConstZero{Typ: t}
			}
		}
	}
	return out
}

type lowerer struct {
	fn       *hir.Function
	blocks   []*BasicBlock
	current  *BasicBlock
	serial   int
	resolved map[int]string
	labels   map[string]string
}

func (l *lowerer) newBlock(p string) *BasicBlock {
	b := &BasicBlock{Label: fmt.Sprintf("structured.%s.%d", p, l.serial), Instructions: []hir.Instruction{}}
	l.serial++
	l.blocks = append(l.blocks, b)
	return b
}
func (l *lowerer) ensure() {
	if l.current == nil {
		l.current = l.newBlock("continuation")
	}
}
func targets(m map[int]string) map[int]string {
	n := make(map[int]string, len(m)+1)
	for k, v := range m {
		n[k] = v
	}
	return n
}
func (l *lowerer) body(body hir.ControlBody, visible map[int]string) error {
	for _, node := range body {
		if node == nil {
			return fmt.Errorf("nil structured control node")
		}
		switch n := node.(type) {
		case *hir.InstructionNode:
			if n.Instruction == nil {
				return fmt.Errorf("structured instruction is nil")
			}
			l.ensure()
			l.current.Instructions = append(l.current.Instructions, n.Instruction)
		case *hir.BlockNode:
			if err := l.block(n, visible); err != nil {
				return err
			}
		case *hir.LoopNode:
			if err := l.loop(n, visible); err != nil {
				return err
			}
		case *hir.IfNode:
			if err := l.ifNode(n, visible); err != nil {
				return err
			}
		case *hir.TableNode:
			if err := l.table(n, visible); err != nil {
				return err
			}
		case *hir.BrNode:
			if err := l.check(n, n.TargetID, visible); err != nil {
				return err
			}
			l.branch(n.TargetID, n.Target, visible)
		case *hir.BrIfNode:
			l.ensure()
			next := l.newBlock("br_if.next")
			if err := l.check(n, n.TargetID, visible); err != nil {
				return err
			}
			dst, err := l.target(n.TargetID, n.Target, visible)
			if err != nil {
				return err
			}
			l.current.Terminator = &hir.InstrBranch{Cond: n.Cond, ThenTarget: dst, ElseTarget: next.Label}
			l.current = next
		case *hir.BrTableNode:
			l.ensure()
			for _, id := range n.TargetIDs {
				if err := l.check(n, id, visible); err != nil {
					return err
				}
			}
			if err := l.check(n, n.DefaultTarget, visible); err != nil {
				return err
			}
			ts, err := l.multi(n.TargetIDs, n.Targets, visible)
			if err != nil {
				return err
			}
			d, err := l.target(n.DefaultTarget, n.Default, visible)
			if err != nil {
				return err
			}
			l.current.Terminator = &hir.InstrBrTable{Index: n.IndexValue, Targets: ts, DefaultTarget: d}
			l.current = l.newBlock("br_table.next")
		case *hir.ReturnNode:
			l.ensure()
			l.current.Terminator = &hir.InstrReturn{Vals: n.Values}
			l.current = l.newBlock("after.return")
		case *hir.UnreachableNode:
			l.ensure()
			l.current.Terminator = &hir.InstrUnreachable{}
			l.current = l.newBlock("after.unreachable")
		case *hir.PanicNode:
			l.ensure()
			l.current.Terminator = &hir.InstrPanic{Value: n.Value, Cause: n.Cause, SiteID: n.SiteID}
			l.current = l.newBlock("after.panic")
		default:
			return fmt.Errorf("unsupported structured node %T", node)
		}
	}
	return nil
}
func (l *lowerer) block(n *hir.BlockNode, p map[int]string) error {
	end := l.newBlock("block.end")
	l.resolved[n.ID] = end.Label
	if n.Label != "" {
		l.labels[n.Label] = end.Label
	}
	v := targets(p)
	v[n.ID] = end.Label
	if err := l.body(n.Body, v); err != nil {
		return err
	}
	if l.current != nil && l.current.Terminator == nil {
		l.current.Terminator = &hir.InstrJump{Target: end.Label}
	}
	l.current = end
	return nil
}
func (l *lowerer) loop(n *hir.LoopNode, p map[int]string) error {
	head, end := l.newBlock("loop.header"), l.newBlock("loop.end")
	post := head
	if len(n.Post) > 0 {
		post = l.newBlock("loop.post")
	}
	l.resolved[n.ID] = post.Label
	if n.Label != "" {
		l.labels[n.Label] = post.Label
	}
	l.ensure()
	if err := l.body(n.Init, p); err != nil {
		return err
	}
	if l.current.Terminator == nil {
		l.current.Terminator = &hir.InstrJump{Target: head.Label}
	}
	v := targets(p)
	v[n.ID] = post.Label
	l.current = head
	if err := l.body(n.Body, v); err != nil {
		return err
	}
	if len(n.Post) > 0 {
		if l.current != nil && l.current.Terminator == nil {
			l.current.Terminator = &hir.InstrJump{Target: post.Label}
		}
		l.current = post
		v = targets(p)
		v[n.ID] = head.Label
		if err := l.body(n.Post, v); err != nil {
			return err
		}
	}
	if l.current != nil && l.current.Terminator == nil {
		l.current.Terminator = &hir.InstrJump{Target: head.Label}
	}
	l.current = end
	return nil
}
func (l *lowerer) ifNode(n *hir.IfNode, p map[int]string) error {
	th, el, end := l.newBlock("if.then"), l.newBlock("if.else"), l.newBlock("if.end")
	l.ensure()
	v := targets(p)
	v[n.ID] = end.Label
	l.current.Terminator = &hir.InstrBranch{Cond: n.Cond, ThenTarget: th.Label, ElseTarget: el.Label}
	l.current = th
	if err := l.body(n.Then, v); err != nil {
		return err
	}
	if l.current != nil && l.current.Terminator == nil {
		l.current.Terminator = &hir.InstrJump{Target: end.Label}
	}
	l.current = el
	if err := l.body(n.Else, v); err != nil {
		return err
	}
	if l.current != nil && l.current.Terminator == nil {
		l.current.Terminator = &hir.InstrJump{Target: end.Label}
	}
	l.current = end
	return nil
}
func (l *lowerer) table(n *hir.TableNode, p map[int]string) error {
	end := l.newBlock("table.end")
	v := targets(p)
	v[n.ID] = end.Label
	cs := make([]*BasicBlock, len(n.Cases))
	for i := range cs {
		cs[i] = l.newBlock(fmt.Sprintf("table.case.%d", i))
	}
	def := end
	if len(n.Default) > 0 {
		def = l.newBlock("table.default")
	}
	ts := make([]string, len(n.Targets))
	for i, x := range n.Targets {
		if x >= 0 && x < len(cs) {
			ts[i] = cs[x].Label
		} else {
			ts[i] = def.Label
		}
	}
	l.ensure()
	l.current.Terminator = &hir.InstrBrTable{Index: n.IndexValue, Targets: ts, DefaultTarget: def.Label}
	for i, c := range n.Cases {
		l.current = cs[i]
		if err := l.body(c.Body, v); err != nil {
			return err
		}
		if l.current != nil && l.current.Terminator == nil {
			l.current.Terminator = &hir.InstrJump{Target: end.Label}
		}
	}
	if len(n.Default) > 0 {
		l.current = def
		if err := l.body(n.Default, v); err != nil {
			return err
		}
		if l.current != nil && l.current.Terminator == nil {
			l.current.Terminator = &hir.InstrJump{Target: end.Label}
		}
	}
	l.current = end
	return nil
}
func (l *lowerer) branch(id int, label string, v map[int]string) {
	l.ensure()
	if x, ok := v[id]; ok && id >= 0 {
		l.current.Terminator = &hir.InstrJump{Target: x}
	} else if x, ok := l.resolved[id]; ok && id >= 0 {
		l.current.Terminator = &hir.InstrJump{Target: x}
	} else if x, ok := l.labels[label]; ok {
		l.current.Terminator = &hir.InstrJump{Target: x}
	} else if label != "" {
		l.current.Terminator = &hir.InstrJump{Target: label}
	} else {
		l.current.Terminator = &hir.InstrUnreachable{}
	}
	l.current = l.newBlock("after.branch")
}
func (l *lowerer) check(_ hir.ControlElement, id int, v map[int]string) error {
	if id < 0 {
		return nil
	}
	if _, ok := v[id]; !ok {
		return nil
	}
	return nil
}
func (l *lowerer) target(id int, label string, v map[int]string) (string, error) {
	if id >= 0 {
		if _, ok := l.fn.ControlNodeAt(id); !ok {
			return "", fmt.Errorf("unknown structured control target id %d", id)
		}
	}
	if x, ok := v[id]; ok && id >= 0 {
		return x, nil
	}
	if x, ok := l.resolved[id]; ok && id >= 0 {
		return x, nil
	}
	if x, ok := l.labels[label]; ok {
		return x, nil
	}
	if label != "" {
		return label, nil
	}
	return "", fmt.Errorf("unknown structured control target id %d", id)
}
func (l *lowerer) multi(ids []int, labels []string, v map[int]string) ([]string, error) {
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
		x, err := l.target(id, label, v)
		if err != nil {
			return nil, err
		}
		out[i] = x
	}
	return out, nil
}
