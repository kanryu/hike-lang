package transform

import (
	"fmt"

	"hikec-go/pkg/ast"
)

// ValidateConcreteProgram verifies that Transform removed all generic
// declarations and unresolved generic instantiations.  This is an explicit
// AST visitor rather than a reflect-based walker so the transform package can
// be compiled by the Go-Hike self-hosting path without a reflection runtime.
func ValidateConcreteProgram(program *ast.Program) error {
	if program == nil {
		return fmt.Errorf("concrete AST is nil")
	}
	for i, decl := range program.Decls {
		if err := validateDecl(decl, fmt.Sprintf("program.decls[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}

func validateDecl(decl ast.Decl, path string) error {
	if decl == nil {
		return nil
	}
	switch n := decl.(type) {
	case nil:
		return nil
	case *ast.ConstDecl:
		return validateExpr(n.Value, path+".value")
	case *ast.VarDecl:
		return validateVar(n, path)
	case *ast.AssignStmt:
		return validateAssign(n, path)
	case *ast.MemoryBlockDecl:
		if err := validateExpr(n.Size, path+".size"); err != nil {
			return err
		}
		for i, v := range n.Vars {
			if err := validateVar(v, fmt.Sprintf("%s.vars[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.TypeDecl:
		if len(n.TypeParams) != 0 {
			return fmt.Errorf("generic type remains at %s", path)
		}
		return validateType(n.Type, path+".type")
	case *ast.FuncDecl:
		if len(n.TypeParams) != 0 {
			return fmt.Errorf("generic function remains at %s", path)
		}
		if err := validateParam(n.Receiver, path+".receiver"); err != nil {
			return err
		}
		for i, p := range n.Params {
			if err := validateParam(p, fmt.Sprintf("%s.params[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, t := range n.ReturnTypes {
			if err := validateType(t, fmt.Sprintf("%s.returns[%d]", path, i)); err != nil {
				return err
			}
		}
		return validateBlock(n.Body, path+".body")
	case *ast.CFuncDecl:
		for i, p := range n.Params {
			if err := validateParam(p, fmt.Sprintf("%s.params[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, t := range n.ReturnTypes {
			if err := validateType(t, fmt.Sprintf("%s.returns[%d]", path, i)); err != nil {
				return err
			}
		}
		return validateBlock(n.Body, path+".body")
	case *ast.ExternFuncDecl:
		for i, p := range n.Params {
			if err := validateParam(p, fmt.Sprintf("%s.params[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, t := range n.ReturnTypes {
			if err := validateType(t, fmt.Sprintf("%s.returns[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.JFuncDecl:
		for i, p := range n.Params {
			if err := validateParam(p, fmt.Sprintf("%s.params[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, t := range n.ReturnTypes {
			if err := validateType(t, fmt.Sprintf("%s.returns[%d]", path, i)); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported declaration node %T at %s", decl, path)
	}
	return nil
}

func validateVar(n *ast.VarDecl, path string) error {
	if n == nil {
		return nil
	}
	if err := validateType(n.Type, path+".type"); err != nil {
		return err
	}
	return validateExpr(n.Value, path+".value")
}

func validateParam(n *ast.ParamDecl, path string) error {
	if n == nil {
		return nil
	}
	if err := validateType(n.Type, path+".type"); err != nil {
		return err
	}
	return validateExpr(n.Default, path+".default")
}

func validateBlock(n *ast.BlockStmt, path string) error {
	if n == nil {
		return nil
	}
	for i, s := range n.Statements {
		if err := validateStmt(s, fmt.Sprintf("%s.statements[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateStmt(s ast.Statement, path string) error {
	if s == nil {
		return nil
	}
	switch n := s.(type) {
	case nil:
		return nil
	case *ast.VarDecl:
		return validateVar(n, path)
	case *ast.AssignStmt:
		return validateAssign(n, path)
	case *ast.ExprStmt:
		return validateExpr(n.Expr, path+".expr")
	case *ast.ReturnStmt:
		for i, e := range n.Values {
			if err := validateExpr(e, fmt.Sprintf("%s.values[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.DeferStmt:
		return validateExpr(n.Call, path+".call")
	case *ast.LockStmt:
		return validateBlock(n.Body, path+".body")
	case *ast.AreaStmt:
		if err := validateExpr(n.Size, path+".size"); err != nil {
			return err
		}
		return validateBlock(n.Body, path+".body")
	case *ast.SendStmt:
		if err := validateExpr(n.Chan, path+".chan"); err != nil {
			return err
		}
		return validateExpr(n.Value, path+".value")
	case *ast.BlockStmt:
		return validateBlock(n, path)
	case *ast.IfStmt:
		if err := validateStmt(n.Init, path+".init"); err != nil {
			return err
		}
		if err := validateExpr(n.Condition, path+".condition"); err != nil {
			return err
		}
		if err := validateBlock(n.Consequence, path+".consequence"); err != nil {
			return err
		}
		return validateStmt(n.Alternative, path+".alternative")
	case *ast.ForStmt:
		if err := validateStmt(n.Init, path+".init"); err != nil {
			return err
		}
		if err := validateExpr(n.Cond, path+".condition"); err != nil {
			return err
		}
		if err := validateStmt(n.Post, path+".post"); err != nil {
			return err
		}
		return validateBlock(n.Body, path+".body")
	case *ast.ForRangeStmt:
		if err := validateExpr(n.Key, path+".key"); err != nil {
			return err
		}
		if err := validateExpr(n.Value, path+".value"); err != nil {
			return err
		}
		if err := validateExpr(n.X, path+".range"); err != nil {
			return err
		}
		return validateBlock(n.Body, path+".body")
	case *ast.SwitchStmt:
		if err := validateStmt(n.Init, path+".init"); err != nil {
			return err
		}
		if err := validateExpr(n.Value, path+".value"); err != nil {
			return err
		}
		for i, c := range n.Cases {
			if err := validateCase(c, fmt.Sprintf("%s.cases[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.TypeSwitchStmt:
		if err := validateStmt(n.Init, path+".init"); err != nil {
			return err
		}
		if err := validateExpr(n.Expr, path+".expr"); err != nil {
			return err
		}
		for i, c := range n.Cases {
			if err := validateTypeCase(c, fmt.Sprintf("%s.cases[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.CaseClause:
		return validateCase(n, path)
	case *ast.TypeCaseClause:
		return validateTypeCase(n, path)
	case *ast.BreakStmt, *ast.ContinueStmt:
		return nil
	default:
		return fmt.Errorf("unsupported statement node %T at %s", s, path)
	}
	return nil
}

func validateAssign(n *ast.AssignStmt, path string) error {
	if n == nil {
		return nil
	}
	if err := validateType(n.Type, path+".type"); err != nil {
		return err
	}
	for i, e := range n.Left {
		if err := validateExpr(e, fmt.Sprintf("%s.left[%d]", path, i)); err != nil {
			return err
		}
	}
	for i, e := range n.Right {
		if err := validateExpr(e, fmt.Sprintf("%s.right[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateCase(n *ast.CaseClause, path string) error {
	if n == nil {
		return nil
	}
	for i, e := range n.Values {
		if err := validateExpr(e, fmt.Sprintf("%s.values[%d]", path, i)); err != nil {
			return err
		}
	}
	for i, s := range n.Body {
		if err := validateStmt(s, fmt.Sprintf("%s.body[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateTypeCase(n *ast.TypeCaseClause, path string) error {
	if n == nil {
		return nil
	}
	for i, t := range n.Types {
		if err := validateType(t, fmt.Sprintf("%s.types[%d]", path, i)); err != nil {
			return err
		}
	}
	for i, s := range n.Body {
		if err := validateStmt(s, fmt.Sprintf("%s.body[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateExpr(e ast.Expression, path string) error {
	// Optional expression fields are represented by a nil interface when they
	// are absent.  Check that case before entering the typed-nil switch below.
	// This also keeps the Go-Hike backend from dispatching a type switch on a
	// completely nil interface value.
	if e == nil {
		return nil
	}
	if isNilExpr(e) {
		return nil
	}
	switch n := e.(type) {
	case nil:
		return nil
	case *ast.GenericInstExpr:
		return fmt.Errorf("unresolved generic instantiation at %s (line %d, token %q)", path, n.Token.Line, n.Token.Literal)
	case *ast.ImplicitCastExpr:
		if err := validateExpr(n.Expr, path+".expr"); err != nil {
			return err
		}
		return validateType(n.TargetType, path+".target")
	case *ast.PrefixExpr:
		return validateExpr(n.Right, path+".right")
	case *ast.ReceiveExpr:
		return validateExpr(n.Expr, path+".expr")
	case *ast.AsyncExpr:
		return validateExpr(n.Fn, path+".fn")
	case *ast.BinaryExpr:
		if err := validateExpr(n.Left, path+".left"); err != nil {
			return err
		}
		return validateExpr(n.Right, path+".right")
	case *ast.IndexExpr:
		if err := validateExpr(n.Left, path+".left"); err != nil {
			return err
		}
		return validateExpr(n.Index, path+".index")
	case *ast.MemberExpr:
		return validateExpr(n.Object, path+".object")
	case *ast.CallExpr:
		if err := validateExpr(n.Function, path+".function"); err != nil {
			return err
		}
		for i, a := range n.Args {
			if err := validateExpr(a, fmt.Sprintf("%s.args[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.InlineAsmExpr:
		for i, o := range n.Operands {
			if err := validateExpr(o, fmt.Sprintf("%s.operands[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.SliceExpr:
		if err := validateExpr(n.Left, path+".left"); err != nil {
			return err
		}
		if err := validateExpr(n.Low, path+".low"); err != nil {
			return err
		}
		return validateExpr(n.High, path+".high")
	case *ast.SliceLiteral:
		if err := validateType(n.Type, path+".type"); err != nil {
			return err
		}
		for i, v := range n.Elements {
			if err := validateExpr(v, fmt.Sprintf("%s.elements[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.StructLiteral:
		if err := validateType(n.Type, path+".type"); err != nil {
			return err
		}
		for i, f := range n.Fields {
			if f != nil {
				if err := validateExpr(f.Value, fmt.Sprintf("%s.fields[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case *ast.ArrayLiteral:
		if err := validateType(n.Type, path+".type"); err != nil {
			return err
		}
		for i, v := range n.Elements {
			if err := validateExpr(v, fmt.Sprintf("%s.elements[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.MapLiteral:
		if err := validateType(n.Type, path+".type"); err != nil {
			return err
		}
		for i, entry := range n.Entries {
			if entry != nil {
				if err := validateExpr(entry.Key, fmt.Sprintf("%s.entries[%d].key", path, i)); err != nil {
					return err
				}
				if err := validateExpr(entry.Value, fmt.Sprintf("%s.entries[%d].value", path, i)); err != nil {
					return err
				}
			}
		}
	case *ast.TypeAssertExpr:
		if err := validateExpr(n.Expr, path+".expr"); err != nil {
			return err
		}
		return validateType(n.Target, path+".target")
	case *ast.FuncLit:
		for i, p := range n.Params {
			if err := validateParam(p, fmt.Sprintf("%s.params[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, t := range n.ReturnTypes {
			if err := validateType(t, fmt.Sprintf("%s.returns[%d]", path, i)); err != nil {
				return err
			}
		}
		return validateBlock(n.Body, path+".body")
	case *ast.NamedType:
		return validateType(n, path)
	case *ast.PointerType, *ast.SliceType, *ast.EllipsisType, *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FutureType, *ast.InterfaceType, *ast.FuncType, *ast.ConstArg:
		return validateType(n.(ast.TypeExpr), path)
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.CharLiteral, *ast.StringLiteral, *ast.NilLiteral, *ast.IotaExpr:
		return nil
	default:
		return fmt.Errorf("unsupported expression node %T at %s", e, path)
	}
	return nil
}

func validateType(t ast.TypeExpr, path string) error {
	if t == nil {
		return nil
	}
	if isNilType(t) {
		return nil
	}
	switch n := t.(type) {
	case nil:
		return nil
	case *ast.NamedType:
		for i, a := range n.TypeArgs {
			if err := validateType(a, fmt.Sprintf("%s.args[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.PointerType:
		return validateType(n.Base, path+".base")
	case *ast.StructType:
		for i, f := range n.Fields {
			if f != nil {
				if err := validateType(f.Type, fmt.Sprintf("%s.fields[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case *ast.SliceType:
		return validateType(n.Elem, path+".elem")
	case *ast.EllipsisType:
		return validateType(n.Elem, path+".elem")
	case *ast.ArrayType:
		return validateType(n.Elem, path+".elem")
	case *ast.MapType:
		if err := validateType(n.Key, path+".key"); err != nil {
			return err
		}
		return validateType(n.Value, path+".value")
	case *ast.ChanType:
		return validateType(n.Elem, path+".elem")
	case *ast.FutureType:
		for i, r := range n.ReturnTypes {
			if err := validateType(r, fmt.Sprintf("%s.returns[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.InterfaceType:
		for i, e := range n.Embedded {
			if err := validateType(e, fmt.Sprintf("%s.embedded[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, m := range n.Methods {
			if m != nil {
				for j, p := range m.ParamTypes {
					if err := validateType(p, fmt.Sprintf("%s.methods[%d].params[%d]", path, i, j)); err != nil {
						return err
					}
				}
				for j, r := range m.ReturnTypes {
					if err := validateType(r, fmt.Sprintf("%s.methods[%d].returns[%d]", path, i, j)); err != nil {
						return err
					}
				}
			}
		}
	case *ast.FuncType:
		for i, p := range n.ParamTypes {
			if err := validateType(p, fmt.Sprintf("%s.params[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, r := range n.ReturnTypes {
			if err := validateType(r, fmt.Sprintf("%s.returns[%d]", path, i)); err != nil {
				return err
			}
		}
	case *ast.TypeParam:
		return validateType(n.Constraint, path+".constraint")
	case *ast.ConstArg:
		return validateExpr(n.Expr, path+".expr")
	default:
		return fmt.Errorf("unsupported type node %T at %s", t, path)
	}
	return nil
}

// Interfaces containing a typed nil pointer are possible in the AST because
// optional type and expression fields use interface types.  Keep those cases
// out of the visitor without using reflect.Value.IsNil.
func isNilExpr(e ast.Expression) bool {
	switch n := e.(type) {
	case *ast.Identifier:
		return n == nil
	case *ast.IntegerLiteral:
		return n == nil
	case *ast.FloatLiteral:
		return n == nil
	case *ast.CharLiteral:
		return n == nil
	case *ast.StringLiteral:
		return n == nil
	case *ast.NilLiteral:
		return n == nil
	case *ast.PrefixExpr:
		return n == nil
	case *ast.ReceiveExpr:
		return n == nil
	case *ast.AsyncExpr:
		return n == nil
	case *ast.BinaryExpr:
		return n == nil
	case *ast.IndexExpr:
		return n == nil
	case *ast.GenericInstExpr:
		return n == nil
	case *ast.MemberExpr:
		return n == nil
	case *ast.CallExpr:
		return n == nil
	case *ast.InlineAsmExpr:
		return n == nil
	case *ast.IotaExpr:
		return n == nil
	case *ast.SliceExpr:
		return n == nil
	case *ast.SliceLiteral:
		return n == nil
	case *ast.StructLiteral:
		return n == nil
	case *ast.ArrayLiteral:
		return n == nil
	case *ast.MapLiteral:
		return n == nil
	case *ast.TypeAssertExpr:
		return n == nil
	case *ast.FuncLit:
		return n == nil
	case *ast.NamedType:
		return n == nil
	case *ast.PointerType:
		return n == nil
	case *ast.SliceType:
		return n == nil
	case *ast.EllipsisType:
		return n == nil
	case *ast.ArrayType:
		return n == nil
	case *ast.MapType:
		return n == nil
	case *ast.ChanType:
		return n == nil
	case *ast.FutureType:
		return n == nil
	case *ast.InterfaceType:
		return n == nil
	case *ast.FuncType:
		return n == nil
	case *ast.ConstArg:
		return n == nil
	default:
		return false
	}
}

func isNilType(t ast.TypeExpr) bool {
	switch n := t.(type) {
	case *ast.NamedType:
		return n == nil
	case *ast.PointerType:
		return n == nil
	case *ast.StructType:
		return n == nil
	case *ast.SliceType:
		return n == nil
	case *ast.EllipsisType:
		return n == nil
	case *ast.ArrayType:
		return n == nil
	case *ast.MapType:
		return n == nil
	case *ast.ChanType:
		return n == nil
	case *ast.FutureType:
		return n == nil
	case *ast.InterfaceType:
		return n == nil
	case *ast.FuncType:
		return n == nil
	case *ast.TypeParam:
		return n == nil
	case *ast.ConstArg:
		return n == nil
	default:
		return false
	}
}
