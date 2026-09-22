package hir

import "fmt"

// LowerStructuredBody converts structured control into the legacy CFG form.
// Control IDs are resolved through the lexical target map; no label scan is
// required. This is the compatibility bridge used while backends migrate to
// native structured emission.
func LowerStructuredBody(fn *Function) ([]*BasicBlock, error) {
	if fn == nil {
		return nil, fmt.Errorf("cannot lower structured body of nil function")
	}
	for id, node := range fn.ControlNodes {
		if node == nil || node.Index() != id {
			return nil, fmt.Errorf("control table entry %d has inconsistent ID", id)
		}
	}
	b := &structuredCFGBuilder{fn: fn}
	b.current = b.newBlock("entry")
	if err := b.lowerBody(fn.StructuredBody, map[int]string{}); err != nil {
		return nil, err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &InstrUnreachable{}
	}
	return b.blocks, nil
}

type structuredCFGBuilder struct {
	fn      *Function
	blocks  []*BasicBlock
	current *BasicBlock
	serial  int
}

func (b *structuredCFGBuilder) newBlock(prefix string) *BasicBlock {
	label := fmt.Sprintf("structured.%s.%d", prefix, b.serial)
	b.serial++
	block := &BasicBlock{Label: label, Instructions: []Instruction{}}
	b.blocks = append(b.blocks, block)
	return block
}

func (b *structuredCFGBuilder) lowerBody(body ControlBody, targets map[int]string) error {
	for _, node := range body {
		if node == nil {
			return fmt.Errorf("nil structured control node")
		}
		switch n := node.(type) {
		case *InstructionNode:
			if n.Instruction == nil {
				return fmt.Errorf("structured instruction is nil")
			}
			b.ensureCurrent()
			b.current.Instructions = append(b.current.Instructions, n.Instruction)
		case *BlockNode:
			if err := b.lowerBlock(n, targets); err != nil {
				return err
			}
		case *LoopNode:
			if err := b.lowerLoop(n, targets); err != nil {
				return err
			}
		case *IfNode:
			if err := b.lowerIf(n, targets); err != nil {
				return err
			}
		case *TableNode:
			if err := b.lowerTable(n, targets); err != nil {
				return err
			}
		case *BrNode:
			if err := b.validateBranch(n, n.TargetID, targets); err != nil {
				return err
			}
			b.branchTo(n.TargetID, n.Target, targets)
		case *BrIfNode:
			b.ensureCurrent()
			continuation := b.newBlock("br_if.next")
			if err := b.validateBranch(n, n.TargetID, targets); err != nil {
				return err
			}
			target, err := b.resolveTarget(n.TargetID, n.Target, targets)
			if err != nil {
				return err
			}
			b.current.Terminator = &InstrBranch{Cond: n.Cond, ThenTarget: target, ElseTarget: continuation.Label}
			b.current = continuation
		case *BrTableNode:
			b.ensureCurrent()
			for _, targetID := range n.TargetIDs {
				if err := b.validateBranch(n, targetID, targets); err != nil {
					return err
				}
			}
			if err := b.validateBranch(n, n.DefaultTarget, targets); err != nil {
				return err
			}
			resolvedTargets, err := b.resolveTargets(n.TargetIDs, n.Targets, targets)
			if err != nil {
				return err
			}
			defaultTarget, err := b.resolveTarget(n.DefaultTarget, n.Default, targets)
			if err != nil {
				return err
			}
			b.current.Terminator = &InstrBrTable{Index: n.IndexValue, Targets: resolvedTargets, DefaultTarget: defaultTarget}
			b.current = b.newBlock("br_table.next")
		case *ReturnNode:
			b.ensureCurrent()
			b.current.Terminator = &InstrReturn{Vals: n.Values}
			b.current = b.newBlock("after.return")
		case *UnreachableNode:
			b.ensureCurrent()
			b.current.Terminator = &InstrUnreachable{}
			b.current = b.newBlock("after.unreachable")
		default:
			return fmt.Errorf("unsupported structured node %T", node)
		}
	}
	return nil
}

func (b *structuredCFGBuilder) lowerBlock(node *BlockNode, parent map[int]string) error {
	end := b.newBlock("block.end")
	targets := copyTargetMap(parent)
	targets[node.ID] = end.Label
	if err := b.lowerBody(node.Body, targets); err != nil {
		return err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &InstrJump{Target: end.Label}
	}
	b.current = end
	return nil
}

func (b *structuredCFGBuilder) lowerLoop(node *LoopNode, parent map[int]string) error {
	header := b.newBlock("loop.header")
	end := b.newBlock("loop.end")
	b.ensureCurrent()
	if b.current.Terminator == nil {
		b.current.Terminator = &InstrJump{Target: header.Label}
	}
	targets := copyTargetMap(parent)
	targets[node.ID] = header.Label
	b.current = header
	if err := b.lowerBody(node.Body, targets); err != nil {
		return err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &InstrJump{Target: header.Label}
	}
	b.current = end
	return nil
}

