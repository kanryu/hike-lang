package transform

import (
	"testing"

	"hikec-go/pkg/ast"
)

func TestValidateExprAcceptsTypeExpressions(t *testing.T) {
	expr := &ast.SliceType{}
	if err := validateExpr(expr, "expr"); err != nil {
		t.Fatalf("validateExpr(%T) failed: %v", expr, err)
	}
}
