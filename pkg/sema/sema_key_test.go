package sema

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/token"
)

func TestBuildInternalKeyUsesReceiverDelimiter(t *testing.T) {
	tests := []struct {
		name     string
		receiver string
		want     string
	}{
		{"function", "", "crypto/Sum"},
		{"value receiver", "Digest", "crypto/Sum@Digest"},
		{"pointer receiver", "*Digest", "crypto/Sum@@Digest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildInternalKey("crypto", "Sum", tt.receiver); got != tt.want {
				t.Fatalf("BuildInternalKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMangleInternalKeyWithReceiver(t *testing.T) {
	tests := map[string]string{
		"crypto/Sum@Digest":  "crypto_Digest_Sum",
		"crypto/Sum@@Digest": "crypto_Digest_ptr_Sum",
	}
	for key, want := range tests {
		if got := MangleInternalKeyToIR(key); got != want {
			t.Errorf("MangleInternalKeyToIR(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestLookupFunctionFromModuleFQDN(t *testing.T) {
	ctx := NewContext()
	shaSum := &FuncType{Name: "sha256/Sum256", InternalKey: "sha256/Sum256"}
	md5Sum := &FuncType{Name: "md5/Sum", InternalKey: "md5/Sum"}
	ctx.Functions[shaSum.Name] = shaSum
	ctx.Functions[md5Sum.Name] = md5Sum

	for name, want := range map[string]*FuncType{
		"sha256/Sum256": shaSum,
		"md5/Sum":       md5Sum,
	} {
		got, canonical := ctx.LookupFunction(name)
		if got != want || canonical != name {
			t.Errorf("LookupFunction(%q) = (%p, %q), want (%p, %q)", name, got, canonical, want, name)
		}
	}
}

func TestLookupMethodFromReceiverFQDN(t *testing.T) {
	ctx := NewContext()
	valueMethod := &FuncType{
		Name:        "pkg/Value@Digest",
		InternalKey: "pkg/Value@Digest",
		IsMethod:    true,
	}
	pointerMethod := &FuncType{
		Name:        "pkg/Value@@Digest",
		InternalKey: "pkg/Value@@Digest",
		IsMethod:    true,
	}
	otherMethod := &FuncType{
		Name:        "other/Value@Digest",
		InternalKey: "other/Value@Digest",
		IsMethod:    true,
	}
	ctx.RegisterMethod("Digest", "Sum", valueMethod)
	ctx.RegisterMethod("*Digest", "Sum", pointerMethod)
	ctx.RegisterMethod("other.Digest", "Sum", otherMethod)

	for _, tt := range []struct {
		receiver string
		want     *FuncType
		key      string
	}{
		{"Digest", valueMethod, "pkg/Value@Digest"},
		{"*Digest", pointerMethod, "pkg/Value@@Digest"},
		{"other.Digest", otherMethod, "other/Value@Digest"},
	} {
		got, key := ctx.LookupMethod(tt.receiver, "Sum")
		if got != tt.want || key != tt.key {
			t.Errorf("LookupMethod(%q, Sum) = (%p, %q), want (%p, %q)", tt.receiver, got, key, tt.want, tt.key)
		}
	}

	if got, _ := ctx.LookupMethod("Digest", "Missing"); got != nil {
		t.Fatalf("LookupMethod returned a method for an unknown method name: %v", got)
	}
}

func TestNestedIndexResolutionThroughIndexableReceivers(t *testing.T) {
	ctx := NewContext()
	horizontal := &StructType{Name: "Horizontal", Fields: []Field{{Name: "data", Type: &PointerType{Base: TypeInt}}}}
	vertical := &StructType{Name: "Vertical", Fields: []Field{{Name: "data", Type: &PointerType{Base: TypeInt}}}}
	ctx.Structs[horizontal.Name] = horizontal
	ctx.Structs[vertical.Name] = vertical

	rowMethod := &FuncType{
		Name:        "matrix/Horizontal@Get",
		InternalKey: "matrix/Horizontal@Get",
		IsMethod:    true,
		ParamTypes:  []Type{&PointerType{Base: horizontal}, TypeInt},
		ReturnTypes: []Type{&PointerType{Base: vertical}},
	}
	cellMethod := &FuncType{
		Name:        "matrix/Vertical@Get",
		InternalKey: "matrix/Vertical@Get",
		IsMethod:    true,
		ParamTypes:  []Type{&PointerType{Base: vertical}, TypeInt},
		ReturnTypes: []Type{TypeInt},
	}
	ctx.RegisterMethod("Horizontal", "Get", rowMethod)
	ctx.RegisterMethod("*Vertical", "Get", cellMethod)

	rowKey, rowType, _, rowFn := ctx.CheckIndexable(horizontal)
	if rowFn != rowMethod || rowKey != TypeInt || rowType.TypeName() != "*Vertical" {
		t.Fatalf("first index resolution = (%v, %v, %v), want (int, *Vertical, Horizontal.Get)", rowKey, rowType, rowFn)
	}

	cellKey, cellType, _, cellFn := ctx.CheckIndexable(rowType)
	if cellFn != cellMethod || cellKey != TypeInt || cellType != TypeInt {
		t.Fatalf("second index resolution = (%v, %v, %v), want (int, int, Vertical.Get)", cellKey, cellType, cellFn)
	}
}

func TestConstGenericArgumentsRequireTrailingConstValues(t *testing.T) {
	ctx := NewContext()
	ctx.Constants["N"] = 8
	ctx.Structs["Matrix"] = &StructType{
		Name:            "Matrix",
		InternalKey:     "matrix/Matrix",
		TypeParams:      []string{"T", "Rows", "Cols"},
		ConstTypeParams: map[string]bool{"Rows": true, "Cols": true},
		Specializations: make(map[string]*StructType),
	}
	intType := &ast.NamedType{Name: &ast.Identifier{Value: "int"}}
	literal := &ast.ConstArg{Expr: &ast.IntegerLiteral{Value: 8}}
	constant := &ast.ConstArg{Expr: &ast.Identifier{Value: "N"}}
	matrixType := &ast.NamedType{
		Token:    token.Token{},
		Name:     &ast.Identifier{Value: "Matrix"},
		TypeArgs: []ast.TypeExpr{intType, literal, constant},
	}
	resolved := ctx.ResolveType(matrixType)
	st, ok := resolved.(*StructType)
	if !ok || len(st.TypeArgs) != 3 {
		t.Fatalf("resolved const generic type = %T, %+v", resolved, resolved)
	}
	if st.InternalKey != "matrix/Matrix@int@8@8" {
		t.Fatalf("specialized internal key = %q, want matrix/Matrix@int@8@8", st.InternalKey)
	}
	if got, ok := st.TypeArgs[1].(*ConstValueType); !ok || got.Value != 8 {
		t.Fatalf("literal const argument = %T, %+v", st.TypeArgs[1], st.TypeArgs[1])
	}
	if got, ok := st.TypeArgs[2].(*ConstValueType); !ok || got.Value != 8 {
		t.Fatalf("const variable argument = %T, %+v", st.TypeArgs[2], st.TypeArgs[2])
	}
}

func TestConstGenericArgumentRestrictions(t *testing.T) {
	newContext := func() *Context {
		ctx := NewContext()
		ctx.Structs["Matrix"] = &StructType{
			Name:            "Matrix",
			TypeParams:      []string{"T", "Rows", "Cols"},
			ConstTypeParams: map[string]bool{"Rows": true, "Cols": true},
			Specializations: make(map[string]*StructType),
		}
		return ctx
	}
	intArg := func() ast.TypeExpr {
		return &ast.NamedType{Name: &ast.Identifier{Value: "int"}}
	}
	constArg := func(value int64) ast.TypeExpr {
		return &ast.ConstArg{Expr: &ast.IntegerLiteral{Value: value}}
	}
	instantiate := func(ctx *Context, args ...ast.TypeExpr) {
		ctx.ResolveType(&ast.NamedType{Name: &ast.Identifier{Value: "Matrix"}, TypeArgs: args})
	}

	t.Run("requires a type argument first", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected a const-first instantiation to fail")
			}
		}()
		instantiate(newContext(), constArg(8), constArg(8))
	})
	t.Run("rejects a non-const variable after const arguments", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected a runtime variable to fail")
			}
		}()
		instantiate(newContext(), intArg(), constArg(8), &ast.NamedType{Name: &ast.Identifier{Value: "runtimeRows"}})
	})
	t.Run("rejects an expression after const arguments", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected a runtime expression to fail")
			}
		}()
		expr := &ast.BinaryExpr{Left: &ast.IntegerLiteral{Value: 7}, Operator: "+", Right: &ast.IntegerLiteral{Value: 1}}
		instantiate(newContext(), intArg(), constArg(8), &ast.ConstArg{Expr: expr})
	})
}

func TestConstGenericDeclarationRequiresUintConstraint(t *testing.T) {
	uintConstraint := &ast.NamedType{Name: &ast.Identifier{Value: "uint"}}
	declared := []*ast.TypeParam{
		{Name: &ast.Identifier{Value: "T"}},
		{Name: &ast.Identifier{Value: "Rows"}, Constraint: uintConstraint},
		{Name: &ast.Identifier{Value: "Cols"}, Constraint: uintConstraint},
	}
	constraints := constTypeParams(declared)
	if !constraints["Rows"] || !constraints["Cols"] {
		t.Fatalf("uint-constrained parameters were not marked as const parameters: %#v", constraints)
	}

	bare := []*ast.TypeParam{{Name: &ast.Identifier{Value: "Rows"}}}
	if got := constTypeParams(bare); got["Rows"] {
		t.Fatal("a bare type parameter must not become a const generic parameter")
	}
}