func (b *structuredCFGBuilder) lowerIf(node *IfNode, parent map[int]string) error {
	thenBlock := b.newBlock("if.then")
	elseBlock := b.newBlock("if.else")
	end := b.newBlock("if.end")
	b.ensureCurrent()
	targets := copyTargetMap(parent)
	targets[node.ID] = end.Label
	b.current.Terminator = &InstrBranch{Cond: node.Cond, ThenTarget: thenBlock.Label, ElseTarget: elseBlock.Label}
	b.current = thenBlock
	if err := b.lowerBody(node.Then, targets); err != nil {
		return err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &InstrJump{Target: end.Label}
	}
	b.current = elseBlock
	if err := b.lowerBody(node.Else, targets); err != nil {
		return err
	}
	if b.current != nil && b.current.Terminator == nil {
		b.current.Terminator = &InstrJump{Target: end.Label}
	}
	b.current = end
	return nil
}

func (b *structuredCFGBuilder) lowerTable(node *TableNode, parent map[int]string) error {
	end := b.newBlock("table.end")
	targets := copyTargetMap(parent)
	targets[node.ID] = end.Label
	caseBlocks := make([]*BasicBlock, len(node.Cases))
	for i := range node.Cases {
		caseBlocks[i] = b.newBlock(fmt.Sprintf("table.case.%d", i))
	}
	defaultBlock := end
	if len(node.Default) > 0 {
		defaultBlock = b.newBlock("table.default")
	}
	resolved := make([]string, len(node.Targets))
	for i, caseIndex := range node.Targets {
		if caseIndex >= 0 && caseIndex < len(caseBlocks) {
			resolved[i] = caseBlocks[caseIndex].Label
		} else {
			resolved[i] = defaultBlock.Label
		}
	}
	b.ensureCurrent()
	b.current.Terminator = &InstrBrTable{Index: node.IndexValue, Targets: resolved, DefaultTarget: defaultBlock.Label}
	for i, c := range node.Cases {
		b.current = caseBlocks[i]
		if err := b.lowerBody(c.Body, targets); err != nil {
			return err
		}
		if b.current != nil && b.current.Terminator == nil {
			b.current.Terminator = &InstrJump{Target: end.Label}
		}
	}
	if len(node.Default) > 0 {
		b.current = defaultBlock
		if err := b.lowerBody(node.Default, targets); err != nil {
			return err
		}
		if b.current != nil && b.current.Terminator == nil {
			b.current.Terminator = &InstrJump{Target: end.Label}
		}
	}
	b.current = end
	return nil
}

func (b *structuredCFGBuilder) branchTo(id int, label string, targets map[int]string) {
	b.ensureCurrent()
	if target, ok := targets[id]; ok && id >= 0 {
		b.current.Terminator = &InstrJump{Target: target}
	} else if label != "" {
		b.current.Terminator = &InstrJump{Target: label}
	} else {
		b.current.Terminator = &InstrUnreachable{}
	}
	b.current = b.newBlock("after.branch")
}

func (b *structuredCFGBuilder) validateBranch(branch ControlElement, targetID int, targets map[int]string) error {
	if targetID < 0 {
		return nil
	}
	if _, ok := targets[targetID]; !ok {
		return fmt.Errorf("control target %d is outside the current structured scope", targetID)
	}
	if _, ok := b.fn.BranchDepth(branch, targetID); !ok {
		return fmt.Errorf("control target %d has invalid depth for branch %d", targetID, branch.Index())
	}
	return nil
}

func (b *structuredCFGBuilder) resolveTarget(id int, label string, targets map[int]string) (string, error) {
	if id >= 0 {
		if _, ok := b.fn.ControlNodeAt(id); !ok {
			return "", fmt.Errorf("unknown structured control target id %d", id)
		}
	}
	if target, ok := targets[id]; ok && id >= 0 {
		return target, nil
	}
	if label != "" {
		return label, nil
	}
	return "", fmt.Errorf("unknown structured control target id %d", id)
}

func (b *structuredCFGBuilder) resolveTargets(ids []int, labels []string, targets map[int]string) ([]string, error) {
	result := make([]string, len(ids))
	if len(ids) == 0 {
		result = make([]string, len(labels))
	}
	for i := range result {
		id := -1
		label := ""
		if i < len(ids) {
			id = ids[i]
		}
		if i < len(labels) {
			label = labels[i]
		}
		resolved, err := b.resolveTarget(id, label, targets)
		if err != nil {
			return nil, err
		}
		result[i] = resolved
	}
	return result, nil
}

func (b *structuredCFGBuilder) ensureCurrent() {
	if b.current == nil {
		b.current = b.newBlock("continuation")
	}
}

func copyTargetMap(source map[int]string) map[int]string {
	result := make(map[int]string, len(source)+1)
	for id, target := range source {
		result[id] = target
	}
	return result
}
