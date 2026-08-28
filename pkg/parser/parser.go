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
	LESSGREATER // >, <, >=, <=
	SUM         // +, -
	PRODUCT     // *, /
	PREFIX      // -X, !X
	CALL        // fn(X)
	INDEX       // array[index]
	DOT         // obj.field
)

var precedences = map[token.TokenType]int{
	token.LOR:      LOR,
	token.LAND:     LAND,
	token.EQ:       EQUALS,
	token.NEQ:      EQUALS,
	token.LT:       LESSGREATER,
	token.GT:       LESSGREATER,
	token.LE:       LESSGREATER,
	token.GE:       LESSGREATER,
	token.PLUS:     SUM,
	token.MINUS:    SUM,
	token.SLASH:    PRODUCT,
	token.ASTERISK: PRODUCT,
	token.LPAREN:   CALL,
	token.LBRACKET: INDEX,
	token.DOT:      DOT,
}

type (
	prefixParseFn func() ast.Expression
	infixParseFn  func(ast.Expression) ast.Expression
)

type Parser struct {
	l      *lexer.Lexer
	errors []string

	curToken  token.Token
	peekToken token.Token

	prefixParseFns map[token.TokenType]prefixParseFn
	infixParseFns  map[token.TokenType]infixParseFn
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:      l,
		errors: []string{},
	}

	p.prefixParseFns = make(map[token.TokenType]prefixParseFn)
	p.registerPrefix(token.IDENT, p.parseIdentifierExpr)
	p.registerPrefix(token.INT, p.parseIntegerLiteral)
	p.registerPrefix(token.STRING, p.parseStringLiteral)
	p.registerPrefix(token.NIL, p.parseNilLiteral)
	p.registerPrefix(token.NOT, p.parsePrefixExpr)
	p.registerPrefix(token.MINUS, p.parsePrefixExpr)
	p.registerPrefix(token.ASTERISK, p.parsePrefixExpr)  // 単項前置ポインタ演算子 (*) を登録
	p.registerPrefix(token.AMPERSAND, p.parsePrefixExpr) // ここを追加
	p.registerPrefix(token.LPAREN, p.parseGroupedExpression)

	p.infixParseFns = make(map[token.TokenType]infixParseFn)
	p.registerInfix(token.PLUS, p.parseBinaryExpr)
	p.registerInfix(token.MINUS, p.parseBinaryExpr)
	p.registerInfix(token.ASTERISK, p.parseBinaryExpr)
	p.registerInfix(token.SLASH, p.parseBinaryExpr)
	p.registerInfix(token.EQ, p.parseBinaryExpr)
	p.registerInfix(token.NEQ, p.parseBinaryExpr)
	p.registerInfix(token.LT, p.parseBinaryExpr)
	p.registerInfix(token.GT, p.parseBinaryExpr)
	p.registerInfix(token.LE, p.parseBinaryExpr)
	p.registerInfix(token.GE, p.parseBinaryExpr)
	p.registerInfix(token.LAND, p.parseBinaryExpr)
	p.registerInfix(token.LOR, p.parseBinaryExpr)
	p.registerInfix(token.LPAREN, p.parseCallExpr)
	p.registerInfix(token.LBRACKET, p.parseIndexExpr)
	p.registerInfix(token.DOT, p.parseMemberExpr)

	p.nextToken()
	p.nextToken()

	return p
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
	p.peekError(t)
	return false
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("[%d:%d] expected next token to be %s, got %s instead",
		p.peekToken.Line, p.peekToken.Col, t, p.peekToken.Type)
	p.errors = append(p.errors, msg)
}

func (p *Parser) registerPrefix(tokenType token.TokenType, fn prefixParseFn) {
	p.prefixParseFns[tokenType] = fn
}

func (p *Parser) registerInfix(tokenType token.TokenType, fn infixParseFn) {
	p.infixParseFns[tokenType] = fn
}

