package parser

import (
	"fmt"
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
	LESSGREATER // <, >, <=, >=
	SUM         // +, -
	PRODUCT     // *, /
	PREFIX      // -X, !X, *X
	CALL        // myFunction(X)
	INDEX       // array[index], slice[low:high]
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

type Parser struct {
	l         *lexer.Lexer
	errors    []string
	curToken  token.Token
	peekToken token.Token
	verbose   bool
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:       l,
		errors:  []string{},
		verbose: false,
	}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) SetVerbose(v bool) {
	p.verbose = v
	if p.l != nil {
		p.l.SetVerbose(v)
	}
}

func (p *Parser) log(msg string) {
	if p.verbose {
		fmt.Printf("[PARSER] [Line %d:%d] %s\n", p.curToken.Line, p.curToken.Col, msg)
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
	p.peekError(t)
	return false
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead", t, p.peekToken.Type)
	p.errors = append(p.errors, msg)
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

func isTypeStart(tok token.Token) bool {
	switch tok.Type {
	case token.IDENT, token.ASTERISK, token.LPAREN, token.STRUCT, token.LBRACKET:
		return true
	default:
		return false
	}
}

func (p *Parser) ParseProgram() *ast.Program {
	prog := &ast.Program{
		Imports: []*ast.ImportDecl{},
		Decls:   []ast.Decl{},
	}

	if p.curTokenIs(token.PACKAGE) {
		if p.expectPeek(token.IDENT) {
			prog.Package = p.curToken.Literal
			p.log(fmt.Sprintf("Declared package '%s'", prog.Package))
			p.nextToken()
		}
	}

	for p.curTokenIs(token.IMPORT) {
		if p.peekTokenIs(token.STRING) {
			p.nextToken()
			p.log(fmt.Sprintf("Imported '%s'", p.curToken.Literal))
			prog.Imports = append(prog.Imports, &ast.ImportDecl{Token: p.curToken, Path: p.curToken.Literal})
			p.nextToken()
		} else if p.peekTokenIs(token.LPAREN) {
			p.nextToken()
			p.nextToken()
			for !p.curTokenIs(token.RPAREN) && !p.curTokenIs(token.EOF) {
				if p.curTokenIs(token.STRING) {
					p.log(fmt.Sprintf("Imported '%s'", p.curToken.Literal))
					prog.Imports = append(prog.Imports, &ast.ImportDecl{Token: p.curToken, Path: p.curToken.Literal})
				}
				p.nextToken()
			}
			if p.curTokenIs(token.RPAREN) {
				p.nextToken()
			}
		} else {
			p.nextToken()
		}
	}

	for !p.curTokenIs(token.EOF) {
		decl := p.parseDecl()
		if decl != nil {
			prog.Decls = append(prog.Decls, decl)
		}
		p.nextToken()
	}

	return prog
}

func (p *Parser) parseDecl() ast.Decl {
	switch p.curToken.Type {
	case token.TYPE:
		return p.parseTypeDecl()
	case token.FUNC:
		return p.parseFuncDecl()
	case token.CONST:
		return p.parseConstDecl()
	default:
		return nil
	}
}

func (p *Parser) parseConstDecl() *ast.ConstDecl {
	tok := p.curToken
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	ident := p.parseIdentifier()
	if !p.expectPeek(token.ASSIGN) {
		return nil
	}
	p.nextToken()
	val := p.parseExpression(LOWEST)
	p.log(fmt.Sprintf("Parsed const declaration: %s", ident.Value))
	return &ast.ConstDecl{Token: tok, Name: ident, Value: val}
}

func (p *Parser) parseTypeDecl() *ast.TypeDecl {
	tok := p.curToken
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	name := p.parseIdentifier()
	p.nextToken()
	t := p.parseTypeExpr()
	p.log(fmt.Sprintf("Parsed type declaration: %s", name.Value))
	return &ast.TypeDecl{Token: tok, Name: name, Type: t}
}

func (p *Parser) parseFuncDecl() *ast.FuncDecl {
	tok := p.curToken
	var recv *ast.ParamDecl = nil

	if p.peekTokenIs(token.LPAREN) {
		p.nextToken()
		p.nextToken()
		rName := p.parseIdentifier()
		p.nextToken()
		rType := p.parseTypeExpr()
		if !p.expectPeek(token.RPAREN) {
			return nil
		}
		recv = &ast.ParamDecl{Token: rName.Token, Name: rName, Type: rType}
	}

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	fnName := p.parseIdentifier()
	p.log(fmt.Sprintf("Parsing function: %s", fnName.Value))

	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	params := []*ast.ParamDecl{}
	isVariadic := false

	if !p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		for {
			if p.curTokenIs(token.ELLIPSIS) {
				isVariadic = true
				p.log("Parsed variadic parameter ellipsis '...'")
				if p.peekTokenIs(token.COMMA) {
					p.nextToken()
					p.nextToken()
					continue
				}
				break
			}

			pName := p.parseIdentifier()
			p.nextToken()

			if p.curTokenIs(token.ELLIPSIS) {
				isVariadic = true
				p.nextToken()
			}

			pType := p.parseTypeExpr()
			params = append(params, &ast.ParamDecl{Token: pName.Token, Name: pName, Type: pType})

			if p.peekTokenIs(token.COMMA) {
				p.nextToken()
				p.nextToken()
			} else {
				break
			}
		}
	}
	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	retTypes := []ast.TypeExpr{}
	if isTypeStart(p.peekToken) {
		p.nextToken()
		if p.curTokenIs(token.LPAREN) {
			p.nextToken()
			for !p.curTokenIs(token.RPAREN) && !p.curTokenIs(token.EOF) {
				retTypes = append(retTypes, p.parseTypeExpr())
				if p.peekTokenIs(token.COMMA) {
					p.nextToken()
					p.nextToken()
				} else {
					break
				}
			}
			p.expectPeek(token.RPAREN)
		} else {
			retTypes = append(retTypes, p.parseTypeExpr())
		}
	}

	var body *ast.BlockStmt = nil
	if p.peekTokenIs(token.LBRACE) {
		p.nextToken()
		body = p.parseBlockStmt()
	}

	return &ast.FuncDecl{
		Token:       tok,
		Receiver:    recv,
		Name:        fnName,
		Params:      params,
		ReturnTypes: retTypes,
		Body:        body,
		IsVariadic:  isVariadic,
	}
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
	case token.IF:
		return p.parseIfStmt()
	case token.FOR:
		return p.parseForStmt()
	case token.SWITCH:
		return p.parseSwitchStmt()
	case token.DEFER:
		return p.parseDeferStmt()
	default:
		return p.parseAssignOrExprStmt()
	}
}

