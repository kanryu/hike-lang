package parser

import (
	"fmt"
	"strconv"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/token"
)

const (
	_ int = iota
	LOWEST
	LOR         // ||
	LAND        // &&
	EQUALS      // ==, !=
	LESSGREATER // >, <, <=, >=
	SUM         // +, -
	PRODUCT     // *, /
	PREFIX      // -X or !X
	CALL        // myFunction(X)
	INDEX       // array[index], .field
)

var precedences = map[token.TokenType]int{
	token.LOR:       LOR,
	token.LAND:      LAND,
	token.OR:        SUM,
	token.CARET:     SUM,
	token.EQ:        EQUALS,
	token.NEQ:       EQUALS,
	token.LT:        LESSGREATER,
	token.GT:        LESSGREATER,
	token.LE:        LESSGREATER,
	token.GE:        LESSGREATER,
	token.PLUS:      SUM,
	token.MINUS:     SUM,
	token.SLASH:     PRODUCT,
	token.ASTERISK:  PRODUCT,
	token.AMPERSAND: PRODUCT,
	token.SHL:       PRODUCT,
	token.SHR:       PRODUCT,
	token.LPAREN:    CALL,
	token.LBRACKET:  INDEX,
	token.DOT:       INDEX,
}

type Parser struct {
	l              *lexer.Lexer
	curToken       token.Token
	peekToken      token.Token
	errors         []string
	verbose        bool
	allowStructLit bool
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:              l,
		errors:         []string{},
		verbose:        false,
		allowStructLit: true,
	}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) SetVerbose(v bool) {
	p.verbose = v
}

func (p *Parser) log(msg string) {
	if p.verbose {
		fmt.Printf("[PARSER] %s\n", msg)
	}
}

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) curTokenIs(t token.TokenType) bool {
	return p.curToken.Type == t
}

func (p *Parser) peekTokenIs(t token.TokenType) bool {
	return p.peekToken.Type == t
}

func (p *Parser) expectPeek(t token.TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.errors = append(p.errors, fmt.Sprintf("expected next token to be %s, got %s instead", t, p.peekToken.Type))
	return false
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) ParseProgram() *ast.Program {
	prog := &ast.Program{
		Decls:   []ast.Decl{},
		Imports: []*ast.ImportDecl{},
	}
	for !p.curTokenIs(token.EOF) {
		switch p.curToken.Type {
		case token.PACKAGE:
			p.nextToken()
			if p.curTokenIs(token.IDENT) {
				prog.Package = p.curToken.Literal
				p.log(fmt.Sprintf("[%d:%d] Declared package '%s'", p.curToken.Line, p.curToken.Col, prog.Package))
			}
		case token.IMPORT:
			imports := p.parseImportDecl()
			prog.Imports = append(prog.Imports, imports...)
		case token.CONST:
			constDecls := p.parseConstDecl()
			prog.Decls = append(prog.Decls, constDecls...)
		default:
			decl := p.parseTopLevelDecl()
			if decl != nil {
				prog.Decls = append(prog.Decls, decl)
			}
		}
		p.nextToken()
	}
	return prog
}

func (p *Parser) parseTopLevelDecl() ast.Decl {
	switch p.curToken.Type {
	case token.TYPE:
		return p.parseTypeDecl()
	case token.FUNC:
		return p.parseFuncDecl()
	case token.VAR:
		return p.parseVarDecl()
	default:
		return nil
	}
}

func (p *Parser) parseVarDecl() *ast.VarDecl {
	decl := &ast.VarDecl{Token: p.curToken}
	p.nextToken()
	decl.Name = p.parseIdentifier()
	p.nextToken()

	if !p.curTokenIs(token.ASSIGN) && !p.curTokenIs(token.SEMICOLON) && !p.curTokenIs(token.EOF) {
		decl.Type = p.parseTypeExpr()
		if p.peekTokenIs(token.ASSIGN) {
			p.nextToken()
		}
	}

	if p.curTokenIs(token.ASSIGN) {
		p.nextToken()
		decl.Value = p.parseExpression(LOWEST)
	}
	p.log(fmt.Sprintf("[%d:%d] Parsed var declaration: %s", decl.Token.Line, decl.Token.Col, decl.Name.Value))
	return decl
}

