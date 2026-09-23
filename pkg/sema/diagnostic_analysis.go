package sema

import (
	"hikec-go/pkg/ast"
	"hikec-go/pkg/diag"
	"hikec-go/pkg/token"
	"path"
	"strings"
)

// AnalyzeWithReporter は通常の意味解析に加えて、回復可能な型エラーを
// Reporter に蓄積します。エラーがあっても同じブロックの後続文を検査します。
func AnalyzeWithReporter(prog *ast.Program, reporter *diag.Reporter, filename string) (*Context, error) {
	return AnalyzeWithReporterMode(prog, reporter, filename, false)
}

// AnalyzeWithReporterMode additionally gates the optional region API.
func AnalyzeWithReporterMode(prog *ast.Program, reporter *diag.Reporter, filename string, regionEnabled bool) (*Context, error) {
	return AnalyzeWithReporterModes(prog, reporter, filename, regionEnabled, false)
}

// AnalyzeWithReporterModes additionally enables Go/Hike compatibility mode.
func AnalyzeWithReporterModes(prog *ast.Program, reporter *diag.Reporter, filename string, regionEnabled, goHikeEnabled bool) (*Context, error) {
	ctx, err := AnalyzeMode(prog, goHikeEnabled)
	if err != nil {
		if reporter != nil {
			reporter.AddRaw(filename, err.Error())
		}
		return nil, nil
	}
	ctx.RegionModeEnabled = regionEnabled
	if regionEnabled && programContainsArea(prog) {
		if reporter != nil {
			reporter.AddRaw(filename, "region and area memory models cannot be used together")
		}
		return nil, nil
	}
	if !regionEnabled {
		for _, imp := range prog.Imports {
			if strings.Trim(imp.Path, "\"`") == "std/alloc/region" {
				if reporter != nil {
					reporter.AddRaw(filename, "region allocation API requires --alloc=region")
				}
				return nil, nil
			}
		}
	}
	if reporter != nil {
		ctx.collectDiagnostics(prog, reporter, filename)
	}
	return ctx, nil
}

func programContainsArea(prog *ast.Program) bool {
	if prog == nil {
		return false
	}
	for _, decl := range prog.Decls {
		var body *ast.BlockStmt
		switch d := decl.(type) {
		case *ast.FuncDecl:
			body = d.Body
		case *ast.CFuncDecl:
			body = d.Body
		}
		if blockContainsArea(body) {
			return true
		}
	}
	return false
}

func blockContainsArea(block *ast.BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.Statements {
		if statementContainsArea(stmt) {
			return true
		}
	}
	return false
}

func statementContainsArea(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.AreaStmt:
		return true
	case *ast.BlockStmt:
		return blockContainsArea(s)
	case *ast.IfStmt:
		if blockContainsArea(s.Consequence) || statementContainsArea(s.Alternative) {
			return true
		}
		return statementContainsArea(s.Init)
	case *ast.ForStmt:
		return blockContainsArea(s.Body) || statementContainsArea(s.Init) || statementContainsArea(s.Post)
	case *ast.ForRangeStmt:
		return blockContainsArea(s.Body)
	case *ast.SwitchStmt:
		if statementContainsArea(s.Init) {
			return true
		}
		for _, clause := range s.Cases {
			for _, nested := range clause.Body {
				if statementContainsArea(nested) {
					return true
				}
			}
		}
	case *ast.TypeSwitchStmt:
		if statementContainsArea(s.Init) {
			return true
		}
		for _, clause := range s.Cases {
			for _, nested := range clause.Body {
				if statementContainsArea(nested) {
					return true
				}
			}
		}
	}
	return false
}