func (p *Parser) peekPrecedence() int {
	if prec, ok := precedences[p.peekToken.Type]; ok {
		return prec
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if prec, ok := precedences[p.curToken.Type]; ok {
		return prec
	}
	return LOWEST
}
func (p *Parser) ParseProgram() *ast.Program {
	prog := &ast.Program{
		Imports: []*ast.ImportDecl{},
		Decls:   []ast.Decl{},
	}

	// 1. package 句の解析
	if p.curToken.Type == token.PACKAGE {
		p.nextToken()
		if p.curToken.Type == token.IDENT {
			prog.Package = p.curToken.Literal
			p.nextToken()
		}
	}

	// 2. import 句の解析
	for p.curToken.Type == token.IMPORT {
		p.nextToken()
		if p.curToken.Type == token.LPAREN {
			p.nextToken()
			for p.curToken.Type != token.RPAREN && p.curToken.Type != token.EOF {
				if p.curToken.Type == token.STRING {
					prog.Imports = append(prog.Imports, &ast.ImportDecl{Path: p.curToken.Literal})
					p.nextToken()
				}
				if p.curToken.Type == token.SEMICOLON {
					p.nextToken()
				}
			}
			if p.curToken.Type == token.RPAREN {
				p.nextToken()
			}
		} else if p.curToken.Type == token.STRING {
			prog.Imports = append(prog.Imports, &ast.ImportDecl{Path: p.curToken.Literal})
			p.nextToken()
		}
	}

	// 3. 宣言群の解析
	for p.curToken.Type != token.EOF {
		decl := p.parseDecl()
		if decl != nil {
			prog.Decls = append(prog.Decls, decl)
		} else {
			// パース失敗時のみトークンを1つ進めてリカバリ
			p.nextToken()
		}
	}

	return prog
}

func (p *Parser) parseIdentifier() *ast.Identifier {
	return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseIdentifierExpr() ast.Expression {
	return p.parseIdentifier()
}

func (p *Parser) parseIntegerLiteral() ast.Expression {
	lit := &ast.IntegerLiteral{Token: p.curToken}
	val, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		p.errors = append(p.errors, fmt.Sprintf("could not parse %q as integer", p.curToken.Literal))
		return nil
	}
	lit.Value = val
	return lit
}

func (p *Parser) parseStringLiteral() ast.Expression {
	return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseNilLiteral() ast.Expression {
	return &ast.NilLiteral{Token: p.curToken}
}

func (p *Parser) parseGroupedExpression() ast.Expression {
	p.nextToken()
	exp := p.parseExpression(LOWEST)
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	return exp
}

func (p *Parser) parsePrefixExpr() ast.Expression {
	expr := &ast.PrefixExpr{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
	}
	p.nextToken()
	expr.Right = p.parseExpression(PREFIX)
	return expr
}

func (p *Parser) parseBinaryExpr(left ast.Expression) ast.Expression {
	expr := &ast.BinaryExpr{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
		Left:     left,
	}
	precedence := p.curPrecedence()
	p.nextToken()
	expr.Right = p.parseExpression(precedence)
	return expr
}

func (p *Parser) parseIndexExpr(left ast.Expression) ast.Expression {
	expr := &ast.IndexExpr{
		Token: p.curToken,
		Left:  left,
	}
	p.nextToken()
	expr.Index = p.parseExpression(LOWEST)
	if !p.expectPeek(token.RBRACKET) {
		return nil
	}
	return expr
}

func (p *Parser) parseMemberExpr(left ast.Expression) ast.Expression {
	expr := &ast.MemberExpr{
		Token:  p.curToken,
		Object: left,
	}
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	expr.Field = p.parseIdentifier()
	return expr
}

func (p *Parser) parseCallExpr(function ast.Expression) ast.Expression {
	expr := &ast.CallExpr{
		Token:    p.curToken,
		Function: function,
		Args:     []ast.Expression{},
	}

	if !p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		expr.Args = append(expr.Args, p.parseExpression(LOWEST))

		for p.peekTokenIs(token.COMMA) {
			p.nextToken()
			p.nextToken()
			expr.Args = append(expr.Args, p.parseExpression(LOWEST))
		}
	}

	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return expr
}

func (p *Parser) parseExpression(precedence int) ast.Expression {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] no prefix parse function for %s",
			p.curToken.Line, p.curToken.Col, p.curToken.Type))
		return nil
	}
	leftExp := prefix()

	for !p.peekTokenIs(token.SEMICOLON) &&
		!p.peekTokenIs(token.LBRACE) &&
		!p.peekTokenIs(token.RPAREN) &&
		!p.peekTokenIs(token.RBRACKET) &&
		!p.peekTokenIs(token.COMMA) &&
		!p.peekTokenIs(token.COLON) &&
		precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}
		p.nextToken()
		leftExp = infix(leftExp)
	}

	return leftExp
}