func (p *Parser) parseImportDecl() []*ast.ImportDecl {
	imports := []*ast.ImportDecl{}
	p.nextToken()
	if p.curTokenIs(token.LPAREN) {
		for !p.peekTokenIs(token.RPAREN) && !p.peekTokenIs(token.EOF) {
			p.nextToken()
			if p.curTokenIs(token.STRING) {
				imports = append(imports, &ast.ImportDecl{Token: p.curToken, Path: p.curToken.Literal})
				p.log(fmt.Sprintf("[%d:%d] Imported '%s'", p.curToken.Line, p.curToken.Col, p.curToken.Literal))
			}
		}
		p.expectPeek(token.RPAREN)
	} else if p.curTokenIs(token.STRING) {
		imports = append(imports, &ast.ImportDecl{Token: p.curToken, Path: p.curToken.Literal})
		p.log(fmt.Sprintf("[%d:%d] Imported '%s'", p.curToken.Line, p.curToken.Col, p.curToken.Literal))
	}
	return imports
}

// 1. parseTypeDecl の更新
func (p *Parser) parseTypeDecl() *ast.TypeDecl {
	stmt := &ast.TypeDecl{Token: p.curToken}
	p.nextToken()
	stmt.Name = p.parseIdentifier()
	p.nextToken()

	if p.curTokenIs(token.STRUCT) {
		st := &ast.StructType{Token: p.curToken, Fields: []*ast.FieldDecl{}}
		if p.expectPeek(token.LBRACE) {
			for !p.peekTokenIs(token.RBRACE) && !p.peekTokenIs(token.EOF) {
				p.nextToken()
				fieldName := p.parseIdentifier()
				p.nextToken()
				fieldType := p.parseTypeExpr()
				st.Fields = append(st.Fields, &ast.FieldDecl{Token: fieldName.Token, Name: fieldName, Type: fieldType})
			}
			p.expectPeek(token.RBRACE)
		}
		stmt.Type = st
	} else if p.curTokenIs(token.INTERFACE) || (p.curTokenIs(token.IDENT) && p.curToken.Literal == "interface") {
		it := &ast.InterfaceType{Token: p.curToken, Methods: []*ast.FieldDecl{}}
		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
			p.expectPeek(token.RBRACE)
		}
		stmt.Type = it
	} else {
		stmt.Type = p.parseTypeExpr()
	}
	p.log(fmt.Sprintf("[%d:%d] Parsed type declaration: %s", stmt.Token.Line, stmt.Token.Col, stmt.Name.Value))
	return stmt
}

func (p *Parser) parseConstDecl() []ast.Decl {
	decls := []ast.Decl{}
	p.nextToken()

	if p.curTokenIs(token.LPAREN) {
		p.nextToken()
		iotaVal := int64(0)
		var lastExpr ast.Expression = nil

		for !p.curTokenIs(token.RPAREN) && !p.curTokenIs(token.EOF) {
			if p.curTokenIs(token.IDENT) {
				name := p.parseIdentifier()
				var valExpr ast.Expression = nil

				if p.peekTokenIs(token.ASSIGN) {
					p.nextToken()
					p.nextToken()
					valExpr = p.parseExpression(LOWEST)
					lastExpr = valExpr
				} else if lastExpr != nil {
					valExpr = lastExpr
				} else {
					valExpr = &ast.IotaExpr{Token: name.Token, Value: iotaVal}
				}

				valExpr = replaceIota(valExpr, iotaVal)

				decls = append(decls, &ast.ConstDecl{
					Token: name.Token,
					Name:  name,
					Value: valExpr,
				})
				iotaVal++
			}
			p.nextToken()
		}
	} else {
		name := p.parseIdentifier()
		var valExpr ast.Expression = nil
		if p.peekTokenIs(token.ASSIGN) {
			p.nextToken()
			p.nextToken()
			valExpr = p.parseExpression(LOWEST)
		}
		decls = append(decls, &ast.ConstDecl{
			Token: name.Token,
			Name:  name,
			Value: valExpr,
		})
	}
	return decls
}

func replaceIota(expr ast.Expression, iotaVal int64) ast.Expression {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if e.Value == "iota" {
			return &ast.IotaExpr{Token: e.Token, Value: iotaVal}
		}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Token:    e.Token,
			Operator: e.Operator,
			Left:     replaceIota(e.Left, iotaVal),
			Right:    replaceIota(e.Right, iotaVal),
		}
	}
	return expr
}