func (c *Context) collectDiagnostics(prog *ast.Program, reporter *diag.Reporter, filename string) {
	packageNames := make(map[string]bool)
	for _, imp := range prog.Imports {
		name := path.Base(strings.Trim(imp.Path, "\"`"))
		packageNames[name] = true
	}
	c.diagnosticPackages = packageNames
	for _, decl := range prog.Decls {
		var body *ast.BlockStmt
		var params []*ast.ParamDecl
		var returns []ast.TypeExpr
		switch d := decl.(type) {
		case *ast.FuncDecl:
			// Loader は依存パッケージの宣言も同じ AST に展開するため、
			// ここではエントリープログラムの main を診断対象にする。
			// 依存パッケージの詳細診断は、そのパッケージを直接コンパイル
			// する際に行う。
			if d.Name == nil || d.Name.Value != "main" {
				continue
			}
			body, params, returns = d.Body, d.Params, d.ReturnTypes
		case *ast.CFuncDecl:
			body, params, returns = d.Body, d.Params, d.ReturnTypes
		default:
			continue
		}
		if body == nil {
			continue
		}
		locals := make(map[string]Type, len(params))
		for _, p := range params {
			locals[p.Name.Value] = TypeInt
		}
		c.checkAreaEscapes(body, cloneAreaTypes(locals), reporter, filename)
		c.checkDiagnosticBlock(body, locals, returns, packageNames, reporter, filename)
	}
}

// checkAreaEscapes rejects shallow copies of area-backed values into an outer
// variable. Such values would retain a pointer into storage invalidated when
// the area ends; callers must use deepcopy instead.
func (c *Context) checkAreaEscapes(block *ast.BlockStmt, locals map[string]Type, reporter *diag.Reporter, filename string) {
	c.checkAreaEscapeBlock(block, locals, nil, reporter, filename)
}

func (c *Context) checkAreaEscapeBlock(block *ast.BlockStmt, locals map[string]Type, areaNames map[string]bool, reporter *diag.Reporter, filename string) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		switch s := stmt.(type) {
		case *ast.AreaStmt:
			innerLocals := cloneAreaTypes(locals)
			innerNames := cloneAreaNames(areaNames)
			c.checkAreaEscapeBlock(s.Body, innerLocals, innerNames, reporter, filename)
		case *ast.VarDecl:
			var t Type = TypeInt
			if s.Value != nil {
				t = c.InferExprType(s.Value, locals)
			}
			if s.Type != nil {
				t = c.ResolveType(s.Type)
			}
			locals[s.Name.Value] = t
			if areaNames != nil {
				areaNames[s.Name.Value] = true
			}
		case *ast.AssignStmt:
			if s.Token.Type == token.DEFINE || s.Token.Literal == ":=" {
				for i, left := range s.Left {
					if ident, ok := left.(*ast.Identifier); ok {
						var t Type = TypeInt
						if i < len(s.Right) {
							t = c.InferExprType(s.Right[i], locals)
						}
						locals[ident.Value] = t
						if areaNames != nil {
							areaNames[ident.Value] = true
						}
					}
				}
			} else if areaNames != nil {
				for i, left := range s.Left {
					if _, ok := left.(*ast.Identifier); !ok || i >= len(s.Right) {
						continue
					}
					right, ok := s.Right[i].(*ast.Identifier)
					if !ok || !areaNames[right.Value] {
						continue
					}
					t := c.InferExprType(s.Right[i], locals)
					if areaEscapeType(t) {
						reporter.Errorf(filename, s.Token.Line, s.Token.Col, "cannot copy area value %s outside its area; use deepcopy", typeNameOf(t))
					}
				}
			}
		case *ast.IfStmt:
			c.checkAreaEscapeBlock(s.Consequence, cloneAreaTypes(locals), cloneAreaNames(areaNames), reporter, filename)
			if alt, ok := s.Alternative.(*ast.BlockStmt); ok {
				c.checkAreaEscapeBlock(alt, cloneAreaTypes(locals), cloneAreaNames(areaNames), reporter, filename)
			}
		case *ast.ForStmt:
			c.checkAreaEscapeBlock(s.Body, cloneAreaTypes(locals), cloneAreaNames(areaNames), reporter, filename)
		case *ast.ForRangeStmt:
			c.checkAreaEscapeBlock(s.Body, cloneAreaTypes(locals), cloneAreaNames(areaNames), reporter, filename)
		case *ast.SwitchStmt:
			for _, clause := range s.Cases {
				c.checkAreaEscapeBlock(&ast.BlockStmt{Statements: clause.Body}, cloneAreaTypes(locals), cloneAreaNames(areaNames), reporter, filename)
			}
		case *ast.TypeSwitchStmt:
			for _, clause := range s.Cases {
				c.checkAreaEscapeBlock(&ast.BlockStmt{Statements: clause.Body}, cloneAreaTypes(locals), cloneAreaNames(areaNames), reporter, filename)
			}
		}
	}
}

