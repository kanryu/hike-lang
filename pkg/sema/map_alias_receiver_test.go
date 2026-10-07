package sema

import (
	"testing"

	"hikec-go/pkg/ast"
)

func TestResolveFuncDeclTypeUsesRegisteredMethodBeforeIncompleteAlias(t *testing.T) {
	ctx := NewContext()
	var incompleteValue *BasicType
	ctx.Aliases["Number"] = &MapType{Key: TypeString, Value: incompleteValue}

	method := &FuncType{Name: "Number#Format", IsMethod: true}
	ctx.RegisterMethod("Number", "Format", method)

	decl := &ast.FuncDecl{
		Name: &ast.Identifier{Value: "Format"},
		Receiver: &ast.ParamDecl{
			Name: &ast.Identifier{Value: "n"},
			Type: &ast.NamedType{Name: &ast.Identifier{Value: "Number"}},
		},
	}

	resolveFuncDeclType(decl, ctx)
	if len(method.ParamTypes) != 1 || method.ParamTypes[0] != ctx.Aliases["Number"] {
		t.Fatalf("method receiver type was not resolved from the registered method")
	}
}