func (p *Parser) parseFuncDecl() *ast.FuncDecl {
	fn := &ast.FuncDecl{Token: p.curToken}
	p.nextToken()

	if p.curTokenIs(token.LPAREN) {
		p.nextToken()
		recvName := p.parseIdentifier()
		p.nextToken()
		recvType := p.parseTypeExpr()
		p.expectPeek(token.RPAREN)
		fn.Receiver = &ast.ParamDecl{Token: recvName.Token, Name: recvName, Type: recvType}
		p.nextToken()
	}

	fn.Name = p.parseIdentifier()
	p.log(fmt.Sprintf("[%d:%d] Parsing function: %s", fn.Token.Line, fn.Token.Col, fn.Name.Value))
	p.nextToken()

	fn.Params = []*ast.ParamDecl{}
	if !p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		for {
			if p.curTokenIs(token.ELLIPSIS) {
				fn.IsVariadic = true
				if p.peekTokenIs(token.RPAREN) {
					break
				}
				p.nextToken()
			} else {
				pName := p.parseIdentifier()
				p.nextToken()
				if p.curTokenIs(token.ELLIPSIS) {
					fn.IsVariadic = true
					p.nextToken()
				}
				pType := p.parseTypeExpr()
				fn.Params = append(fn.Params, &ast.ParamDecl{Token: pName.Token, Name: pName, Type: pType})
			}

			if p.peekTokenIs(token.COMMA) {
				p.nextToken()
				if p.peekTokenIs(token.RPAREN) {
					break
				}
				p.nextToken()
			} else {
				break
			}
		}
	}
	p.expectPeek(token.RPAREN)

	// parseFuncDecl 内の戻り値パース部分を更新
	fn.ReturnTypes = []ast.TypeExpr{}
	if !p.curTokenIs(token.LBRACE) && !p.peekTokenIs(token.LBRACE) && !p.peekTokenIs(token.EOF) && !p.curTokenIs(token.EOF) {
		if p.peekTokenIs(token.LPAREN) {
			p.nextToken() // '('
			p.nextToken()
			for {
				fn.ReturnTypes = append(fn.ReturnTypes, p.parseTypeExpr())
				if p.peekTokenIs(token.COMMA) {
					p.nextToken()
					p.nextToken()
				} else {
					break
				}
			}
			p.expectPeek(token.RPAREN)
		} else if !p.peekTokenIs(token.SEMICOLON) {
			p.nextToken()
			fn.ReturnTypes = append(fn.ReturnTypes, p.parseTypeExpr())
		}
	}

	if p.peekTokenIs(token.LBRACE) {
		p.nextToken()
		fn.Body = p.parseBlockStmt()
	} else if p.curTokenIs(token.LBRACE) {
		fn.Body = p.parseBlockStmt()
	}

	return fn
}

// 2. parseTypeExpr の更新
func (p *Parser) parseTypeExpr() ast.TypeExpr {
	if p.curTokenIs(token.IDENT) {
		ident := p.parseIdentifier()
		if ident.Value == "interface" && p.peekTokenIs(token.LBRACE) {
			tok := p.curToken
			p.nextToken()
			p.expectPeek(token.RBRACE)
			return &ast.InterfaceType{Token: tok}
		}
		if p.peekTokenIs(token.DOT) {
			p.nextToken()
			p.nextToken()
			field := p.parseIdentifier()
			return &ast.NamedType{Token: ident.Token, Package: ident, Name: field}
		}
		return &ast.NamedType{Token: ident.Token, Package: nil, Name: ident}
	} else if p.curTokenIs(token.ASTERISK) {
		tok := p.curToken
		p.nextToken()
		base := p.parseTypeExpr()
		return &ast.PointerType{Token: tok, Base: base}
	} else if p.curTokenIs(token.LBRACKET) {
		tok := p.curToken
		if p.peekTokenIs(token.INT) {
			p.nextToken()
			arrLen, _ := strconv.ParseInt(p.curToken.Literal, 0, 64)
			p.expectPeek(token.RBRACKET)
			p.nextToken()
			elem := p.parseTypeExpr()
			return &ast.ArrayType{Token: tok, Len: arrLen, Elem: elem}
		} else if p.expectPeek(token.RBRACKET) {
			p.nextToken()
			elem := p.parseTypeExpr()
			return &ast.SliceType{Token: tok, Elem: elem}
		}
	}
	return &ast.NamedType{Token: p.curToken, Package: nil, Name: &ast.Identifier{Token: p.curToken, Value: "int"}}
}

