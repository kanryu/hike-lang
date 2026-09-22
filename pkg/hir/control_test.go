package hir

import (
	"strings"
	"testing"

	"hikec-go/pkg/sema"
)

func TestStructuredControlBodyString(t *testing.T) {
	cond := &ConstBool{Val: true, Typ: sema.TypeBool}
	body := ControlBody{
		&IfNode{
			Cond: cond,
			Then: ControlBody{&ReturnNode{Values: []Value{&ConstInt{Val: 1, Typ: sema.TypeInt}}}},
			Else: ControlBody{&ReturnNode{Values: []Value{&ConstInt{Val: 0, Typ: sema.TypeInt}}}},
		},
	}
	text := body.String()
	if !strings.Contains(text, "if true") || !strings.Contains(text, "return 1") {
		t.Fatalf("unexpected structured HIR: %s", text)
	}
}

func TestValidateStructuredBranchDepth(t *testing.T) {
	valid := ControlBody{
		&BlockNode{Label: "exit", Body: ControlBody{
			&LoopNode{Label: "continue", Body: ControlBody{
				&BrIfNode{Cond: &ConstBool{Val: true, Typ: sema.TypeBool}, Target: "continue"},
				&BrNode{Target: "exit"},
			}},
		}},
	}
	if err := ValidateControlBody(valid); err != nil {
		t.Fatalf("valid structured body rejected: %v", err)
	}

	invalid := ControlBody{&BrNode{Target: "missing"}}
	if err := ValidateControlBody(invalid); err == nil {
		t.Fatal("branch outside a structured construct was accepted")
	}
}