func (p *Parser) parseDecl() ast.Decl {
	switch p.curToken.Type {
	case token.TYPE:
		return p.parseTypeDecl()
	case token.FUNC:
		return p.parseFuncDecl()
	default:
		return nil
	}
}

func (p *Parser) parseTypeDecl() *ast.TypeDecl {
	decl := &ast.TypeDecl{Token: p.curToken}
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	decl.Name = p.parseIdentifier()

	if !p.expectPeek(token.STRUCT) {
		return nil
	}

	st := &ast.StructType{Token: p.curToken, Fields: []*ast.FieldDecl{}}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}
		fieldName := p.parseIdentifier()
		p.nextToken()
		fieldType := p.parseType()
		st.Fields = append(st.Fields, &ast.FieldDecl{Name: fieldName, Type: fieldType})
		p.nextToken()
	}
	decl.Type = st
	p.nextToken()
	return decl
}

func (p *Parser) parseFuncDecl() *ast.FuncDecl {
	fn := &ast.FuncDecl{
		Token:       p.curToken,
		Params:      []*ast.ParamDecl{},
		ReturnTypes: []ast.TypeExpr{},
	}

	// 1. レシーバの解析: func の次が '(' の場合 (例: func (b *Builder) ...)
	if p.peekTokenIs(token.LPAREN) {
		p.nextToken() // '(' へ
		p.nextToken() // レシーバ変数名へ (例: b)

		recvName := p.parseIdentifier()
		p.nextToken() // 型へ進む (例: *Builder)
		recvType := p.parseType()

		if !p.expectPeek(token.RPAREN) {
			return nil
		}

		fn.Receiver = &ast.ParamDecl{
			Name: recvName,
			Type: recvType,
		}
	}

	// 2. 関数名 / メソッド名の取得
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	fn.Name = p.parseIdentifier()

	// 3. 引数リスト '('
	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(token.RPAREN) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.COMMA) {
			p.nextToken()
			continue
		}
		if p.curTokenIs(token.ELLIPSIS) {
			fn.IsVariadic = true
			p.nextToken()
			break
		}

		paramName := p.parseIdentifier()
		p.nextToken()
		paramType := p.parseType()
		fn.Params = append(fn.Params, &ast.ParamDecl{Name: paramName, Type: paramType})
		p.nextToken()
	}

	if p.curTokenIs(token.RPAREN) {
		p.nextToken()
	}

	// 4. 戻り値の型 (既存ロジックをそのまま維持)
	if p.curTokenIs(token.LPAREN) {
		p.nextToken()
		for !p.curTokenIs(token.RPAREN) && !p.curTokenIs(token.EOF) {
			if p.curTokenIs(token.COMMA) {
				p.nextToken()
				continue
			}
			retType := p.parseType()
			fn.ReturnTypes = append(fn.ReturnTypes, retType)
			p.nextToken()
		}
		if p.curTokenIs(token.RPAREN) {
			p.nextToken()
		}
	} else if p.curTokenIs(token.IDENT) || p.curTokenIs(token.ASTERISK) {
		retType := p.parseType()
		fn.ReturnTypes = append(fn.ReturnTypes, retType)
		p.nextToken()
	}

	// 5. 関数本体 '{' (本体がない外部宣言の場合は nil)
	if p.curTokenIs(token.LBRACE) {
		fn.Body = p.parseBlockStmt()
		if p.curTokenIs(token.RBRACE) {
			p.nextToken()
		}
	}

	return fn
}