// parseStatement に token.VAR を追加
func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.VAR:
		return p.parseVarStmt()
	case token.IF:
		return p.parseIfStmt()
	case token.FOR:
		return p.parseForStmt()
	case token.SWITCH:
		return p.parseSwitchStmt()
	case token.RETURN:
		return p.parseReturnStmt()
	case token.DEFER:
		return p.parseDeferStmt()
	case token.BREAK:
		stmt := &ast.BreakStmt{Token: p.curToken}
		return stmt
	case token.CONTINUE:
		stmt := &ast.ContinueStmt{Token: p.curToken}
		return stmt
	default:
		return p.parseAssignOrExprStmt()
	}
}

// var x Type = expr または var x = expr の解析
func (p *Parser) parseVarStmt() ast.Statement {
	varTok := p.curToken
	p.nextToken() // 変数名へ
	ident := p.parseIdentifier()
	p.nextToken()

	var typeExpr ast.TypeExpr = nil
	if !p.curTokenIs(token.ASSIGN) && !p.curTokenIs(token.SEMICOLON) && !p.curTokenIs(token.EOF) {
		typeExpr = p.parseTypeExpr()
		if p.peekTokenIs(token.ASSIGN) {
			p.nextToken()
		}
	}

	if p.curTokenIs(token.ASSIGN) {
		p.nextToken()
		rhs := p.parseExpression(LOWEST)
		return &ast.AssignStmt{
			Token: varTok,
			Left:  []ast.Expression{ident},
			Right: []ast.Expression{rhs},
			Type:  typeExpr,
		}
	}

	return &ast.AssignStmt{
		Token: varTok,
		Left:  []ast.Expression{ident},
		Right: []ast.Expression{&ast.IntegerLiteral{Token: varTok, Value: 0}},
		Type:  typeExpr,
	}
}

func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	block := &ast.BlockStmt{Token: p.curToken, Statements: []ast.Statement{}}
	p.nextToken()
	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}
	return block
}

func (p *Parser) parseIfStmt() *ast.IfStmt {
	stmt := &ast.IfStmt{Token: p.curToken}
	p.nextToken()

	oldAllow := p.allowStructLit
	p.allowStructLit = false

	firstStmt := p.parseAssignOrExprStmt()

	if p.peekTokenIs(token.SEMICOLON) {
		stmt.Init = firstStmt
		p.nextToken() // ';'
		p.nextToken() // condへ
		stmt.Condition = p.parseExpression(LOWEST)
	} else {
		if exprStmt, ok := firstStmt.(*ast.ExprStmt); ok {
			stmt.Condition = exprStmt.Expr
		}
	}
	p.allowStructLit = oldAllow

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	stmt.Consequence = p.parseBlockStmt()

	if p.peekTokenIs(token.ELSE) {
		p.nextToken()
		if p.peekTokenIs(token.IF) {
			p.nextToken()
			stmt.Alternative = p.parseIfStmt()
		} else if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
			stmt.Alternative = p.parseBlockStmt()
		}
	}

	return stmt
}

