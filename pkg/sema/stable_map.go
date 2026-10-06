package sema

import (
	"fmt"

	"hikec-go/pkg/ast"
)

// validateStableMaps checks the parts of stable-map semantics that cannot be
// expressed by the ordinary map type checker: a complete literal initializer,
// a fixed key set, and compile-time rejection of insert/delete operations.
func validateStableMaps(prog *ast.Program, ctx *Context) error {
	keyOf := func(expr ast.Expression) (string, bool) {
		switch k := expr.(type) {
		case *ast.StringLiteral:
			return "s:" + k.Value, true
		case *ast.IntegerLiteral:
			return fmt.Sprintf("i:%d", k.Value), true
		case *ast.CharLiteral:
			return "c:" + k.Value, true
		default:
			return "", false
		}
	}

	keysFrom := func(lit *ast.MapLiteral) (map[string]int, error) {
		keys := make(map[string]int, len(lit.Entries))
		for index, entry := range lit.Entries {
			key, ok := keyOf(entry.Key)
			if !ok {
				return nil, fmt.Errorf("line %d:%d: stable map keys must be compile-time literals", lit.Token.Line, lit.Token.Col)
			}
			if _, exists := keys[key]; exists {
				return nil, fmt.Errorf("line %d:%d: duplicate key in stable map initializer", lit.Token.Line, lit.Token.Col)
			}
			keys[key] = index
		}
		return keys, nil
	}

	register := func(name string, typ Type, value ast.Expression, dst map[string]map[string]int) error {
		mp, ok := typ.(*MapType)
		if !ok || !mp.Stable {
			return nil
		}
		lit, ok := value.(*ast.MapLiteral)
		if !ok || lit.Type == nil || !lit.Type.Stable {
			return fmt.Errorf("stable map %q must be initialized with a complete stable map literal", name)
		}
		keys, err := keysFrom(lit)
		if err != nil {
			return err
		}
		dst[name] = keys
		return nil
	}

	for _, decl := range prog.Decls {
		if vd, ok := decl.(*ast.VarDecl); ok {
			if err := register(vd.Name.Value, ctx.Globals[vd.Name.Value], vd.Value, ctx.StableMapKeys); err != nil {
				return err
			}
		}
	}

	var checkExpr func(ast.Expression, map[string]Type, map[string]map[string]int) error
	checkExpr = func(expr ast.Expression, locals map[string]Type, localKeys map[string]map[string]int) error {
		if expr == nil {
			return nil
		}
		lookup := func(name string) (Type, map[string]int) {
			typ := locals[name]
			keys := localKeys[name]
			if typ == nil {
				typ = ctx.Globals[name]
				keys = ctx.StableMapKeys[name]
			}
			return typ, keys
		}
		switch n := expr.(type) {
		case *ast.IndexExpr:
			if id, ok := n.Left.(*ast.Identifier); ok {
				if typ, keys := lookup(id.Value); typ != nil {
					if mp, stable := typ.(*MapType); stable && mp.Stable {
						key, literal := keyOf(n.Index)
						if !literal {
							return fmt.Errorf("line %d:%d: stable map access requires a declared compile-time key", n.Token.Line, n.Token.Col)
						}
						if _, exists := keys[key]; !exists {
							return fmt.Errorf("line %d:%d: key is not declared in stable map %q", n.Token.Line, n.Token.Col, id.Value)
						}
					}
				}
			}
			return checkExpr(n.Left, locals, localKeys)
		case *ast.CallExpr:
			if id, ok := n.Function.(*ast.Identifier); ok && (id.Value == "delete" || id.Value == "insert") && len(n.Args) > 0 {
				if arg, ok := n.Args[0].(*ast.Identifier); ok {
					if typ, _ := lookup(arg.Value); typ != nil {
						if mp, stable := typ.(*MapType); stable && mp.Stable {
							return fmt.Errorf("line %d:%d: %s is not allowed for stable map %q", n.Token.Line, n.Token.Col, id.Value, arg.Value)
						}
					}
				}
			}
			for _, arg := range n.Args {
				if err := checkExpr(arg, locals, localKeys); err != nil {
					return err
				}
			}
		case *ast.MapLiteral:
			for _, entry := range n.Entries {
				if err := checkExpr(entry.Key, locals, localKeys); err != nil {
					return err
				}
				if err := checkExpr(entry.Value, locals, localKeys); err != nil {
					return err
				}
			}
		}
		return nil
	}

	var checkStmt func(ast.Statement, map[string]Type, map[string]map[string]int) error
	checkStmt = func(stmt ast.Statement, locals map[string]Type, localKeys map[string]map[string]int) error {
		if stmt == nil {
			return nil
		}
		switch n := stmt.(type) {
		case *ast.VarDecl:
			typ := ctx.InferExprType(n.Value, locals)
			if n.Type != nil {
				typ = ctx.ResolveType(n.Type)
			}
			if err := register(n.Name.Value, typ, n.Value, localKeys); err != nil {
				return err
			}
			locals[n.Name.Value] = typ
			return checkExpr(n.Value, locals, localKeys)
		case *ast.AssignStmt:
			for _, left := range n.Left {
				if idx, ok := left.(*ast.IndexExpr); ok {
					if err := checkExpr(idx, locals, localKeys); err != nil {
						return err
					}
				}
			}
			for _, right := range n.Right {
				if err := checkExpr(right, locals, localKeys); err != nil {
					return err
				}
			}
		case *ast.ExprStmt:
			return checkExpr(n.Expr, locals, localKeys)
		case *ast.BlockStmt:
			if n == nil {
				return nil
			}
			for _, child := range n.Statements {
				if err := checkStmt(child, locals, localKeys); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, decl := range prog.Decls {
		switch n := decl.(type) {
		case *ast.FuncDecl:
			locals, localKeys := make(map[string]Type), make(map[string]map[string]int)
			if err := checkStmt(n.Body, locals, localKeys); err != nil {
				return err
			}
		case *ast.CFuncDecl:
			locals, localKeys := make(map[string]Type), make(map[string]map[string]int)
			if err := checkStmt(n.Body, locals, localKeys); err != nil {
				return err
			}
		}
	}
	return nil
}
