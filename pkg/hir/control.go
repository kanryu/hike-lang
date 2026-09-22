package hir

import (
	"fmt"
	"strings"

	"hikec-go/pkg/sema"
)

// ControlNode is the structured-control counterpart of the CFG terminators.
//
// Structured control is lowered to transient backend blocks only when a
// backend requires them. Every structured construct owns a unique label;
// branch nodes refer to that label and the backend resolves it to its target
// representation.
type ControlNode interface {
	String() string
}

// ControlElement is a label-bearing structured control node. Its ID is
// assigned by the owning function and equals its index in ControlNodes.
type ControlElement interface {
	ControlNode
	Depth() int
	Index() int
	Next() int
	BreakTarget() int
	ContinueTarget() int
	SetControlPosition(index, depth int)
	SetControlLinks(next, breakTarget, continueTarget int)
}

type controlLinks struct {
	next           int
	breakTarget    int
	continueTarget int
}

func (l *controlLinks) Next() int           { return l.next }
func (l *controlLinks) BreakTarget() int    { return l.breakTarget }
func (l *controlLinks) ContinueTarget() int { return l.continueTarget }
func (l *controlLinks) SetControlLinks(next, breakTarget, continueTarget int) {
	l.next, l.breakTarget, l.continueTarget = next, breakTarget, continueTarget
}

// ControlBody is a sequence of structured instructions and control nodes.
// Values produced by ordinary HIR instructions remain in the sequence through
// InstructionNode; this keeps the structured layer orthogonal to the existing
// register/value model.
type ControlBody []ControlNode

