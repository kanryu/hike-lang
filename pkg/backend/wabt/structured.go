package wabt

import (
	"strings"

	"hikec-go/pkg/hir"
)

// canEmitStructuredSubset reports whether a body only contains structured
// containers whose fallthrough semantics can be emitted directly. Unsupported
// nodes remain on the compatibility CFG path until their WAT lowering exists.
func canEmitStructuredSubset(body hir.ControlBody) bool {
	return canEmitStructuredBody(body, map[int]bool{})
}

func canEmitStructuredBody(body hir.ControlBody, visible map[int]bool) bool {
	for _, node := range body {
		switch n := node.(type) {
		case *hir.InstructionNode:
			if n.Instruction == nil {
				return false
			}
		case *hir.BlockNode:
			nested := copyVisible(visible)
			nested[n.ID] = true
			if !canEmitStructuredBody(n.Body, nested) {
				return false
			}
		case *hir.LoopNode:
			nested := copyVisible(visible)
			nested[n.ID] = true
			if n.Continuation != nil {
				nested[n.Continuation.ID] = true
			}
			if !canEmitStructuredBody(n.Init, copyVisible(visible)) ||
				!canEmitStructuredBody(n.Post, nested) {
				return false
			}
			if !canEmitStructuredBody(n.Body, nested) {
				return false
			}
		case *hir.IfNode:
			if n.Cond == nil || !canEmitStructuredBody(n.Then, copyVisible(visible)) || !canEmitStructuredBody(n.Else, copyVisible(visible)) {
				return false
			}
		case *hir.TableNode:
			for _, c := range n.Cases {
				if !canEmitStructuredBody(c.Body, copyVisible(visible)) {
					return false
				}
			}
			if !canEmitStructuredBody(n.Default, copyVisible(visible)) {
				return false
			}
		case *hir.BrNode:
			if n.TargetID < 0 || !visible[n.TargetID] {
				return false
			}
		case *hir.BrIfNode:
			if n.Cond == nil || n.TargetID < 0 || !visible[n.TargetID] {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func copyVisible(src map[int]bool) map[int]bool {
	dst := make(map[int]bool, len(src)+1)
	for id, ok := range src {
		dst[id] = ok
	}
	return dst
}

func structuredInstructions(body hir.ControlBody) []hir.Instruction {
	var out []hir.Instruction
	var visit func(hir.ControlBody)
	visit = func(nodes hir.ControlBody) {
		for _, node := range nodes {
			switch n := node.(type) {
			case *hir.InstructionNode:
				if n.Instruction != nil {
					out = append(out, n.Instruction)
				}
			case *hir.BlockNode:
				visit(n.Body)
			case *hir.LoopNode:
				visit(n.Init)
				visit(n.Body)
				visit(n.Post)
			case *hir.IfNode:
				visit(n.Then)
				visit(n.Else)
			case *hir.TableNode:
				for _, c := range n.Cases {
					visit(c.Body)
				}
				visit(n.Default)
			}
		}
	}
	visit(body)
	return out
}

func (e *Emitter) emitStructuredSubset(body hir.ControlBody, indent string) {
	e.emitStructuredBody(body, indent, map[int]string{})
}

// emitStructuredBody emits the structured HIR directly as WAT. The labels
// map is lexical: it contains only labels that are valid ancestors at the
// current point. Loop lowering temporarily remaps the loop ID between its
// body (continue -> post) and its post region (back-edge -> loop header).
func (e *Emitter) emitStructuredBody(body hir.ControlBody, indent string, labels map[int]string) {
	for _, node := range body {
		switch n := node.(type) {
		case *hir.InstructionNode:
			e.debugMarkerAt(indent)
			e.instruction(n.Instruction)
		case *hir.BlockNode:
			blockLabels := copyLabels(labels)
			blockLabels[n.ID] = structuredLabel(n.ID)
			e.b.WriteString(indent + "(block " + structuredLabel(n.ID) + "\n")
			e.emitStructuredBody(n.Body, indent+"  ", blockLabels)
			e.b.WriteString(indent + ")\n")
		case *hir.LoopNode:
			e.emitStructuredLoop(n, indent, labels)
		case *hir.TableNode:
			e.emitStructuredTable(n, indent, labels)
		case *hir.IfNode:
			e.b.WriteString(indent + "(if " + e.val(n.Cond) + "\n")
			e.b.WriteString(indent + "  (then\n")
			e.emitStructuredBody(n.Then, indent+"    ", copyLabels(labels))
			e.b.WriteString(indent + "  )\n")
			if len(n.Else) > 0 {
				e.b.WriteString(indent + "  (else\n")
				e.emitStructuredBody(n.Else, indent+"    ", copyLabels(labels))
				e.b.WriteString(indent + "  )\n")
			}
			e.b.WriteString(indent + ")\n")
		case *hir.BrNode:
			e.b.WriteString(indent + "(br " + resolveStructuredLabel(labels, n.TargetID) + ")\n")
		case *hir.BrIfNode:
			e.b.WriteString(indent + "(br_if " + resolveStructuredLabel(labels, n.TargetID) + " " + e.val(n.Cond) + ")\n")
		}
	}
}

func (e *Emitter) emitStructuredTable(n *hir.TableNode, indent string, parent map[int]string) {
	exitLabel := "$hir_table_exit_" + itoa(n.ID)
	caseLabels := make([]string, len(n.Cases))
	for i := range caseLabels {
		caseLabels[i] = "$hir_table_case_" + itoa(n.ID) + "_" + itoa(i)
	}
	defaultLabel := "$hir_table_default_" + itoa(n.ID)

	// All dispatch destinations must be active ancestors at the br_table. The
	// blocks are therefore nested in reverse execution order; each case body
	// is emitted after its corresponding block and explicitly exits the table
	// to prevent fallthrough into the next case.
	e.b.WriteString(indent + "(block " + exitLabel + "\n")
	for i := range caseLabels {
		label := caseLabels[i]
		e.b.WriteString(indent + "  (block " + label + "\n")
	}
	e.b.WriteString(indent + "    (block " + defaultLabel + "\n")
	targets := make([]string, len(n.Targets))
	for i, target := range n.Targets {
		if target >= 0 && target < len(caseLabels) {
			targets[i] = caseLabels[target]
		} else {
			targets[i] = defaultLabel
		}
	}
	if len(targets) == 0 {
		targets = []string{defaultLabel}
	}
	fmtTargets := strings.Join(targets, " ")
	e.b.WriteString(indent + "      (br_table " + fmtTargets + " " + e.val(n.IndexValue) + ")\n")
	e.b.WriteString(indent + "    )\n")
	defaultLabels := copyLabels(parent)
	e.emitStructuredBody(n.Default, indent+"    ", defaultLabels)
	e.b.WriteString(indent + "    (br " + exitLabel + ")\n")

	for i := len(n.Cases) - 1; i >= 0; i-- {
		caseLabelsForBody := copyLabels(parent)
		e.emitStructuredBody(n.Cases[i].Body, indent+"    ", caseLabelsForBody)
		e.b.WriteString(indent + "    (br " + exitLabel + ")\n")
		e.b.WriteString(indent + "  )\n")
	}
	e.b.WriteString(indent + ")\n")
}

func (e *Emitter) emitStructuredLoop(n *hir.LoopNode, indent string, parent map[int]string) {
	exitLabel := structuredLoopExitLabel(n.ID)
	loopLabel := structuredLabel(n.ID)
	postLabel := structuredLoopPostLabel(n.ID)

	// The continuation block is a sibling in HIR, but a break in Wasm must
	// target an active ancestor. Map the loop's continuation ID to this
	// synthesized exit block while emitting the loop body.
	bodyLabels := copyLabels(parent)
	if n.Continuation != nil {
		bodyLabels[n.Continuation.ID] = exitLabel
	}
	bodyLabels[n.ID] = postLabel

	e.b.WriteString(indent + "(block " + exitLabel + "\n")
	e.b.WriteString(indent + "  (loop " + loopLabel + "\n")
	if len(n.Init) > 0 {
		e.emitStructuredBody(n.Init, indent+"    ", parent)
	}
	// The canonical body already contains the condition IfNode. Keeping the
	// condition in the body avoids evaluating it twice.
	e.b.WriteString(indent + "    (block " + postLabel + "\n")
	e.emitStructuredBody(n.Body, indent+"      ", bodyLabels)
	e.b.WriteString(indent + "    )\n")

	postLabels := copyLabels(parent)
	if n.Continuation != nil {
		postLabels[n.Continuation.ID] = exitLabel
	}
	postLabels[n.ID] = loopLabel
	e.emitStructuredBody(n.Post, indent+"    ", postLabels)
	e.b.WriteString(indent + "    (br " + loopLabel + ")\n")
	e.b.WriteString(indent + "  )\n")
	e.b.WriteString(indent + ")\n")
}

func copyLabels(src map[int]string) map[int]string {
	dst := make(map[int]string, len(src)+1)
	for id, label := range src {
		dst[id] = label
	}
	return dst
}

func resolveStructuredLabel(labels map[int]string, id int) string {
	if label, ok := labels[id]; ok {
		return label
	}
	return structuredLabel(id)
}

func (e *Emitter) debugMarkerAt(indent string) {
	if e.debugInfo {
		e.b.WriteString(indent + "(block (nop))\n")
	}
}

func structuredLabel(id int) string {
	return "$hir_control_" + itoa(id)
}

func structuredLoopExitLabel(id int) string {
	return "$hir_loop_exit_" + itoa(id)
}

func structuredLoopPostLabel(id int) string {
	return "$hir_loop_post_" + itoa(id)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