func (p *Parser) parseForStmt() ast.Statement {
	forTok := p.curToken
	p.nextToken()

	if p.curTokenIs(token.LBRACE) {
		body := p.parseBlockStmt()
		return &ast.ForStmt{Token: forTok, Body: body}
	}

	oldAllow := p.allowStructLit
	p.allowStructLit = false

	// A. for range X { ... }
	if p.curTokenIs(token.RANGE) {
		p.nextToken()
		x := p.parseExpression(LOWEST)
		p.allowStructLit = oldAllow
		if !p.expectPeek(token.LBRACE) {
			return nil
		}
		body := p.parseBlockStmt()
		return &ast.ForRangeStmt{Token: forTok, Key: nil, Value: nil, X: x, Body: body}
	}

	// B. for i, v := range X または for i := range X
	var firstStmt ast.Statement = nil
	if p.curTokenIs(token.IDENT) && (p.peekTokenIs(token.COMMA) || p.peekTokenIs(token.DEFINE) || p.peekTokenIs(token.ASSIGN)) {
		firstIdent := p.parseIdentifier()

		if p.peekTokenIs(token.COMMA) {
			p.nextToken()
			p.nextToken()
			secondIdent := p.parseIdentifier()

			if p.peekTokenIs(token.DEFINE) || p.peekTokenIs(token.ASSIGN) {
				p.nextToken()
				if p.peekTokenIs(token.RANGE) {
					p.nextToken()
					p.nextToken()
					x := p.parseExpression(LOWEST)
					p.allowStructLit = oldAllow
					if !p.expectPeek(token.LBRACE) {
						return nil
					}
					body := p.parseBlockStmt()
					return &ast.ForRangeStmt{Token: forTok, Key: firstIdent, Value: secondIdent, X: x, Body: body}
				} else {
					p.nextToken()
					rights := []ast.Expression{p.parseExpression(LOWEST)}
					for p.peekTokenIs(token.COMMA) {
						p.nextToken()
						p.nextToken()
						rights = append(rights, p.parseExpression(LOWEST))
					}
					firstStmt = &ast.AssignStmt{Token: p.curToken, Left: []ast.Expression{firstIdent, secondIdent}, Right: rights}
				}
			}
		} else if p.peekTokenIs(token.DEFINE) || p.peekTokenIs(token.ASSIGN) {
			assignTok := p.peekToken
			p.nextToken()
			if p.peekTokenIs(token.RANGE) {
				p.nextToken()
				p.nextToken()
				x := p.parseExpression(LOWEST)
				p.allowStructLit = oldAllow
				if !p.expectPeek(token.LBRACE) {
					return nil
				}
				body := p.parseBlockStmt()
				return &ast.ForRangeStmt{Token: forTok, Key: firstIdent, Value: nil, X: x, Body: body}
			} else {
				p.nextToken()
				rhs := p.parseExpression(LOWEST)
				firstStmt = &ast.AssignStmt{Token: assignTok, Left: []ast.Expression{firstIdent}, Right: []ast.Expression{rhs}}
			}
		}
	} else {
		firstStmt = p.parseAssignOrExprStmt()
	}

	// 3-clause for loop: init; cond; post
	if p.peekTokenIs(token.SEMICOLON) {
		init := firstStmt
		p.nextToken()
		p.nextToken()
		var cond ast.Expression = nil
		if !p.curTokenIs(token.SEMICOLON) {
			cond = p.parseExpression(LOWEST)
		}
		p.expectPeek(token.SEMICOLON)
		p.nextToken()
		var post ast.Statement = nil
		if !p.curTokenIs(token.LBRACE) {
			post = p.parseAssignOrExprStmt()
		}
		p.allowStructLit = oldAllow
		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
		}
		body := p.parseBlockStmt()
		return &ast.ForStmt{Token: forTok, Init: init, Cond: cond, Post: post, Body: body}
	}

	// Single condition loop: for cond { ... }
	var cond ast.Expression = nil
	if exprStmt, ok := firstStmt.(*ast.ExprStmt); ok {
		cond = exprStmt.Expr
	}
	p.allowStructLit = oldAllow
	if p.peekTokenIs(token.LBRACE) {
		p.nextToken()
	}
	body := p.parseBlockStmt()
	return &ast.ForStmt{Token: forTok, Cond: cond, Body: body}
}

func (p *Parser) parseSwitchStmt() *ast.SwitchStmt {
	stmt := &ast.SwitchStmt{Token: p.curToken, Cases: []*ast.CaseClause{}}
	p.nextToken()

	oldAllow := p.allowStructLit
	p.allowStructLit = false

	firstStmt := p.parseAssignOrExprStmt()

	if p.peekTokenIs(token.SEMICOLON) {
		stmt.Init = firstStmt
		p.nextToken() // ';'
		p.nextToken() // exprへ
		stmt.Value = p.parseExpression(LOWEST)
	} else {
		if exprStmt, ok := firstStmt.(*ast.ExprStmt); ok {
			stmt.Value = exprStmt.Expr
		}
	}
	p.allowStructLit = oldAllow

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.CASE) || p.curTokenIs(token.DEFAULT) {
			c := &ast.CaseClause{Token: p.curToken, Values: []ast.Expression{}, Body: []ast.Statement{}}
			if p.curTokenIs(token.CASE) {
				p.nextToken()
				for {
					c.Values = append(c.Values, p.parseExpression(LOWEST))
					if p.peekTokenIs(token.COMMA) {
						p.nextToken()
						p.nextToken()
					} else {
						break
					}
				}
			}
			p.expectPeek(token.COLON)
			p.nextToken()
			for !p.curTokenIs(token.CASE) && !p.curTokenIs(token.DEFAULT) && !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
				s := p.parseStatement()
				if s != nil {
					c.Body = append(c.Body, s)
				}
				p.nextToken()
			}
			stmt.Cases = append(stmt.Cases, c)
		} else {
			p.nextToken()
		}
	}

	return stmt
}