func (p *Parser) parseReturnStmt() *ast.ReturnStmt {
	stmt := &ast.ReturnStmt{Token: p.curToken, Values: []ast.Expression{}}
	if p.peekTokenIs(token.SEMICOLON) || p.peekTokenIs(token.RBRACE) {
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

func (p *Parser) parseIfStmt() *ast.IfStmt {
	stmt := &ast.IfStmt{Token: p.curToken}
	p.nextToken()
	stmt.Condition = p.parseExpression(LOWEST)
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	stmt.Consequence = p.parseBlockStmt()

	if p.peekTokenIs(token.ELSE) {
		p.nextToken()
		if p.peekTokenIs(token.IF) {
			p.nextToken()
			stmt.Alternative = p.parseIfStmt()
		} else if p.expectPeek(token.LBRACE) {
			stmt.Alternative = p.parseBlockStmt()
		}
	}
	return stmt
}

func (p *Parser) parseForStmt() *ast.ForStmt {
	stmt := &ast.ForStmt{Token: p.curToken}
	p.nextToken() // 'for' を消費

	// 1. 無限ループ: for { ... }
	if p.curTokenIs(token.LBRACE) {
		stmt.Body = p.parseBlockStmt()
		return stmt
	}

	// 2. 空の初期化節で始まる 3節 for: for ; cond; post { ... }
	if p.curTokenIs(token.SEMICOLON) {
		p.nextToken()
		if !p.curTokenIs(token.SEMICOLON) {
			stmt.Cond = p.parseExpression(LOWEST)
		}
		p.expectPeek(token.SEMICOLON)
		p.nextToken()
		if !p.curTokenIs(token.LBRACE) {
			stmt.Post = p.parseAssignOrExprStmt()
		}
		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
			stmt.Body = p.parseBlockStmt()
		}
		return stmt
	}

	// 最初の要素を文として解析 (代入文 'i := 0' または 式文 'i < 100')
	firstStmt := p.parseAssignOrExprStmt()

	// 3. 3節 for: for init; cond; post { ... }
	if p.peekTokenIs(token.SEMICOLON) {
		stmt.Init = firstStmt
		p.nextToken() // ';' に移動
		p.nextToken() // ';' の次へ

		// 条件節 (cond)
		if !p.curTokenIs(token.SEMICOLON) {
			stmt.Cond = p.parseExpression(LOWEST)
		}

		// 後処理節 (post)
		if p.peekTokenIs(token.SEMICOLON) {
			p.nextToken() // ';' に移動
			p.nextToken() // ';' の次へ
			if !p.curTokenIs(token.LBRACE) {
				stmt.Post = p.parseAssignOrExprStmt()
			}
		}

		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
			stmt.Body = p.parseBlockStmt()
		}
		return stmt
	}

	// 4. 単一条件 for: for cond { ... }
	if exprStmt, ok := firstStmt.(*ast.ExprStmt); ok {
		stmt.Cond = exprStmt.Expr
	}
	if p.peekTokenIs(token.LBRACE) {
		p.nextToken()
		stmt.Body = p.parseBlockStmt()
	}

	return stmt
}