func (p *Parser) parseType() ast.TypeExpr {
	if p.curTokenIs(token.ASTERISK) {
		tok := p.curToken
		p.nextToken()
		base := p.parseType()
		return &ast.PointerType{Token: tok, Base: base}
	}

	ident := p.parseIdentifier()
	if p.peekTokenIs(token.DOT) {
		p.nextToken()
		p.nextToken()
		typeName := p.parseIdentifier()
		return &ast.NamedType{
			Token:   ident.Token,
			Package: ident,
			Name:    typeName,
		}
	}

	return &ast.NamedType{Token: ident.Token, Name: ident}
}

func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	block := &ast.BlockStmt{Token: p.curToken, Statements: []ast.Statement{}}
	p.nextToken()

	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}

	return block
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.RETURN:
		return p.parseReturnStmt()
	case token.DEFER:
		return p.parseDeferStmt()
	case token.IF:
		return p.parseIfStmt()
	case token.FOR:
		return p.parseForStmt()
	case token.SWITCH:
		return p.parseSwitchStmt()
	case token.SEMICOLON:
		return nil
	default:
		return p.parseExpressionOrAssignStmt()
	}
}

func (p *Parser) parseIfStmt() *ast.IfStmt {
	stmt := &ast.IfStmt{Token: p.curToken}
	p.nextToken() // 'if' を消費

	stmt.Condition = p.parseExpression(LOWEST)

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	stmt.Consequence = p.parseBlockStmt()

	// else / else if の解析
	if p.peekTokenIs(token.ELSE) {
		p.nextToken() // 'else' を消費

		if p.peekTokenIs(token.IF) {
			p.nextToken() // 'if' へ進む
			stmt.Alternative = p.parseIfStmt()
		} else if p.peekTokenIs(token.LBRACE) {
			p.nextToken() // '{' へ進む
			stmt.Alternative = p.parseBlockStmt()
		}
	}

	return stmt
}