func cloneAreaTypes(src map[string]Type) map[string]Type {
	dst := make(map[string]Type, len(src))
	for name, typ := range src {
		dst[name] = typ
	}
	return dst
}

func cloneAreaNames(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src)+1)
	for name, present := range src {
		dst[name] = present
	}
	return dst
}

func areaEscapeType(typ Type) bool {
	if typ == nil {
		return false
	}
	switch t := typ.(type) {
	case *BasicType:
		return t == TypeString || t.Name == "string"
	case *PointerType, *SliceType:
		return true
	case *StructType:
		for _, field := range t.Fields {
			if areaEscapeType(field.Type) {
				return true
			}
		}
	}
	return false
}

func (c *Context) checkDiagnosticBlock(block *ast.BlockStmt, locals map[string]Type, returns []ast.TypeExpr, packageNames map[string]bool, reporter *diag.Reporter, filename string) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		c.checkDiagnosticStmt(stmt, locals, returns, packageNames, reporter, filename)
	}
}

func (c *Context) checkDiagnosticStmt(stmt ast.Statement, locals map[string]Type, returns []ast.TypeExpr, packageNames map[string]bool, reporter *diag.Reporter, filename string) {
	switch s := stmt.(type) {
	case *ast.VarDecl:
		var declType Type = TypeInt
		var valueType Type = TypeInt
		if s.Value != nil {
			valueType = c.InferExprTypeWithDiag(s.Value, locals, reporter, filename)
		}
		if s.Type != nil {
			declType = c.resolveDiagnosticType(s.Type)
			if s.Value != nil && isSimpleDiagnosticTarget(s.Type) && isDiagnosticLiteral(s.Value) && !IsBad(valueType) && !IsBad(declType) && !c.typesCompatible(declType, valueType) {
				reporter.Errorf(filename, s.Token.Line, s.Token.Col, "cannot use %s as %s", typeNameOf(valueType), typeNameOf(declType))
			}
		} else {
			declType = valueType
		}
		locals[s.Name.Value] = declType

	case *ast.AssignStmt:
		isDefine := s.Type != nil || s.Token.Literal == ":=" || s.Token.Type == token.DEFINE || s.Token.Type == token.VAR || s.Token.Type == token.CONST
		if isDefine {
			rightTypes := make([]Type, len(s.Right))
			for i, right := range s.Right {
				rightTypes[i] = c.InferExprTypeWithDiag(right, locals, reporter, filename)
			}
			c.checkTupleAssignment(s, rightTypes, locals, reporter, filename)
			for _, left := range s.Left {
				if ident, ok := left.(*ast.Identifier); ok {
					var valueType Type = TypeInt
					if len(rightTypes) > 0 {
						valueType = rightTypes[0]
					}
					if s.Type != nil && len(rightTypes) > 0 && isSimpleDiagnosticTarget(s.Type) {
						declType := c.resolveDiagnosticType(s.Type)
						if !IsBad(valueType) && !IsBad(declType) && !c.typesCompatible(declType, valueType) {
							reporter.Errorf(filename, s.Token.Line, s.Token.Col, "cannot use %s as %s", typeNameOf(valueType), typeNameOf(declType))
						}
						valueType = declType
					}
					locals[ident.Value] = valueType
				}
			}
			return
		}

		rightTypes := make([]Type, len(s.Right))
		for i, right := range s.Right {
			rightTypes[i] = c.InferExprTypeWithDiag(right, locals, reporter, filename)
		}
		c.checkTupleAssignment(s, rightTypes, locals, reporter, filename)

	case *ast.ExprStmt:
		return
	case *ast.ReturnStmt:
		for i, value := range s.Values {
			actual := c.InferExprTypeWithDiag(value, locals, reporter, filename)
			if i < len(returns) && isDiagnosticLiteral(value) && !IsBad(actual) {
				expected := c.resolveDiagnosticType(returns[i])
				if !IsBad(expected) && !c.typesCompatible(expected, actual) {
					reporter.Errorf(filename, s.Token.Line, s.Token.Col, "cannot use %s as %s", typeNameOf(actual), typeNameOf(expected))
				}
			}
		}
	case *ast.BlockStmt:
		c.checkDiagnosticBlock(s, cloneTypes(locals), returns, packageNames, reporter, filename)
	case *ast.IfStmt:
		if s.Init != nil {
			c.checkDiagnosticStmt(s.Init, locals, returns, packageNames, reporter, filename)
		}
		if _, isInteger := s.Condition.(*ast.IntegerLiteral); isInteger {
			conditionType := c.InferExprTypeWithDiag(s.Condition, locals, reporter, filename)
			if !IsBad(conditionType) && conditionType != TypeBool {
				reporter.Errorf(filename, s.Token.Line, s.Token.Col, "cannot use %s as bool", typeNameOf(conditionType))
			}
		}
		c.checkDiagnosticBlock(s.Consequence, cloneTypes(locals), returns, packageNames, reporter, filename)
		if alt, ok := s.Alternative.(*ast.BlockStmt); ok {
			c.checkDiagnosticBlock(alt, cloneTypes(locals), returns, packageNames, reporter, filename)
		} else if alt, ok := s.Alternative.(*ast.IfStmt); ok {
			c.checkDiagnosticStmt(alt, cloneTypes(locals), returns, packageNames, reporter, filename)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			c.checkDiagnosticStmt(s.Init, locals, returns, packageNames, reporter, filename)
		}
		if s.Post != nil {
			c.checkDiagnosticStmt(s.Post, locals, returns, packageNames, reporter, filename)
		}
		c.checkDiagnosticBlock(s.Body, cloneTypes(locals), returns, packageNames, reporter, filename)
	case *ast.ForRangeStmt:
		rangeLocals := cloneTypes(locals)
		if ident, ok := s.Key.(*ast.Identifier); ok {
			rangeLocals[ident.Value] = TypeInt
		}
		if ident, ok := s.Value.(*ast.Identifier); ok {
			rangeLocals[ident.Value] = TypeInt
		}
		c.checkDiagnosticBlock(s.Body, rangeLocals, returns, packageNames, reporter, filename)
	case *ast.SendStmt:
		return
	case *ast.DeferStmt:
		return
	case *ast.LockStmt:
		if statementContainsLock(s.Body) {
			reporter.Errorf(filename, s.Token.Line, s.Token.Col, "nested lock blocks are not allowed")
		}
		if statementContainsCall(s.Body) {
			reporter.Errorf(filename, s.Token.Line, s.Token.Col, "function calls are not allowed inside a lock block")
		}
		c.checkDiagnosticBlock(s.Body, cloneTypes(locals), returns, packageNames, reporter, filename)
	case *ast.AreaStmt:
		if s.Size != nil {
			c.InferExprTypeWithDiag(s.Size, locals, reporter, filename)
		}
		c.checkDiagnosticBlock(s.Body, cloneTypes(locals), returns, packageNames, reporter, filename)
	}
}

