package hir

// FlattenTransparentBlocks removes only synthetic, unreferenced instruction
// containers. Control-table entries are deliberately retained so that the
// function-local IDs remain stable after normalization.
func FlattenTransparentBlocks(fn *Function) {
	if fn == nil || fn.ControlRoot == nil {
		return
	}

	referenced := make(map[int]bool)
	for _, element := range fn.ControlNodes {
		if element == nil {
			continue
		}
		markControlID(referenced, element.Next())
		markControlID(referenced, element.BreakTarget())
		markControlID(referenced, element.ContinueTarget())
	}
	markBodyReferences(referenced, fn.StructuredBody)
	fn.StructuredBody = flattenControlBody(fn.StructuredBody, referenced)
}

func markControlID(referenced map[int]bool, id int) {
	if id >= 0 {
		referenced[id] = true
	}
}

func markBodyReferences(referenced map[int]bool, body ControlBody) {
	for _, node := range body {
		switch n := node.(type) {
		case *BlockNode:
			markBodyReferences(referenced, n.Body)
		case *LoopNode:
			markBodyReferences(referenced, n.Body)
		case *IfNode:
			markBodyReferences(referenced, n.Then)
			markBodyReferences(referenced, n.Else)
		case *TableNode:
			for _, c := range n.Cases {
				markBodyReferences(referenced, c.Body)
			}
			markBodyReferences(referenced, n.Default)
		case *BrNode:
			markControlID(referenced, n.TargetID)
		case *BrIfNode:
			markControlID(referenced, n.TargetID)
		case *BrTableNode:
			for _, id := range n.TargetIDs {
				markControlID(referenced, id)
			}
			markControlID(referenced, n.DefaultTarget)
		}
	}
}

func flattenControlBody(body ControlBody, referenced map[int]bool) ControlBody {
	for _, node := range body {
		switch n := node.(type) {
		case *BlockNode:
			n.Body = flattenControlBody(n.Body, referenced)
		case *LoopNode:
			n.Body = flattenControlBody(n.Body, referenced)
		case *IfNode:
			n.Then = flattenControlBody(n.Then, referenced)
			n.Else = flattenControlBody(n.Else, referenced)
		case *TableNode:
			for i := range n.Cases {
				n.Cases[i].Body = flattenControlBody(n.Cases[i].Body, referenced)
			}
			n.Default = flattenControlBody(n.Default, referenced)
		}
	}

	if len(body) != 1 {
		return body
	}
	block, ok := body[0].(*BlockNode)
	if !ok || block.FunctionExit || !block.ContinuationPlaceholder || referenced[block.Index()] {
		return body
	}
	if !isInstructionOnlyBody(block.Body) {
		return body
	}
	return block.Body
}

func isInstructionOnlyBody(body ControlBody) bool {
	for _, node := range body {
		switch node.(type) {
		case *InstructionNode, *ReturnNode, *UnreachableNode:
		default:
			return false
		}
	}
	return true
}