func (p *Parser) parseReturnStmt() *ast.ReturnStmt {
	stmt := &ast.ReturnStmt{Token: p.curToken, Values: []ast.Expression{}}
	if p.peekTokenIs(token.SEMICOLON) || p.peekTokenIs(token.RBRACE) || p.peekTokenIs(token.EOF) {
		return stmt
	}
	p.nextToken()
	for {
		stmt.Values = append(stmt.Values, p.parseExpression(LOWEST))
		if p.peekTokenIs(token.COMMA) {
			p.nextToken()
			p.nextToken()
		} else {
			break
		}
	}
	return stmt
}

func (p *Parser) parseDeferStmt() ast.Statement {
	stmt := &ast.DeferStmt{Token: p.curToken}
	p.nextToken() // 'defer' の次へ
	exp := p.parseExpression(LOWEST)
	if call, ok := exp.(*ast.CallExpr); ok {
		stmt.Call = call
	}
	p.log(fmt.Sprintf("[%d:%d] Parsed defer statement", stmt.Token.Line, stmt.Token.Col))
	return stmt
}

func (p *Parser) parseAssignOrExprStmt() ast.Statement {
	startTok := p.curToken
	leftExpr := p.parseExpression(LOWEST)

	if p.peekTokenIs(token.INC) || p.peekTokenIs(token.DEC) {
		p.nextToken()
		opTok := p.curToken
		return &ast.AssignStmt{
			Token: opTok,
			Left:  []ast.Expression{leftExpr},
			Right: []ast.Expression{&ast.IntegerLiteral{Token: opTok, Value: 1}},
		}
	}

	// parseAssignOrExprStmt 内での複合代入演算子判定の更新
	if p.peekTokenIs(token.PLUS_ASSIGN) || p.peekTokenIs(token.MINUS_ASSIGN) ||
		p.peekTokenIs(token.ASTERISK_ASSIGN) || p.peekTokenIs(token.SLASH_ASSIGN) ||
		p.peekTokenIs(token.AND_ASSIGN) || p.peekTokenIs(token.OR_ASSIGN) ||
		p.peekTokenIs(token.XOR_ASSIGN) || p.peekTokenIs(token.SHL_ASSIGN) || p.peekTokenIs(token.SHR_ASSIGN) {
		p.nextToken()
		assignTok := p.curToken
		p.nextToken()
		rhs := p.parseExpression(LOWEST)
		return &ast.AssignStmt{
			Token: assignTok,
			Left:  []ast.Expression{leftExpr},
			Right: []ast.Expression{rhs},
		}
	}

	if p.peekTokenIs(token.DEFINE) || p.peekTokenIs(token.ASSIGN) {
		p.nextToken()
		assignTok := p.curToken
		p.nextToken()
		rhs := p.parseExpression(LOWEST)
		return &ast.AssignStmt{
			Token: assignTok,
			Left:  []ast.Expression{leftExpr},
			Right: []ast.Expression{rhs},
		}
	}

	if p.peekTokenIs(token.COMMA) {
		lefts := []ast.Expression{leftExpr}
		for p.peekTokenIs(token.COMMA) {
			p.nextToken()
			p.nextToken()
			lefts = append(lefts, p.parseExpression(LOWEST))
		}
		if p.peekTokenIs(token.DEFINE) || p.peekTokenIs(token.ASSIGN) {
			p.nextToken()
			assignTok := p.curToken
			p.nextToken()
			rights := []ast.Expression{}
			for {
				rights = append(rights, p.parseExpression(LOWEST))
				if p.peekTokenIs(token.COMMA) {
					p.nextToken()
					p.nextToken()
				} else {
					break
				}
			}
			return &ast.AssignStmt{Token: assignTok, Left: lefts, Right: rights}
		}
	}

	return &ast.ExprStmt{Token: startTok, Expr: leftExpr}
}