func statementContainsLock(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.LockStmt:
		return true
	case *ast.BlockStmt:
		for _, child := range s.Statements {
			if statementContainsLock(child) {
				return true
			}
		}
	case *ast.IfStmt:
		return statementContainsLock(s.Init) || statementContainsLock(s.Consequence) || statementContainsLock(s.Alternative)
	case *ast.ForStmt:
		return statementContainsLock(s.Init) || statementContainsLock(s.Post) || statementContainsLock(s.Body)
	case *ast.ForRangeStmt:
		return statementContainsLock(s.Body)
	case *ast.SwitchStmt:
		if statementContainsLock(s.Init) {
			return true
		}
		for _, clause := range s.Cases {
			for _, child := range clause.Body {
				if statementContainsLock(child) {
					return true
				}
			}
		}
	case *ast.TypeSwitchStmt:
		if statementContainsLock(s.Init) {
			return true
		}
		for _, clause := range s.Cases {
			for _, child := range clause.Body {
				if statementContainsLock(child) {
					return true
				}
			}
		}
	case *ast.CaseClause:
		for _, child := range s.Body {
			if statementContainsLock(child) {
				return true
			}
		}
	case *ast.TypeCaseClause:
		for _, child := range s.Body {
			if statementContainsLock(child) {
				return true
			}
		}
	case *ast.AreaStmt:
		return statementContainsLock(s.Body)
	}
	return false
}

