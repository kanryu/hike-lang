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

func TestRegisterHikeFuncStripsPackagePrefixFromMethodName(t *testing.T) {
	ctx := NewContext()
	fn := &ast.FuncDecl{
		Name: &ast.Identifier{Value: "os_Perm"},
		Receiver: &ast.ParamDecl{
			Name: &ast.Identifier{Value: "m"},
			Type: &ast.NamedType{Name: &ast.Identifier{Value: "os_FileMode"}},
		},
	}

	registerHikeFunc(fn, "os", ctx)
	method, _ := ctx.LookupMethod("os_FileMode", "Perm")
	if method == nil || method.Name != "os_FileMode_Perm" || method.IRName != "os_FileMode_Perm" {
		t.Fatalf("registered method = %#v, want unqualified method identity", method)
	}
}

func TestRegisterHikeFuncPreservesPointerReceiverFromNamedSyntax(t *testing.T) {
	ctx := NewContext()
	fn := &ast.FuncDecl{
		Name: &ast.Identifier{Value: "sema_TypeName"},
		Receiver: &ast.ParamDecl{
			Name: &ast.Identifier{Value: "t"},
			Type: &ast.NamedType{Name: &ast.Identifier{Value: "*sema_InterfaceType"}},
		},
	}

	registerHikeFunc(fn, "sema", ctx)
	if method, _ := ctx.LookupMethod("*sema_InterfaceType", "TypeName"); method == nil || method.Name != "sema_InterfaceType_TypeName" || method.IRName != "sema_InterfaceType_ptr_TypeName" {
		t.Fatalf("pointer method identity = %#v, want ptr receiver symbol", method)
	}
}