func (b ControlBody) String() string {
	var out strings.Builder
	for _, node := range b {
		if node == nil {
			continue
		}
		out.WriteString(node.String())
		out.WriteByte('\n')
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// InstructionNode embeds a normal HIR instruction in a structured body.
type InstructionNode struct {
	Instruction Instruction
}

func (n *InstructionNode) String() string {
	if n == nil || n.Instruction == nil {
		return ""
	}
	return n.Instruction.String()
}

// BlockNode introduces a named structured block.  The name is useful for
// diagnostics; branches use the target Index and Depth metadata to be lowered
// directly to br/br_if/br_table.
type BlockNode struct {
	controlLinks
	ID                      int
	ControlDepth            int
	Label                   string
	FunctionExit            bool
	ContinuationPlaceholder bool
	ContinuationOf          ControlElement
	ReplacedByID            int
	ResultTypes             []sema.Type
	Body                    ControlBody
}

func (n *BlockNode) Depth() int                       { return n.ControlDepth }
func (n *BlockNode) Index() int                       { return n.ID }
func (n *BlockNode) SetControlPosition(id, depth int) { n.ID, n.ControlDepth = id, depth }

func (n *BlockNode) String() string {
	if n.FunctionExit {
		return fmt.Sprintf("  block %s (function-exit)", n.Label)
	}
	return fmt.Sprintf("  block %s {\n%s\n  }", n.Label, indentControlBody(n.Body))
}

// LoopNode introduces a structured loop. Continue and break targets refer to
// control-element indices; a backend derives relative depth from metadata.
type LoopNode struct {
	controlLinks
	ID           int
	ControlDepth int
	Label        string
	ResultTypes  []sema.Type
	// Init and Condition allow source-level for variants to normalize to one
	// loop shape. A nil Condition represents an infinite loop.
	Init      ControlBody
	Condition Value
	// Body, Post, Exit, and Continuation form the canonical loop layout.
	// Post is executed by continue and after each body iteration; Exit is the
	// loop-local merge point; Continuation is the first node after the loop.
	// BodyExit is the explicit boundary from Body to Post.
	BodyExit     *BlockNode
	Body         ControlBody
	Post         ControlBody
	Exit         *BlockNode
	Continuation *BlockNode
}

func (n *LoopNode) Depth() int                       { return n.ControlDepth }
func (n *LoopNode) Index() int                       { return n.ID }
func (n *LoopNode) SetControlPosition(id, depth int) { n.ID, n.ControlDepth = id, depth }

func (n *LoopNode) String() string {
	condition := "<none>"
	if n.Condition != nil {
		condition = n.Condition.String()
	}
	return fmt.Sprintf("  loop %s {\n  init:\n%s\n  condition: %s\n  body:\n%s\n  post:\n%s\n  }", n.Label, indentControlBody(n.Init), condition, indentControlBody(n.Body), indentControlBody(n.Post))
}

type IfNode struct {
	controlLinks
	ID           int
	ControlDepth int
	Label        string
	Cond         Value
	ResultTypes  []sema.Type
	Then         ControlBody
	Else         ControlBody
}

func (n *IfNode) Depth() int                       { return n.ControlDepth }
func (n *IfNode) Index() int                       { return n.ID }
func (n *IfNode) SetControlPosition(id, depth int) { n.ID, n.ControlDepth = id, depth }

func (n *IfNode) String() string {
	var out strings.Builder
	label := ""
	if n.Label != "" {
		label = " " + n.Label
	}
	fmt.Fprintf(&out, "  if%s %s {\n%s\n  }", label, n.Cond, indentControlBody(n.Then))
	if len(n.Else) > 0 {
		fmt.Fprintf(&out, " else {\n%s\n  }", indentControlBody(n.Else))
	}
	return out.String()
}

type BrNode struct {
	controlLinks
	ID           int
	ControlDepth int
	Target       string
	TargetID     int
	Values       []Value
}

func (n *BrNode) Depth() int                       { return n.ControlDepth }
func (n *BrNode) Index() int                       { return n.ID }
func (n *BrNode) SetControlPosition(id, depth int) { n.ID, n.ControlDepth = id, depth }
func (n *BrNode) String() string {
	if n.TargetID >= 0 {
		return fmt.Sprintf("  br %%control.%d", n.TargetID)
	}
	return fmt.Sprintf("  br %%%s", n.Target)
}

type BrIfNode struct {
	controlLinks
	ID           int
	ControlDepth int
	Cond         Value
	Target       string
	TargetID     int
	Values       []Value
}

func (n *BrIfNode) Depth() int                       { return n.ControlDepth }
func (n *BrIfNode) Index() int                       { return n.ID }
func (n *BrIfNode) SetControlPosition(id, depth int) { n.ID, n.ControlDepth = id, depth }
func (n *BrIfNode) String() string {
	target := n.Target
	if n.TargetID >= 0 {
		target = fmt.Sprintf("control.%d", n.TargetID)
	}
	return fmt.Sprintf("  br_if %s %%%s", n.Cond, target)
}

type BrTableNode struct {
	controlLinks
	ID            int
	ControlDepth  int
	IndexValue    Value
	Targets       []string
	TargetIDs     []int
	Default       string
	DefaultTarget int
}

// TableCase is one destination body of a dense integer switch table.
type TableCase struct {
	Values []int64
	Body   ControlBody
}

// TableNode represents a dense integer switch. Targets contains case indexes
// for values in [Min, Min+len(Targets)); -1 selects Default.
type TableNode struct {
	controlLinks
	ID            int
	ControlDepth  int
	Label         string
	IndexValue    Value
	Min           int64
	Targets       []int
	DefaultTarget int
	Cases         []TableCase
	Default       ControlBody
}

func (n *TableNode) Depth() int                       { return n.ControlDepth }
func (n *TableNode) Index() int                       { return n.ID }
func (n *TableNode) SetControlPosition(id, depth int) { n.ID, n.ControlDepth = id, depth }
func (n *TableNode) String() string {
	return fmt.Sprintf("  table %s [%d..]", n.IndexValue, n.Min)
}

func (n *BrTableNode) Depth() int                       { return n.ControlDepth }
func (n *BrTableNode) Index() int                       { return n.ID }
func (n *BrTableNode) SetControlPosition(id, depth int) { n.ID, n.ControlDepth = id, depth }

func (n *BrTableNode) String() string {
	targets := make([]string, len(n.Targets))
	for i, target := range n.Targets {
		targets[i] = "%" + target
	}
	return fmt.Sprintf("  br_table %s [%s] %%%s", n.IndexValue, strings.Join(targets, ", "), n.Default)
}

type ReturnNode struct {
	Values []Value
}

func (n *ReturnNode) String() string {
	if len(n.Values) == 0 {
		return "  return"
	}
	values := make([]string, len(n.Values))
	for i, value := range n.Values {
		values[i] = value.String()
	}
	return "  return " + strings.Join(values, ", ")
}

type UnreachableNode struct{}

func (n *UnreachableNode) String() string { return "  unreachable" }

// PanicNode is the structured representation of a dynamic panic terminator.
// SiteID indexes the owning function's immutable PanicSites table.
type PanicNode struct {
	Value  Value
	Cause  Value
	SiteID int
}

func (n *PanicNode) String() string {
	if n.Cause != nil {
		return fmt.Sprintf("  panic %s cause %s (site %d)", n.Value, n.Cause, n.SiteID)
	}
	return fmt.Sprintf("  panic %s (site %d)", n.Value, n.SiteID)
}

// ValidateControlBody checks the structural invariants needed by a Wasm-like
// structured backend.  A branch depth may target any enclosing block or loop;
// a body with no enclosing construct cannot contain a branch.  This validator
// deliberately does not impose a single terminator at the end of every body,
// because fallthrough is valid for block/if bodies and is finalized later by
// the lowering pass.
func ValidateControlBody(body ControlBody) error {
	return validateControlBody(body, nil)
}

func validateControlBody(body ControlBody, visible map[string]bool) error {
	if visible == nil {
		visible = make(map[string]bool)
	}
	for index, node := range body {
		switch n := node.(type) {
		case nil:
			return fmt.Errorf("control node %d is nil", index)
		case *InstructionNode:
			if n.Instruction == nil {
				return fmt.Errorf("instruction node %d contains nil instruction", index)
			}
		case *BlockNode:
			if n.Label == "" || visible[n.Label] {
				return fmt.Errorf("block %q has a missing or duplicate label", n.Label)
			}
			nested := copyControlLabels(visible)
			nested[n.Label] = true
			if err := validateControlBody(n.Body, nested); err != nil {
				return fmt.Errorf("block %q: %w", n.Label, err)
			}
		case *LoopNode:
			if n.Label == "" || visible[n.Label] {
				return fmt.Errorf("loop %q has a missing or duplicate label", n.Label)
			}
			nested := copyControlLabels(visible)
			nested[n.Label] = true
			if err := validateControlBody(n.Body, nested); err != nil {
				return fmt.Errorf("loop %q: %w", n.Label, err)
			}
		case *IfNode:
			if n.Cond == nil {
				return fmt.Errorf("if node %d has nil condition", index)
			}
			if n.Label == "" || visible[n.Label] {
				return fmt.Errorf("if %q has a missing or duplicate label", n.Label)
			}
			nested := copyControlLabels(visible)
			nested[n.Label] = true
			if err := validateControlBody(n.Then, nested); err != nil {
				return fmt.Errorf("if then branch: %w", err)
			}
			if err := validateControlBody(n.Else, nested); err != nil {
				return fmt.Errorf("if else branch: %w", err)
			}
		case *TableNode:
			if n.IndexValue == nil || n.Label == "" || visible[n.Label] {
				return fmt.Errorf("table %q has invalid label or index", n.Label)
			}
			nested := copyControlLabels(visible)
			nested[n.Label] = true
			for _, c := range n.Cases {
				if err := validateControlBody(c.Body, nested); err != nil {
					return fmt.Errorf("table case: %w", err)
				}
			}
			if err := validateControlBody(n.Default, nested); err != nil {
				return fmt.Errorf("table default: %w", err)
			}
		case *BrNode:
			if !visible[n.Target] {
				return fmt.Errorf("branch target %q is not visible", n.Target)
			}
		case *BrIfNode:
			if n.Cond == nil {
				return fmt.Errorf("br_if node %d has nil condition", index)
			}
			if !visible[n.Target] {
				return fmt.Errorf("conditional branch target %q is not visible", n.Target)
			}
		case *BrTableNode:
			if n.IndexValue == nil {
				return fmt.Errorf("br_table node %d has nil index", index)
			}
			for _, target := range append(append([]string{}, n.Targets...), n.Default) {
				if !visible[target] {
					return fmt.Errorf("br_table target %q is not visible", target)
				}
			}
		case *ReturnNode, *UnreachableNode:
			// Valid terminal nodes.
		default:
			return fmt.Errorf("unsupported control node %T", node)
		}
	}
	return nil
}

func copyControlLabels(labels map[string]bool) map[string]bool {
	copy := make(map[string]bool, len(labels)+1)
	for label, present := range labels {
		copy[label] = present
	}
	return copy
}

func indentControlBody(body ControlBody) string {
	text := body.String()
	if text == "" {
		return "  "
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}