func statementContainsCall(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		for _, child := range s.Statements {
			if statementContainsCall(child) {
				return true
			}
		}
	case *ast.ExprStmt:
		return expressionContainsCall(s.Expr)
	case *ast.AssignStmt:
		return expressionsContainCall(s.Left) || expressionsContainCall(s.Right)
	case *ast.VarDecl:
		return expressionContainsCall(s.Value)
	case *ast.ReturnStmt:
		return expressionsContainCall(s.Values)
	case *ast.DeferStmt:
		return expressionContainsCall(s.Call)
	case *ast.SendStmt:
		return expressionContainsCall(s.Chan) || expressionContainsCall(s.Value)
	case *ast.IfStmt:
		return statementContainsCall(s.Init) || expressionContainsCall(s.Condition) || statementContainsCall(s.Consequence) || statementContainsCall(s.Alternative)
	case *ast.ForStmt:
		return statementContainsCall(s.Init) || expressionContainsCall(s.Cond) || statementContainsCall(s.Post) || statementContainsCall(s.Body)
	case *ast.ForRangeStmt:
		return expressionContainsCall(s.Key) || expressionContainsCall(s.Value) || expressionContainsCall(s.X) || statementContainsCall(s.Body)
	case *ast.SwitchStmt:
		if statementContainsCall(s.Init) || expressionContainsCall(s.Value) {
			return true
		}
		for _, clause := range s.Cases {
			if expressionsContainCall(clause.Values) || statementContainsCall(clause) {
				return true
			}
		}
	case *ast.TypeSwitchStmt:
		if statementContainsCall(s.Init) || expressionContainsCall(s.Expr) {
			return true
		}
		for _, clause := range s.Cases {
			if statementContainsCall(clause) {
				return true
			}
		}
	case *ast.CaseClause:
		if expressionsContainCall(s.Values) {
			return true
		}
		for _, child := range s.Body {
			if statementContainsCall(child) {
				return true
			}
		}
	case *ast.TypeCaseClause:
		for _, child := range s.Body {
			if statementContainsCall(child) {
				return true
			}
		}
	case *ast.LockStmt, *ast.AreaStmt:
		if s, ok := stmt.(*ast.LockStmt); ok {
			return statementContainsCall(s.Body)
		}
		return statementContainsCall(stmt.(*ast.AreaStmt).Body)
	}
	return false
}

func expressionsContainCall(exprs []ast.Expression) bool {
	for _, expr := range exprs {
		if expressionContainsCall(expr) {
			return true
		}
	}
	return false
}