func (p *Parser) parseSwitchStmt() *ast.SwitchStmt {
	stmt := &ast.SwitchStmt{Token: p.curToken, Cases: []*ast.CaseClause{}}
	p.nextToken()
	stmt.Value = p.parseExpression(LOWEST)
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	p.nextToken()

	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.CASE) || p.curTokenIs(token.DEFAULT) {
			clause := &ast.CaseClause{Token: p.curToken, Values: []ast.Expression{}, Body: []ast.Statement{}}
			if p.curTokenIs(token.CASE) {
				p.nextToken()
				for {
					clause.Values = append(clause.Values, p.parseExpression(LOWEST))
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
				clause.Body = append(clause.Body, p.parseStatement())
				p.nextToken()
			}
			stmt.Cases = append(stmt.Cases, clause)
		} else {
			p.nextToken()
		}
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

func (p *Parser) parseAssignOrExprStmt() ast.Statement {
	startTok := p.curToken
	leftExpr := p.parseExpression(LOWEST)

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
		leftExp = p.parseIdentifier()
	case token.INT:
		leftExp = p.parseIntegerLiteral()
	case token.STRING:
		leftExp = p.parseStringLiteral()
	case token.NIL:
		leftExp = &ast.NilLiteral{Token: p.curToken}
	case token.BANG, token.MINUS, token.ASTERISK, token.AMPERSAND:
		leftExp = p.parsePrefixExpr()
	case token.LBRACKET:
		// []T スライス型表現
		tok := p.curToken
		if p.expectPeek(token.RBRACKET) {
			p.nextToken() // ']' の次（要素型）へ
			elem := p.parseTypeExpr()
			leftExp = &ast.SliceType{Token: tok, Elem: elem}
		} else {
			return nil
		}
	case token.LPAREN:
		p.nextToken()
		leftExp = p.parseExpression(LOWEST)
		p.expectPeek(token.RPAREN)
	default:
		return nil
	}

	for !p.peekTokenIs(token.SEMICOLON) && precedence < p.peekPrecedence() {
		switch p.peekToken.Type {
		case token.PLUS, token.MINUS, token.SLASH, token.ASTERISK, token.EQ, token.NEQ, token.LT, token.GT, token.LE, token.GE, token.LAND, token.LOR:
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
	var val int64
	fmt.Sscanf(p.curToken.Literal, "%d", &val)
	return &ast.IntegerLiteral{Token: p.curToken, Value: val}
}

func (p *Parser) parseStringLiteral() *ast.StringLiteral {
	return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parsePrefixExpr() *ast.PrefixExpr {
	tok := p.curToken
	op := p.curToken.Literal
	p.nextToken()
	right := p.parseExpression(PREFIX)
	return &ast.PrefixExpr{Token: tok, Operator: op, Right: right}
}

func (p *Parser) parseBinaryExpr(left ast.Expression) *ast.BinaryExpr {
	tok := p.curToken
	op := p.curToken.Literal
	prec := p.curPrecedence()
	p.nextToken()
	right := p.parseExpression(prec)
	return &ast.BinaryExpr{Token: tok, Left: left, Operator: op, Right: right}
}

func (p *Parser) parseCallExpr(fn ast.Expression) *ast.CallExpr {
	tok := p.curToken
	args := []ast.Expression{}
	if !p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		for {
			args = append(args, p.parseExpression(LOWEST))
			if p.peekTokenIs(token.COMMA) {
				p.nextToken()
				p.nextToken()
			} else {
				break
			}
		}
	}
	p.expectPeek(token.RPAREN)
	return &ast.CallExpr{Token: tok, Function: fn, Args: args}
}

// インデックス式およびスライス式の解析
func (p *Parser) parseIndexExpr(left ast.Expression) ast.Expression {
	tok := p.curToken // '['

	// s[:high] または s[:] (low省略)
	if p.peekTokenIs(token.COLON) {
		p.nextToken() // ':'
		var high ast.Expression = nil
		if !p.peekTokenIs(token.RBRACKET) {
			p.nextToken()
			high = p.parseExpression(LOWEST)
		}
		p.expectPeek(token.RBRACKET)
		return &ast.SliceExpr{Token: tok, Left: left, Low: nil, High: high}
	}

	p.nextToken()
	low := p.parseExpression(LOWEST)

	// s[low:high] または s[low:]
	if p.peekTokenIs(token.COLON) {
		p.nextToken() // ':'
		var high ast.Expression = nil
		if !p.peekTokenIs(token.RBRACKET) {
			p.nextToken()
			high = p.parseExpression(LOWEST)
		}
		p.expectPeek(token.RBRACKET)
		return &ast.SliceExpr{Token: tok, Left: left, Low: low, High: high}
	}

	// s[i] (通常インデックス)
	p.expectPeek(token.RBRACKET)
	return &ast.IndexExpr{Token: tok, Left: left, Index: low}
}

func (p *Parser) parseMemberExpr(obj ast.Expression) *ast.MemberExpr {
	tok := p.curToken
	p.nextToken()
	field := p.parseIdentifier()
	return &ast.MemberExpr{Token: tok, Object: obj, Field: field}
}

func (p *Parser) parseTypeExpr() ast.TypeExpr {
	// []T の判定
	if p.curTokenIs(token.LBRACKET) {
		tok := p.curToken
		if p.expectPeek(token.RBRACKET) {
			p.nextToken() // ']' の次 (要素型) へ
			elem := p.parseTypeExpr()
			return &ast.SliceType{Token: tok, Elem: elem}
		}
		return nil
	}

	if p.curTokenIs(token.ASTERISK) {
		tok := p.curToken
		p.nextToken()
		base := p.parseTypeExpr()
		return &ast.PointerType{Token: tok, Base: base}
	}

	if p.curTokenIs(token.STRUCT) {
		tok := p.curToken
		p.expectPeek(token.LBRACE)
		p.nextToken()
		fields := []*ast.FieldDecl{}
		for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
			if p.curTokenIs(token.SEMICOLON) {
				p.nextToken()
				continue
			}
			fName := p.parseIdentifier()
			p.nextToken()
			fType := p.parseTypeExpr()
			fields = append(fields, &ast.FieldDecl{Token: fName.Token, Name: fName, Type: fType})
			p.nextToken()
		}
		return &ast.StructType{Token: tok, Fields: fields}
	}

	name := p.parseIdentifier()
	if p.peekTokenIs(token.DOT) {
		p.nextToken()
		p.nextToken()
		field := p.parseIdentifier()
		return &ast.NamedType{Token: name.Token, Package: name, Name: field}
	}

	return &ast.NamedType{Token: name.Token, Package: nil, Name: name}
}