func (p *Parser) parseForStmt() *ast.ForStmt {
	stmt := &ast.ForStmt{Token: p.curToken}
	p.nextToken()

	if p.curTokenIs(token.LBRACE) {
		stmt.Body = p.parseBlockStmt()
		return stmt
	}

	firstStmt := p.parseStatement()

	if p.peekTokenIs(token.SEMICOLON) {
		stmt.Init = firstStmt
		p.nextToken()
		p.nextToken()

		if !p.curTokenIs(token.SEMICOLON) {
			stmt.Cond = p.parseExpression(LOWEST)
			if !p.expectPeek(token.SEMICOLON) {
				return nil
			}
		}

		p.nextToken()

		if !p.curTokenIs(token.LBRACE) {
			stmt.Post = p.parseStatement()
			if !p.expectPeek(token.LBRACE) {
				return nil
			}
		}

		stmt.Body = p.parseBlockStmt()
		return stmt
	}

	if exprStmt, ok := firstStmt.(*ast.ExprStmt); ok {
		stmt.Cond = exprStmt.Expr
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStmt()
	return stmt
}

func (p *Parser) parseSwitchStmt() *ast.SwitchStmt {
	stmt := &ast.SwitchStmt{Token: p.curToken, Cases: []*ast.CaseClause{}}
	p.nextToken() // 'switch' を消費して比較対象式へ

	stmt.Value = p.parseExpression(LOWEST)

	if !p.expectPeek(token.LBRACE) {
		return nil
	}

	for !p.peekTokenIs(token.RBRACE) && !p.peekTokenIs(token.EOF) {
		p.nextToken()
		if p.curTokenIs(token.CASE) || p.curTokenIs(token.DEFAULT) {
			clause := p.parseCaseClause()
			if clause != nil {
				stmt.Cases = append(stmt.Cases, clause)
			}
		}
	}

	if !p.expectPeek(token.RBRACE) {
		return nil
	}

	return stmt
}

func (p *Parser) parseCaseClause() *ast.CaseClause {
	clause := &ast.CaseClause{Token: p.curToken, Values: []ast.Expression{}, Body: []ast.Statement{}}

	if p.curTokenIs(token.CASE) {
		p.nextToken() // 'case' を消費して最初の値へ
		clause.Values = append(clause.Values, p.parseExpression(LOWEST))

		for p.peekTokenIs(token.COMMA) {
			p.nextToken() // ',' を消費
			p.nextToken() // 次の式へ移動
			clause.Values = append(clause.Values, p.parseExpression(LOWEST))
		}
	}

	if !p.expectPeek(token.COLON) {
		return nil
	}

	for !p.peekTokenIs(token.CASE) && !p.peekTokenIs(token.DEFAULT) && !p.peekTokenIs(token.RBRACE) && !p.peekTokenIs(token.EOF) {
		p.nextToken()
		if p.curTokenIs(token.SEMICOLON) {
			continue
		}
		stmt := p.parseStatement()
		if stmt != nil {
			clause.Body = append(clause.Body, stmt)
		}
	}

	return clause
}

func (p *Parser) parseReturnStmt() *ast.ReturnStmt {
	stmt := &ast.ReturnStmt{Token: p.curToken, Values: []ast.Expression{}}

	// 引数なし return (直後が ';' または '}') の場合は式を読み進めない
	if p.peekTokenIs(token.SEMICOLON) || p.peekTokenIs(token.RBRACE) {
		return stmt
	}

	p.nextToken() // 最初の戻り値式へ進む
	stmt.Values = append(stmt.Values, p.parseExpression(LOWEST))

	for p.peekTokenIs(token.COMMA) {
		p.nextToken() // ',' を消費
		p.nextToken() // 次の式へ移動
		stmt.Values = append(stmt.Values, p.parseExpression(LOWEST))
	}

	return stmt
}

func (p *Parser) parseDeferStmt() *ast.DeferStmt {
	stmt := &ast.DeferStmt{Token: p.curToken}
	p.nextToken()
	expr := p.parseExpression(LOWEST)
	if call, ok := expr.(*ast.CallExpr); ok {
		stmt.Call = call
	}
	return stmt
}

func (p *Parser) parseExpressionOrAssignStmt() ast.Statement {
	expr := p.parseExpression(LOWEST)

	if p.peekTokenIs(token.ASSIGN) || p.peekTokenIs(token.DEFINE) {
		p.nextToken() // '=' または ':='
		p.nextToken() // 右辺の先頭
		rhs := p.parseExpression(LOWEST)
		return &ast.AssignStmt{
			Left:  []ast.Expression{expr},
			Right: []ast.Expression{rhs},
		}
	}

	if p.peekTokenIs(token.COMMA) {
		assignStmt := &ast.AssignStmt{
			Left:  []ast.Expression{expr},
			Right: []ast.Expression{},
		}
		for p.peekTokenIs(token.COMMA) {
			p.nextToken()
			p.nextToken()
			assignStmt.Left = append(assignStmt.Left, p.parseExpression(LOWEST))
		}
		if p.peekTokenIs(token.ASSIGN) || p.peekTokenIs(token.DEFINE) {
			p.nextToken()
			p.nextToken()
			assignStmt.Right = append(assignStmt.Right, p.parseExpression(LOWEST))
			return assignStmt
		}
	}

	return &ast.ExprStmt{Token: p.curToken, Expr: expr}
}