func expressionContainsCall(expr ast.Expression) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		return true
	case *ast.PrefixExpr:
		return expressionContainsCall(e.Right)
	case *ast.ReceiveExpr:
		return expressionContainsCall(e.Expr)
	case *ast.AsyncExpr:
		return expressionContainsCall(e.Fn)
	case *ast.BinaryExpr:
		return expressionContainsCall(e.Left) || expressionContainsCall(e.Right)
	case *ast.IndexExpr:
		return expressionContainsCall(e.Left) || expressionContainsCall(e.Index)
	case *ast.MemberExpr:
		return expressionContainsCall(e.Object)
	case *ast.GenericInstExpr:
		return expressionContainsCall(e.Left)
	case *ast.SliceExpr:
		return expressionContainsCall(e.Left) || expressionContainsCall(e.Low) || expressionContainsCall(e.High)
	case *ast.SliceLiteral:
		return expressionsContainCall(e.Elements)
	case *ast.ArrayLiteral:
		return expressionsContainCall(e.Elements)
	case *ast.StructLiteral:
		for _, field := range e.Fields {
			if field != nil && expressionContainsCall(field.Value) {
				return true
			}
		}
	case *ast.MapLiteral:
		for _, entry := range e.Entries {
			if entry != nil && (expressionContainsCall(entry.Key) || expressionContainsCall(entry.Value)) {
				return true
			}
		}
	case *ast.TypeAssertExpr:
		return expressionContainsCall(e.Expr)
	case *ast.FuncLit:
		return statementContainsCall(e.Body)
	}
	return false
}

// checkTupleAssignment verifies the exact value sequence produced by a
// multi-return call before lowering turns it into tuple field accesses.  This
// is intentionally shared by named functions, function values, and closures:
// once expression inference has resolved the function type, their assignment
// ABI must be identical.
func (c *Context) checkTupleAssignment(stmt *ast.AssignStmt, rightTypes []Type, locals map[string]Type, reporter *diag.Reporter, filename string) {
	if len(stmt.Right) != 1 || len(rightTypes) != 1 {
		return
	}
	tuple, ok := rightTypes[0].(*TupleType)
	if !ok {
		return
	}
	if len(stmt.Left) != len(tuple.Types) {
		reporter.Errorf(filename, stmt.Token.Line, stmt.Token.Col,
			"assignment mismatch: %d variables but %d values", len(stmt.Left), len(tuple.Types))
		return
	}
	for i, left := range stmt.Left {
		if ident, ok := left.(*ast.Identifier); ok && ident.Value == "_" {
			continue
		}
		expected := diagnosticAssignmentTargetType(left, stmt, locals, c)
		if expected == nil || IsBad(expected) || IsBad(tuple.Types[i]) || c.typesCompatible(expected, tuple.Types[i]) {
			continue
		}
		reporter.Errorf(filename, stmt.Token.Line, stmt.Token.Col,
			"cannot use %s as %s", typeNameOf(tuple.Types[i]), typeNameOf(expected))
	}
}

func diagnosticAssignmentTargetType(left ast.Expression, stmt *ast.AssignStmt, locals map[string]Type, c *Context) Type {
	if stmt.Type != nil {
		return c.resolveDiagnosticType(stmt.Type)
	}
	if ident, ok := left.(*ast.Identifier); ok {
		return locals[ident.Value]
	}
	return c.InferExprType(left, locals)
}

func cloneTypes(src map[string]Type) map[string]Type {
	dst := make(map[string]Type, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func isDiagnosticLiteral(expr ast.Expression) bool {
	switch expr.(type) {
	case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral, *ast.CharLiteral, *ast.NilLiteral:
		return true
	case *ast.Identifier:
		return expr.TokenLiteral() == "true" || expr.TokenLiteral() == "false"
	default:
		return false
	}
}

func isSimpleDiagnosticTarget(expr ast.TypeExpr) bool {
	if named, ok := expr.(*ast.NamedType); ok && named.Name != nil {
		return named.Name.Value == "string" || named.Name.Value == "bool"
	}
	return false
}

func (c *Context) resolveDiagnosticType(expr ast.TypeExpr) Type {
	// ResolveType is expected to be total for diagnostic expressions.  Keep
	// this wrapper free of a named return value because Go-Hike cannot assign
	// to that value from a deferred recovery block.
	return c.ResolveType(expr)
}