func (p *Parser) parseExpression(precedence int) ast.Expression {
	var leftExp ast.Expression

	switch p.curToken.Type {
	case token.IDENT:
		ident := p.parseIdentifier()
		if p.allowStructLit && p.peekTokenIs(token.LBRACE) {
			p.nextToken()
			namedType := &ast.NamedType{Token: ident.Token, Package: nil, Name: ident}
			fields := []*ast.StructFieldValue{}
			if !p.peekTokenIs(token.RBRACE) {
				p.nextToken()
				for {
					var fName *ast.Identifier = nil
					if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.COLON) {
						fName = p.parseIdentifier()
						p.nextToken()
						p.nextToken()
					}
					val := p.parseExpression(LOWEST)
					fields = append(fields, &ast.StructFieldValue{Name: fName, Value: val})

					if p.peekTokenIs(token.COMMA) {
						p.nextToken()
						if p.peekTokenIs(token.RBRACE) {
							break
						}
						p.nextToken()
					} else {
						break
					}
				}
			}
			p.expectPeek(token.RBRACE)
			leftExp = &ast.StructLiteral{Token: ident.Token, Type: namedType, Fields: fields}
		} else {
			leftExp = ident
		}
	case token.INT:
		leftExp = p.parseIntegerLiteral()
	case token.STRING:
		leftExp = p.parseStringLiteral()
	case token.NIL:
		leftExp = &ast.NilLiteral{Token: p.curToken}
	// parseExpression 内の単項/二項ビット演算子の追加
	case token.BANG, token.MINUS, token.ASTERISK, token.AMPERSAND, token.CARET:
		leftExp = p.parsePrefixExpr()

	case token.LBRACKET:
		tok := p.curToken
		if p.peekTokenIs(token.INT) {
			p.nextToken()
			arrLen, _ := strconv.ParseInt(p.curToken.Literal, 0, 64)
			p.expectPeek(token.RBRACKET)
			p.nextToken()
			elem := p.parseTypeExpr()
			arrT := &ast.ArrayType{Token: tok, Len: arrLen, Elem: elem}

			if p.allowStructLit && p.peekTokenIs(token.LBRACE) {
				p.nextToken()
				elements := []ast.Expression{}
				if !p.peekTokenIs(token.RBRACE) {
					p.nextToken()
					for {
						elements = append(elements, p.parseExpression(LOWEST))
						if p.peekTokenIs(token.COMMA) {
							p.nextToken()
							if p.peekTokenIs(token.RBRACE) {
								break
							}
							p.nextToken()
						} else {
							break
						}
					}
				}
				p.expectPeek(token.RBRACE)
				leftExp = &ast.ArrayLiteral{Token: tok, Type: arrT, Elements: elements}
			} else {
				leftExp = arrT
			}
		} else if p.expectPeek(token.RBRACKET) {
			p.nextToken()
			elem := p.parseTypeExpr()
			sliceT := &ast.SliceType{Token: tok, Elem: elem}

			if p.allowStructLit && p.peekTokenIs(token.LBRACE) {
				p.nextToken()
				elements := []ast.Expression{}
				if !p.peekTokenIs(token.RBRACE) {
					p.nextToken()
					for {
						elements = append(elements, p.parseExpression(LOWEST))
						if p.peekTokenIs(token.COMMA) {
							p.nextToken()
							if p.peekTokenIs(token.RBRACE) {
								break
							}
							p.nextToken()
						} else {
							break
						}
					}
				}
				p.expectPeek(token.RBRACE)
				leftExp = &ast.SliceLiteral{Token: tok, Type: sliceT, Elements: elements}
			} else {
				leftExp = sliceT
			}
		} else {
			return nil
		}

	// 二項演算子ループ内の case token.DOT (型アサーション x.(Type))
	case token.DOT:
		p.nextToken()
		if p.peekTokenIs(token.LPAREN) {
			p.nextToken() // '('
			p.nextToken() // Type
			targetType := p.parseTypeExpr()
			p.expectPeek(token.RPAREN)
			leftExp = &ast.TypeAssertExpr{Token: p.curToken, Expr: leftExp, Target: targetType}
		} else {
			leftExp = p.parseMemberExpr(leftExp)
		}
	case token.LPAREN:
		p.nextToken()
		oldAllow := p.allowStructLit
		p.allowStructLit = true
		leftExp = p.parseExpression(LOWEST)
		p.allowStructLit = oldAllow
		p.expectPeek(token.RPAREN)
	default:
		return nil
	}

	for !p.peekTokenIs(token.SEMICOLON) && precedence < p.peekPrecedence() {
		switch p.peekToken.Type {
		case token.PLUS, token.MINUS, token.SLASH, token.ASTERISK,
			token.EQ, token.NEQ, token.LT, token.GT, token.LE, token.GE,
			token.LAND, token.LOR,
			token.AMPERSAND, token.OR, token.CARET, token.SHL, token.SHR: // 追加
			p.nextToken()
			leftExp = p.parseBinaryExpr(leftExp)
		case token.LPAREN:
			p.nextToken()
			leftExp = p.parseCallExpr(leftExp)
		case token.LBRACKET:
			p.nextToken()
			leftExp = p.parseIndexExpr(leftExp)
		case token.DOT:
			p.nextToken()
			leftExp = p.parseMemberExpr(leftExp)
		default:
			return leftExp
		}
	}

	return leftExp
}

