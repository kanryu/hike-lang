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
		c.checkDiagnosticBlock(body, locals, returns, packageNames, reporter, filename)
	}
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
		isDefine := s.Type != nil || s.Token.Literal == ":=" || s.Token.Type == token.DEFINE || s.Token.Type == token.VAR
		if isDefine {
			rightTypes := make([]Type, len(s.Right))
			for i, right := range s.Right {
				rightTypes[i] = c.InferExprTypeWithDiag(right, locals, reporter, filename)
			}
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
	}
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