func (p *Parser) parseIdentifier() *ast.Identifier {
	return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseIntegerLiteral() *ast.IntegerLiteral {
	val, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		p.errors = append(p.errors, fmt.Sprintf("could not parse %q as integer", p.curToken.Literal))
		return nil
	}
	return &ast.IntegerLiteral{Token: p.curToken, Value: val}
}

func (p *Parser) parseStringLiteral() *ast.StringLiteral {
	return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parsePrefixExpr() ast.Expression {
	tok := p.curToken
	p.nextToken()
	right := p.parseExpression(PREFIX)
	return &ast.PrefixExpr{Token: tok, Operator: tok.Literal, Right: right}
}

func (p *Parser) parseBinaryExpr(left ast.Expression) ast.Expression {
	tok := p.curToken
	precedence := p.curPrecedence()
	p.nextToken()
	right := p.parseExpression(precedence)
	return &ast.BinaryExpr{Token: tok, Operator: tok.Literal, Left: left, Right: right}
}

func (p *Parser) parseCallExpr(fn ast.Expression) *ast.CallExpr {
	tok := p.curToken
	args := []ast.Expression{}
	hasEllipsis := false

	if !p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		for {
			arg := p.parseExpression(LOWEST)
			if p.peekTokenIs(token.ELLIPSIS) {
				p.nextToken()
				hasEllipsis = true
				args = append(args, arg)
				break
			}
			args = append(args, arg)
			if p.peekTokenIs(token.COMMA) {
				p.nextToken()
				if p.peekTokenIs(token.RPAREN) {
					break
				}
				p.nextToken()
			} else {
				break
			}
		}
	}
	p.expectPeek(token.RPAREN)
	return &ast.CallExpr{Token: tok, Function: fn, Args: args, HasEllipsis: hasEllipsis}
}

func (p *Parser) parseIndexExpr(left ast.Expression) ast.Expression {
	tok := p.curToken

	if p.peekTokenIs(token.COLON) {
		p.nextToken()
		var high ast.Expression = nil
		if !p.peekTokenIs(token.RBRACKET) {
			p.nextToken()
			high = p.parseExpression(LOWEST)
		}
		p.expectPeek(token.RBRACKET)
		return &ast.SliceExpr{Token: tok, Left: left, Low: nil, High: high}
	}

	p.nextToken()
	idx := p.parseExpression(LOWEST)

	if p.peekTokenIs(token.COLON) {
		p.nextToken()
		var high ast.Expression = nil
		if !p.peekTokenIs(token.RBRACKET) {
			p.nextToken()
			high = p.parseExpression(LOWEST)
		}
		p.expectPeek(token.RBRACKET)
		return &ast.SliceExpr{Token: tok, Left: left, Low: idx, High: high}
	}

	p.expectPeek(token.RBRACKET)
	return &ast.IndexExpr{Token: tok, Left: left, Index: idx}
}

// parseMemberExpr: .field と .(TargetType) の両方を正確に分岐
func (p *Parser) parseMemberExpr(obj ast.Expression) ast.Expression {
	tok := p.curToken // '.'
	if p.peekTokenIs(token.LPAREN) {
		p.nextToken() // '('
		p.nextToken() // 型名
		targetType := p.parseTypeExpr()
		p.expectPeek(token.RPAREN)
		return &ast.TypeAssertExpr{Token: tok, Expr: obj, Target: targetType}
	}
	p.nextToken()
	field := p.parseIdentifier()
	return &ast.MemberExpr{Token: tok, Object: obj, Field: field}
}
