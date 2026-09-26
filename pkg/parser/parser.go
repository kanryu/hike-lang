package parser

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/token"
)

// -----------------------------------------------------------------------------
// FIFO パースタスク定義
// -----------------------------------------------------------------------------

type ParseTask struct {
	Parent ast.Node      // *ast.FuncDecl, *ast.CFuncDecl, *ast.TypeDecl, *ast.FuncLit
	Tokens []token.Token // 波括弧を含む本体ブロックのトークン列スライス
}

// -----------------------------------------------------------------------------
// パーサー構造体
// -----------------------------------------------------------------------------

type Parser struct {
	tokens         []token.Token
	pos            int
	curToken       token.Token
	peekToken      token.Token
	errors         []string
	verbose        bool
	allowStructLit bool
	queue          []*ParseTask
}

func New(l *lexer.Lexer) *Parser {
	tokens := []token.Token{}
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == token.EOF {
			break
		}
	}
	return NewFromTokens(tokens)
}

func NewFromTokens(tokens []token.Token) *Parser {
	p := &Parser{
		tokens:         tokens,
		pos:            0,
		errors:         []string{},
		verbose:        false,
		allowStructLit: true,
		queue:          make([]*ParseTask, 0),
	}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) newSubParser(tokens []token.Token) *Parser {
	sp := NewFromTokens(tokens)
	sp.verbose = p.verbose
	sp.allowStructLit = true
	// サブパーサーは独自の「空のキュー」を持つ（親キューを複製・混同させない）
	sp.queue = make([]*ParseTask, 0)
	return sp
}

func (p *Parser) enqueue(task *ParseTask) {
	p.queue = append(p.queue, task)
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
	if p.pos < len(p.tokens) {
		p.peekToken = p.tokens[p.pos]
		p.pos++
	} else {
		p.peekToken = token.Token{Type: token.EOF, Literal: ""}
	}
}

// curIdx は現在の curToken が大元スライスのどのインデックスにあるかを返す
func (p *Parser) curIdx() int {
	return p.pos - 2
}

func (p *Parser) jumpTo(targetIdx int) {
	p.pos = targetIdx
	p.nextToken()
	p.nextToken()
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
	p.errors = append(p.errors, fmt.Sprintf("line %d:%d: expected next token to be %s, got %s instead", p.peekToken.Line, p.peekToken.Col, t, p.peekToken.Type))
	return false
}

func (p *Parser) expectCurrent(t token.TokenType) bool {
	if p.curTokenIs(t) {
		return true
	}
	p.errors = append(p.errors, fmt.Sprintf("expected current token to be %s, got %s instead", t, p.curToken.Type))
	return false
}

// cutBraceBlock は startIdx（'{'）から対応する '}' までのスライスと次のトークン位置を返す
func (p *Parser) cutBraceBlock(startIdx int) ([]token.Token, int) {
	if startIdx < 0 || startIdx >= len(p.tokens) {
		return nil, startIdx
	}
	if p.tokens[startIdx].Type != token.LBRACE {
		for startIdx < len(p.tokens) && p.tokens[startIdx].Type != token.LBRACE {
			startIdx++
		}
		if startIdx >= len(p.tokens) {
			return nil, len(p.tokens)
		}
	}

	depth := 0
	for i := startIdx; i < len(p.tokens); i++ {
		tok := p.tokens[i]
		if tok.Type == token.LBRACE {
			depth++
		} else if tok.Type == token.RBRACE {
			depth--
			if depth == 0 {
				return p.tokens[startIdx : i+1], i + 1
			}
		}
	}
	return p.tokens[startIdx:], len(p.tokens)
}

// -----------------------------------------------------------------------------
// FIFO駆動メインループ (ParseProgram)
// -----------------------------------------------------------------------------

func (p *Parser) ParseProgram() *ast.Program {
	prog := &ast.Program{
		Decls:   []ast.Decl{},
		Imports: []*ast.ImportDecl{},
	}

	// Pass 1: トップレベル宣言の骨格（シグネチャ）のみを走査・登録
	for !p.curTokenIs(token.EOF) {
		switch p.curToken.Type {
		case token.PACKAGE:
			p.nextToken()
			if p.curTokenIs(token.IDENT) {
				prog.Package = p.curToken.Literal
				p.log(fmt.Sprintf("[%d:%d] Declared package '%s'", p.curToken.Line, p.curToken.Col, prog.Package))
			}
			p.nextToken()

		case token.IMPORT:
			imports := p.parseImportDecl()
			prog.Imports = append(prog.Imports, imports...)
			p.nextToken()

		case token.CONST:
			constDecls := p.parseConstDecl()
			prog.Decls = append(prog.Decls, constDecls...)
			p.nextToken()

		case token.FUNC:
			fn := p.parseFuncDecl()
			if fn != nil {
				prog.Decls = append(prog.Decls, fn)
			}

		case token.EXTERN:
			efn := p.parseExternFuncDecl()
			if efn != nil {
				prog.Decls = append(prog.Decls, efn)
			}

		case token.JFUNC:
			jfn := p.parseJFuncDecl()
			if jfn != nil {
				prog.Decls = append(prog.Decls, jfn)
			}

		case token.PASSTHROUGH:
			p.nextToken()
			if !p.curTokenIs(token.CFUNC) {
				p.errors = append(p.errors, fmt.Sprintf("line %d:%d: expected 'cfunc' after 'passthrough'", p.curToken.Line, p.curToken.Col))
				p.nextToken()
				continue
			}
			cfn := p.parseCFuncDecl()
			if cfn != nil {
				cfn.IsPassThrough = true
				prog.Decls = append(prog.Decls, cfn)
			}

		case token.CFUNC:
			cfn := p.parseCFuncDecl()
			if cfn != nil {
				prog.Decls = append(prog.Decls, cfn)
			}

		case token.TYPE:
			td := p.parseTypeDecl()
			if td != nil {
				prog.Decls = append(prog.Decls, td)
			}

		case token.VAR:
			if p.peekTokenIs(token.THREADABLE) || p.peekTokenIs(token.CONCURRENT) {
				md := p.parseMemoryBlockDecl()
				if md != nil {
					prog.Decls = append(prog.Decls, md)
				}
				p.nextToken()
			} else if p.peekTokenIs(token.LPAREN) {
				// Go-style grouped variable declarations: var ( A = ...; B = ... ).
				p.nextToken() // '('
				for !p.peekTokenIs(token.RPAREN) && !p.peekTokenIs(token.EOF) {
					p.nextToken()
					if p.curTokenIs(token.SEMICOLON) {
						continue
					}
					vd := p.parseVarSpec()
					if vd != nil {
						prog.Decls = append(prog.Decls, vd)
					}
				}
				p.nextToken() // ')'
			} else {
				vd := p.parseVarDecl()
				if vd != nil {
					prog.Decls = append(prog.Decls, vd)
				}
				p.nextToken()
			}

		case token.SEMICOLON:
			// トップレベルの改行・セミコロンをスキップ
			p.nextToken()

		default:
			p.nextToken()
		}
	}

	// Pass 2: FIFOキューからタスクを取り出し、確定した親ノードの文脈で内容物をパース
	for len(p.queue) > 0 {
		task := p.queue[0]
		p.queue = p.queue[1:]

		subParser := p.newSubParser(task.Tokens)

		switch node := task.Parent.(type) {
		case *ast.FuncDecl:
			node.Body = subParser.parseBlockStmt()
		case *ast.CFuncDecl:
			node.Body = subParser.parseBlockStmt()
		case *ast.FuncLit:
			node.Body = subParser.parseBlockStmt()
		case *ast.TypeDecl:
			if st, ok := node.Type.(*ast.StructType); ok {
				subParser.parseStructFields(st)
			} else if it, ok := node.Type.(*ast.InterfaceType); ok {
				subParser.parseInterfaceMethods(it)
			}
		}

		if len(subParser.errors) > 0 {
			p.errors = append(p.errors, subParser.errors...)
		}
		if len(subParser.queue) > 0 {
			p.queue = append(p.queue, subParser.queue...)
		}
	}

	return prog
}

// -----------------------------------------------------------------------------
// パラメータリスト共通パース (デフォルト引数および末尾配置規則の検証付き)
// -----------------------------------------------------------------------------

func (p *Parser) parseParameterList(allowBareEllipsis bool) ([]*ast.ParamDecl, bool) {
	params := []*ast.ParamDecl{}
	isVariadic := false
	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return params, isVariadic
	}

	p.nextToken()
	hasDefault := false

	for {
		if allowBareEllipsis && p.curTokenIs(token.ELLIPSIS) {
			isVariadic = true
			if p.peekTokenIs(token.RPAREN) {
				break
			}
			p.nextToken()
		} else {
			// Function types and literals commonly omit parameter names, for
			// example func([]byte). Treat an unambiguously type-shaped token as
			// an unnamed parameter instead of trying to parse it as an identifier.
			if p.curTokenIs(token.ASTERISK) || p.curTokenIs(token.LBRACKET) ||
				p.curTokenIs(token.MAP) || p.curTokenIs(token.CHAN) || p.curTokenIs(token.FUNC) ||
				p.curTokenIs(token.INTERFACE) || (p.curTokenIs(token.IDENT) &&
				(p.peekTokenIs(token.RPAREN) || (p.peekTokenIs(token.COMMA) && !p.hasNamedParameterGroup()) || p.peekTokenIs(token.DOT))) {
				pType := p.parseTypeExpr()
				params = append(params, &ast.ParamDecl{Token: p.curToken, Name: &ast.Identifier{Token: p.curToken, Value: ""}, Type: pType})
			} else {
				// Go permits a shared type for a comma-separated group of names:
				// func f(first, second string). Expand the group into ordinary Hike
				// parameter declarations so later phases need no special case.
				if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.COMMA) {
					names := []*ast.Identifier{p.parseIdentifier()}
					idx := p.curIdx()
					typeIdx := -1
					for idx+2 < len(p.tokens) && p.tokens[idx+1].Type == token.COMMA {
						candidate := idx + 2
						if p.tokens[candidate].Type != token.IDENT {
							break
						}
						// The shared type may start with any type-expression token,
						// not only an identifier. For example, Go permits
						// `params, returns []Type`.
						if candidate+1 < len(p.tokens) &&
							p.tokens[candidate+1].Type != token.COMMA &&
							p.tokens[candidate+1].Type != token.RPAREN {
							// An identifier followed by another comma-separated
							// identifier is still part of the shared name group:
							// before, parts, after []Part.
							if p.tokens[candidate+1].Type == token.IDENT && candidate+2 < len(p.tokens) && p.tokens[candidate+2].Type == token.COMMA {
								// An identifier followed by a comma is usually another
								// name, but it can also be the shared type immediately
								// before the next named parameter: `x, y T, z U`.
								if candidate+3 < len(p.tokens) &&
									(p.tokens[candidate+3].Type == token.RPAREN ||
										p.hasNamedParameterGroupAt(candidate+3) ||
										p.isNamedParameterStartAt(candidate+3)) {
									names = append(names, &ast.Identifier{Token: p.tokens[candidate], Value: p.tokens[candidate].Literal})
									typeIdx = candidate + 1
									break
								}
								names = append(names, &ast.Identifier{Token: p.tokens[candidate], Value: p.tokens[candidate].Literal})
								idx = candidate
								continue
							}
							names = append(names, &ast.Identifier{Token: p.tokens[candidate], Value: p.tokens[candidate].Literal})
							typeIdx = candidate + 1
							break
						}
						names = append(names, &ast.Identifier{Token: p.tokens[candidate], Value: p.tokens[candidate].Literal})
						idx = candidate
					}
					if typeIdx >= 0 && len(names) > 1 {
						for p.curIdx() < typeIdx {
							p.nextToken()
						}
						pType := p.parseTypeExpr()
						for _, name := range names {
							params = append(params, &ast.ParamDecl{Token: name.Token, Name: name, Type: pType})
						}
						if p.peekTokenIs(token.COMMA) {
							p.nextToken()
							if p.peekTokenIs(token.RPAREN) {
								break
							}
							p.nextToken()
							continue
						}
						break
					}
				}
				pName := p.parseIdentifier()
				p.nextToken()
				paramIsVariadic := false
				if p.curTokenIs(token.ELLIPSIS) {
					paramIsVariadic = true
					isVariadic = true
					p.nextToken()
					elemType := p.parseTypeExpr()
					pType := &ast.EllipsisType{Token: p.curToken, Elem: elemType}
					if hasDefault {
						p.errors = append(p.errors, fmt.Sprintf("line %d:%d: variadic parameter cannot follow default parameter", pName.Token.Line, pName.Token.Col))
					}
					params = append(params, &ast.ParamDecl{
						Token:      pName.Token,
						Name:       pName,
						Type:       pType,
						IsVariadic: paramIsVariadic,
					})
				} else {
					pType := p.parseTypeExpr()
					var defaultExpr ast.Expression = nil
					if p.peekTokenIs(token.ASSIGN) {
						p.nextToken() // '=' に進む
						p.nextToken() // 式の先頭トークンに進む
						defaultExpr = p.parseExpression(LOWEST)
						hasDefault = true
					} else if hasDefault {
						p.errors = append(p.errors, fmt.Sprintf("line %d:%d: non-default parameter '%s' follows default parameter; default arguments must be trailing",
							pName.Token.Line, pName.Token.Col, pName.Value))
					}

					params = append(params, &ast.ParamDecl{
						Token:      pName.Token,
						Name:       pName,
						Type:       pType,
						Default:    defaultExpr,
						IsVariadic: false,
					})
				}
			}
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

	p.expectPeek(token.RPAREN)
	return params, isVariadic
}

func (p *Parser) hasNamedParameterGroup() bool {
	return p.hasNamedParameterGroupAt(p.curIdx())
}

func (p *Parser) hasNamedParameterGroupAt(idx int) bool {
	if idx >= len(p.tokens) || p.tokens[idx].Type != token.IDENT {
		return false
	}

	// A shared declaration can contain any number of names, for example
	// `first, second, third string`. Walk the whole name list before deciding
	// whether the token after it starts the shared type. This is deliberately
	// conservative: `func(int, string)` must continue to be parsed as two
	// unnamed parameter types.
	for idx+2 < len(p.tokens) &&
		p.tokens[idx+1].Type == token.COMMA &&
		p.tokens[idx+2].Type == token.IDENT {
		next := idx + 3
		if next >= len(p.tokens) || p.tokens[next].Type == token.RPAREN {
			return false
		}
		if p.tokens[next].Type == token.COMMA {
			idx += 2
			continue
		}
		return isTypeStartToken(p.tokens[next].Type)
	}
	return false
}

func (p *Parser) isNamedParameterStartAt(idx int) bool {
	return idx+1 < len(p.tokens) &&
		p.tokens[idx].Type == token.IDENT &&
		p.tokens[idx+1].Type != token.COMMA &&
		p.tokens[idx+1].Type != token.RPAREN &&
		isTypeStartToken(p.tokens[idx+1].Type)
}

// parseReturnTypeList parses both Hike's type-only return list and Go's
// named-return form, such as (result string, err error). Names are currently
// accepted for compatibility; return values remain represented by types in
// the AST because Hike has no naked-return statement.
func (p *Parser) parseReturnTypeList() []ast.TypeExpr {
	returns, _ := p.parseReturnTypeListWithNames()
	return returns
}

func (p *Parser) parseReturnTypeListWithNames() ([]ast.TypeExpr, []string) {
	returns := []ast.TypeExpr{}
	names := []string{}
	p.nextToken() // '('
	p.nextToken() // first name or type
	for !p.curTokenIs(token.RPAREN) && !p.curTokenIs(token.EOF) {
		name := ""
		if p.curTokenIs(token.IDENT) && isTypeStartToken(p.peekToken.Type) {
			name = p.curToken.Literal
			p.nextToken() // skip a named result
		}
		returns = append(returns, p.parseTypeExpr())
		names = append(names, name)
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
	p.expectPeek(token.RPAREN)
	return returns, names
}

func isTypeStartToken(t token.TokenType) bool {
	switch t {
	case token.IDENT, token.ASTERISK, token.LBRACKET, token.MAP, token.CHAN, token.INTERFACE, token.FUNC, token.ELLIPSIS:
		return true
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// トップレベル宣言パース (ヘッダー確定 + スライスカット + エンキュー)
// -----------------------------------------------------------------------------

func (p *Parser) parseExternFuncDecl() *ast.ExternFuncDecl {
	efn := &ast.ExternFuncDecl{Token: p.curToken}
	p.nextToken() // 'extern' を消費

	if !p.curTokenIs(token.FUNC) {
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] expected 'func' after 'extern', got %s", p.curToken.Line, p.curToken.Col, p.curToken.Type))
		return nil
	}
	p.nextToken() // 'func' を消費

	efn.Name = p.parseIdentifier()
	p.log(fmt.Sprintf("[%d:%d] Parsing extern func: %s", efn.Token.Line, efn.Token.Col, efn.Name.Value))
	p.nextToken()

	efn.Params, efn.IsVariadic = p.parseParameterList(true)

	efn.ReturnTypes = []ast.TypeExpr{}
	if p.peekToken.Line == p.curToken.Line &&
		!p.curTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.SEMICOLON) &&
		!p.curTokenIs(token.ASSIGN) && !p.peekTokenIs(token.ASSIGN) &&
		!p.curTokenIs(token.EOF) && !p.peekTokenIs(token.EOF) {
		if p.peekTokenIs(token.LPAREN) {
			efn.ReturnTypes = p.parseReturnTypeList()
		} else {
			p.nextToken()
			efn.ReturnTypes = append(efn.ReturnTypes, p.parseTypeExpr())
		}
	}

	if p.peekTokenIs(token.ASSIGN) {
		p.nextToken()
		p.nextToken()
		efn.TargetCName = p.parseIdentifier()
	}

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}
	if p.curTokenIs(token.SEMICOLON) {
		p.nextToken()
	}

	return efn
}

func (p *Parser) parseJFuncDecl() *ast.JFuncDecl {
	jfn := &ast.JFuncDecl{Token: p.curToken}
	p.nextToken() // 'jfunc' を消費

	jfn.Name = p.parseIdentifier()
	p.log(fmt.Sprintf("[%d:%d] Parsing jfunc: %s", jfn.Token.Line, jfn.Token.Col, jfn.Name.Value))
	p.nextToken()

	jfn.Params, _ = p.parseParameterList(false)

	jfn.ReturnTypes = []ast.TypeExpr{}
	if p.peekToken.Line == p.curToken.Line &&
		!p.curTokenIs(token.LBRACE) && !p.peekTokenIs(token.LBRACE) &&
		!p.peekTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.EOF) && !p.curTokenIs(token.EOF) {
		if p.peekTokenIs(token.LPAREN) {
			jfn.ReturnTypes = p.parseReturnTypeList()
		} else {
			p.nextToken()
			jfn.ReturnTypes = append(jfn.ReturnTypes, p.parseTypeExpr())
		}
	}

	if p.peekTokenIs(token.LBRACE) || p.curTokenIs(token.LBRACE) {
		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
		}
		startIdx := p.curIdx()
		bodyTokens, nextIdx := p.cutBraceBlock(startIdx)
		jfn.JSBody, jfn.MainJSBody, jfn.WorkerJSBody, jfn.HasDirectives = parseJFuncBodies(bodyTokens[1 : len(bodyTokens)-1])
		p.jumpTo(nextIdx)
	} else {
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] expected '{' in jfunc declaration", p.curToken.Line, p.curToken.Col))
		p.nextToken()
		return nil
	}

	return jfn
}

func jfuncTokensToJS(tokens []token.Token) string {
	var jsCode string
	for _, tok := range tokens {
		if tok.Type == token.STRING {
			jsCode += "\"" + tok.Literal + "\" "
		} else {
			jsCode += tok.Literal + " "
		}
	}
	return jsCode
}

func parseJFuncBodies(tokens []token.Token) (string, string, string, bool) {
	mainBody, workerBody := "", ""
	hasDirectives := false
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].Type != token.IDENT || (tokens[i].Literal != "main" && tokens[i].Literal != "worker") || tokens[i+1].Type != token.LBRACE {
			continue
		}
		hasDirectives = true
		depth := 0
		end := i + 1
		for ; end < len(tokens); end++ {
			if tokens[end].Type == token.LBRACE {
				depth++
			} else if tokens[end].Type == token.RBRACE {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		body := ""
		if end > i+1 {
			body = jfuncTokensToJS(tokens[i+2 : end])
		}
		if tokens[i].Literal == "main" {
			mainBody = body
		} else {
			workerBody = body
		}
		i = end
	}
	if !hasDirectives {
		return jfuncTokensToJS(tokens), "", "", false
	}
	return "", mainBody, workerBody, true
}

func (p *Parser) parseCFuncDecl() *ast.CFuncDecl {
	cfn := &ast.CFuncDecl{Token: p.curToken}
	p.nextToken()

	cfn.Name = p.parseIdentifier()
	p.log(fmt.Sprintf("[%d:%d] Parsing cfunc: %s", cfn.Token.Line, cfn.Token.Col, cfn.Name.Value))
	p.nextToken()

	cfn.Params, cfn.IsVariadic = p.parseParameterList(true)

	cfn.ReturnTypes = []ast.TypeExpr{}
	if p.peekToken.Line == p.curToken.Line &&
		!p.curTokenIs(token.LBRACE) && !p.peekTokenIs(token.LBRACE) &&
		!p.curTokenIs(token.ASSIGN) && !p.peekTokenIs(token.ASSIGN) &&
		!p.peekTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.EOF) && !p.curTokenIs(token.EOF) {

		if p.peekTokenIs(token.LPAREN) {
			cfn.ReturnTypes = p.parseReturnTypeList()
		} else {
			p.nextToken()
			cfn.ReturnTypes = append(cfn.ReturnTypes, p.parseTypeExpr())
		}
	}

	if p.peekTokenIs(token.ASSIGN) {
		p.nextToken()
		p.nextToken()
		cfn.TargetCName = p.parseIdentifier()
		p.nextToken()
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
		}
	} else if p.peekTokenIs(token.LBRACE) || p.curTokenIs(token.LBRACE) {
		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
		}
		startIdx := p.curIdx()
		bodyTokens, nextIdx := p.cutBraceBlock(startIdx)
		cfn.BodyTokens = bodyTokens
		p.enqueue(&ParseTask{Parent: cfn, Tokens: bodyTokens})
		p.jumpTo(nextIdx)
	} else {
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] expected '=' or '{' in cfunc declaration", p.curToken.Line, p.curToken.Col))
		p.nextToken()
		return nil
	}

	return cfn
}

func (p *Parser) parseVarDecl() *ast.VarDecl {
	decl := &ast.VarDecl{Token: p.curToken}
	p.nextToken()
	return p.parseVarSpecWithToken(decl.Token)
}

func (p *Parser) parseMemoryBlockDecl() *ast.MemoryBlockDecl {
	varTok := p.curToken
	p.nextToken()
	block := &ast.MemoryBlockDecl{Token: varTok}
	switch p.curToken.Type {
	case token.THREADABLE:
		block.Kind = ast.ThreadableMemoryBlock
	case token.CONCURRENT:
		block.Kind = ast.ConcurrentMemoryBlock
	default:
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] expected threadable or concurrent memory block", p.curToken.Line, p.curToken.Col))
		return nil
	}
	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	if !p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		block.Size = p.parseExpression(LOWEST)
	}
	if !p.expectPeek(token.RPAREN) || !p.expectPeek(token.LBRACE) {
		return nil
	}

	// The block body is deliberately parsed as a list of variable
	// specifications. It does not introduce a function or local scope.
	p.nextToken()
	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}
		if !p.curTokenIs(token.IDENT) {
			p.errors = append(p.errors, fmt.Sprintf("[%d:%d] expected a variable name in memory block", p.curToken.Line, p.curToken.Col))
			p.nextToken()
			continue
		}
		vd := p.parseVarSpec()
		if vd != nil {
			if first, _ := utf8.DecodeRuneInString(vd.Name.Value); unicode.IsUpper(first) {
				p.errors = append(p.errors, fmt.Sprintf("[%d:%d] memory block variable %q must start with a lowercase letter", vd.Token.Line, vd.Token.Col, vd.Name.Value))
			}
			block.Vars = append(block.Vars, vd)
		}
		p.nextToken()
	}
	return block
}

// parseVarSpec parses one variable specification with the current token on
// its identifier. It is shared by single and grouped var declarations.
func (p *Parser) parseVarSpec() *ast.VarDecl {
	return p.parseVarSpecWithToken(p.curToken)
}

func (p *Parser) parseVarSpecWithToken(tok token.Token) *ast.VarDecl {
	decl := &ast.VarDecl{Token: tok}
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
			alias := ""
			if p.curTokenIs(token.IDENT) {
				alias = p.curToken.Literal
				p.nextToken()
			}
			if p.curTokenIs(token.STRING) {
				imports = append(imports, &ast.ImportDecl{Token: p.curToken, Alias: alias, Path: p.curToken.Literal})
				p.log(fmt.Sprintf("[%d:%d] Imported '%s'", p.curToken.Line, p.curToken.Col, p.curToken.Literal))
			}
		}
		p.expectPeek(token.RPAREN)
	} else {
		alias := ""
		if p.curTokenIs(token.IDENT) {
			alias = p.curToken.Literal
			p.nextToken()
		}
		if p.curTokenIs(token.STRING) {
			imports = append(imports, &ast.ImportDecl{Token: p.curToken, Alias: alias, Path: p.curToken.Literal})
			p.log(fmt.Sprintf("[%d:%d] Imported '%s'", p.curToken.Line, p.curToken.Col, p.curToken.Literal))
		}
	}
	return imports
}

func (p *Parser) parseTypeParams() []*ast.TypeParam {
	if !p.curTokenIs(token.LBRACKET) {
		return nil
	}
	p.nextToken()
	params := []*ast.TypeParam{}
	for !p.curTokenIs(token.RBRACKET) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.IDENT) {
			ident := p.parseIdentifier()
			param := &ast.TypeParam{
				Token: ident.Token,
				Name:  ident,
			}
			// Constraints use the compact form `Rows int` (and accept
			// `Rows: int` for readability).  They are metadata on the
			// declaration; instantiation arguments are still parsed separately.
			if p.peekTokenIs(token.COLON) {
				p.nextToken()
			}
			if p.peekTokenIs(token.IDENT) && p.peekToken.Literal == "uint" {
				p.nextToken()
				param.Constraint = p.parseTypeExpr()
			}
			params = append(params, param)
		}
		if p.peekTokenIs(token.COMMA) {
			p.nextToken()
			p.nextToken()
		} else {
			break
		}
	}
	p.expectPeek(token.RBRACKET)
	return params
}

func (p *Parser) parseTypeDecl() *ast.TypeDecl {
	stmt := &ast.TypeDecl{Token: p.curToken}
	p.nextToken()
	stmt.Name = p.parseIdentifier()
	p.nextToken()
	// Go-style type aliases use `type Name = ExistingType`. The equals sign
	// is declaration syntax, not part of the type expression.
	if p.curTokenIs(token.ASSIGN) {
		p.nextToken()
	}

	// A leading [] is a slice type, not a type-parameter list.
	// A type parameter list starts with an identifier (`[T any]`). An
	// integer immediately after `[` is an array length (`[4]uint32`) and
	// must be left for parseTypeExpr.
	if p.curTokenIs(token.LBRACKET) && p.peekTokenIs(token.IDENT) {
		stmt.TypeParams = p.parseTypeParams()
		p.nextToken()
	}

	if p.curTokenIs(token.STRUCT) {
		st := &ast.StructType{Token: p.curToken, Fields: []*ast.FieldDecl{}}
		stmt.Type = st
		if p.expectPeek(token.LBRACE) {
			startIdx := p.curIdx()
			fieldTokens, nextIdx := p.cutBraceBlock(startIdx)
			st.FieldTokens = fieldTokens
			p.enqueue(&ParseTask{Parent: stmt, Tokens: fieldTokens})
			p.jumpTo(nextIdx)
		}
	} else if p.curTokenIs(token.INTERFACE) || (p.curTokenIs(token.IDENT) && p.curToken.Literal == "interface") {
		it := &ast.InterfaceType{Token: p.curToken, Methods: []*ast.MethodSig{}}
		stmt.Type = it
		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
			startIdx := p.curIdx()
			methodTokens, nextIdx := p.cutBraceBlock(startIdx)
			it.MethodTokens = methodTokens
			p.enqueue(&ParseTask{Parent: stmt, Tokens: methodTokens})
			p.jumpTo(nextIdx)
		}
	} else {
		stmt.Type = p.parseTypeExpr()
		p.nextToken()
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
		}
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
			if p.curTokenIs(token.SEMICOLON) {
				p.nextToken()
				continue
			}
			if p.curTokenIs(token.IDENT) {
				name := p.parseIdentifier()
				var valExpr ast.Expression = nil

				if !p.peekTokenIs(token.ASSIGN) && !p.peekTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.RPAREN) && !p.peekTokenIs(token.EOF) {
					p.nextToken()
					_ = p.parseTypeExpr()
				}

				if p.peekTokenIs(token.ASSIGN) {
					p.nextToken()
					p.nextToken()
					valExpr = p.parseExpression(LOWEST)
					lastExpr = valExpr
				} else if lastExpr != nil {
					valExpr = lastExpr
				} else {
					valExpr = &ast.IntegerLiteral{Token: name.Token, Value: iotaVal}
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

		if !p.peekTokenIs(token.ASSIGN) && !p.peekTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.EOF) {
			p.nextToken()
			_ = p.parseTypeExpr()
		}

		if p.peekTokenIs(token.ASSIGN) {
			p.nextToken()
			p.nextToken()
			valExpr = p.parseExpression(LOWEST)
			valExpr = replaceIota(valExpr, 0)
		}
		decls = append(decls, &ast.ConstDecl{
			Token: name.Token,
			Name:  name,
			Value: valExpr,
		})
	}
	return decls
}

func (p *Parser) parseFuncDecl() *ast.FuncDecl {
	fn := &ast.FuncDecl{Token: p.curToken}
	p.nextToken()

	if p.curTokenIs(token.LPAREN) {
		p.nextToken()
		var recvName *ast.Identifier
		var recvType ast.TypeExpr
		if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.RPAREN) {
			// Go permits an unnamed receiver in method declarations such as
			// `func (Value) String()`. The type name is sufficient for all
			// later method and dispatch analysis.
			recvName = p.parseIdentifier()
			recvType = &ast.NamedType{Token: recvName.Token, Name: recvName}
		} else {
			recvName = p.parseIdentifier()
			p.nextToken()
			recvType = p.parseTypeExpr()
		}
		p.expectPeek(token.RPAREN)
		fn.Receiver = &ast.ParamDecl{Token: recvName.Token, Name: recvName, Type: recvType}
		p.nextToken()
	}

	fn.Name = p.parseIdentifier()
	p.log(fmt.Sprintf("[%d:%d] Parsing function: %s", fn.Token.Line, fn.Token.Col, fn.Name.Value))
	p.nextToken()

	if p.curTokenIs(token.LBRACKET) {
		fn.TypeParams = p.parseTypeParams()
		p.nextToken()
	}

	fn.Params, fn.IsVariadic = p.parseParameterList(true)

	fn.ReturnTypes = []ast.TypeExpr{}
	if p.peekToken.Line == p.curToken.Line && !p.curTokenIs(token.LBRACE) && !p.peekTokenIs(token.LBRACE) && !p.peekTokenIs(token.EOF) && !p.curTokenIs(token.EOF) {
		if p.peekTokenIs(token.LPAREN) {
			fn.ReturnTypes, fn.ReturnNames = p.parseReturnTypeListWithNames()
		} else if !p.peekTokenIs(token.SEMICOLON) {
			p.nextToken()
			fn.ReturnTypes = append(fn.ReturnTypes, p.parseTypeExpr())
		}
	}

	if p.peekTokenIs(token.LBRACE) || p.curTokenIs(token.LBRACE) {
		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
		}
		startIdx := p.curIdx()
		bodyTokens, nextIdx := p.cutBraceBlock(startIdx)
		fn.BodyTokens = bodyTokens
		p.enqueue(&ParseTask{Parent: fn, Tokens: bodyTokens})
		p.jumpTo(nextIdx)
	}

	return fn
}

// -------------------------------------------------------------
// サブパーサーによる構造体フィールド・インターフェースメソッドパース
// -------------------------------------------------------------

func (p *Parser) parseStructFields(st *ast.StructType) {
	if p.curTokenIs(token.LBRACE) {
		p.nextToken()
	}
	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}
		if p.curTokenIs(token.ASTERISK) {
			p.nextToken()
			embIdent := p.parseIdentifier()
			pt := &ast.PointerType{Token: embIdent.Token, Base: &ast.NamedType{Token: embIdent.Token, Name: embIdent}}
			st.Fields = append(st.Fields, &ast.FieldDecl{Token: embIdent.Token, Name: embIdent, Type: pt, IsEmbedded: true})
		} else if p.curTokenIs(token.IDENT) {
			firstIdent := p.parseIdentifier()
			if p.peekToken.Line == p.curToken.Line && !p.peekTokenIs(token.RBRACE) && !p.peekTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.EOF) {
				p.nextToken()
				fType := p.parseTypeExpr()
				st.Fields = append(st.Fields, &ast.FieldDecl{Token: firstIdent.Token, Name: firstIdent, Type: fType, IsEmbedded: false})
			} else {
				namedType := &ast.NamedType{Token: firstIdent.Token, Name: firstIdent}
				st.Fields = append(st.Fields, &ast.FieldDecl{Token: firstIdent.Token, Name: firstIdent, Type: namedType, IsEmbedded: true})
			}
		}
		p.nextToken()
	}
}

func (p *Parser) parseInterfaceMethods(it *ast.InterfaceType) {
	if p.curTokenIs(token.LBRACE) {
		p.nextToken()
	}
	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}
		// An interface may embed another interface type, as in Go:
		// type ReadWrite interface { Reader; Writer }
		if p.curTokenIs(token.IDENT) && !p.peekTokenIs(token.LPAREN) {
			it.Embedded = append(it.Embedded, p.parseTypeExpr())
			p.nextToken()
			continue
		}
		methodName := p.parseIdentifier()
		p.expectPeek(token.LPAREN)
		paramTypes := []ast.TypeExpr{}
		if !p.peekTokenIs(token.RPAREN) {
			p.nextToken()
			for {
				// Interface method signatures may use Go's grouped parameter
				// form, for example SetControlPosition(index, depth int).
				// Interface methods only retain the parameter types, so skip
				// all names in the group before parsing the shared type.
				groupedNames := false
				groupedCount := 0
				if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.COMMA) {
					idx := p.curIdx()
					// Scan all comma-separated identifiers.  The token after the
					// last identifier is the shared type in forms such as
					// `next, breakTarget, continueTarget int`.
					candidate := idx
					nameCount := 0
					for candidate < len(p.tokens) && p.tokens[candidate].Type == token.IDENT {
						nameCount++
						candidate++
						if candidate >= len(p.tokens) || p.tokens[candidate].Type != token.COMMA ||
							candidate+1 >= len(p.tokens) || p.tokens[candidate+1].Type != token.IDENT {
							break
						}
						candidate++
					}
					if candidate < len(p.tokens) && p.tokens[candidate].Type != token.COMMA &&
						p.tokens[candidate].Type != token.RPAREN {
						groupedNames = true
						groupedCount = nameCount
					}
				}
				if groupedNames {
					for p.curTokenIs(token.IDENT) && p.peekTokenIs(token.COMMA) {
						p.nextToken()
						p.nextToken()
					}
				}
				if p.curTokenIs(token.IDENT) && !p.peekTokenIs(token.COMMA) && !p.peekTokenIs(token.RPAREN) && !p.peekTokenIs(token.DOT) {
					p.nextToken()
				}
				typeExpr := p.parseTypeExpr()
				if groupedCount > 0 {
					for i := 0; i < groupedCount; i++ {
						paramTypes = append(paramTypes, typeExpr)
					}
				} else {
					paramTypes = append(paramTypes, typeExpr)
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
		returnTypes := []ast.TypeExpr{}
		if !p.peekTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.RBRACE) && !p.peekTokenIs(token.EOF) {
			if p.peekTokenIs(token.LPAREN) {
				returnTypes = p.parseReturnTypeList()
			} else {
				p.nextToken()
				returnTypes = append(returnTypes, p.parseTypeExpr())
			}
		}
		it.Methods = append(it.Methods, &ast.MethodSig{
			Token:       methodName.Token,
			Name:        methodName,
			ParamTypes:  paramTypes,
			ReturnTypes: returnTypes,
		})
		p.nextToken()
	}
}

// -------------------------------------------------------------
// 文パース (Statement)
// -------------------------------------------------------------

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.SEMICOLON:
		return nil
	case token.VAR:
		return p.parseVarStmt()
	case token.CONST:
		// Go permits local and grouped const declarations. The current AST
		// models local bindings as variable statements, which is sufficient for
		// Go-Hike compatibility because constant folding has already happened
		// while parsing the declaration.
		decls := p.parseConstDecl()
		block := &ast.BlockStmt{Token: p.curToken, Statements: []ast.Statement{}}
		for _, decl := range decls {
			if cd, ok := decl.(*ast.ConstDecl); ok {
				block.Statements = append(block.Statements, &ast.VarDecl{Token: cd.Token, Name: cd.Name, Value: cd.Value})
			}
		}
		return block
	case token.TYPE:
		return p.parseTypeDecl()
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
	case token.LOCK:
		return p.parseLockStmt()
	case token.AREA:
		return p.parseAreaStmt()
	case token.BREAK:
		return &ast.BreakStmt{Token: p.curToken}
	case token.CONTINUE:
		return &ast.ContinueStmt{Token: p.curToken}
	case token.LBRACE:
		return p.parseBlockStmt()
	default:
		// Go-only concurrency constructs are not represented in the current
		// Hike AST. In Go-Hike compatibility sources these are used by optional
		// service code; skip their balanced block while keeping the token stream
		// aligned for the surrounding function.
		if p.curTokenIs(token.IDENT) && (p.curToken.Literal == "select" || p.curToken.Literal == "go") {
			if p.skipUnsupportedGoBlock() {
				return nil
			}
		}
		return p.parseAssignOrExprStmt()
	}
}

func (p *Parser) skipUnsupportedGoBlock() bool {
	start := p.curIdx() + 1
	for start < len(p.tokens) && p.tokens[start].Type != token.LBRACE {
		start++
	}
	if start >= len(p.tokens) {
		return false
	}
	_, next := p.cutBraceBlock(start)
	if next <= start || next > len(p.tokens) {
		return false
	}
	// A goroutine literal is commonly invoked immediately: go func() { ... }(...).
	// Consume the call suffix (including arguments) together with the skipped
	// body, since no AST node currently represents detached goroutines.
	if p.curToken.Literal == "go" && next < len(p.tokens) && p.tokens[next].Type == token.LPAREN {
		depth := 0
		for i := next; i < len(p.tokens); i++ {
			if p.tokens[i].Type == token.LPAREN {
				depth++
			} else if p.tokens[i].Type == token.RPAREN {
				depth--
				if depth == 0 {
					next = i + 1
					break
				}
			}
		}
	}
	closeIdx := next - 1
	p.curToken = p.tokens[closeIdx]
	if next < len(p.tokens) {
		p.peekToken = p.tokens[next]
		p.pos = next + 1
	} else {
		p.peekToken = token.Token{Type: token.EOF}
		p.pos = next
	}
	return true
}

func (p *Parser) parseAreaStmt() ast.Statement {
	areaTok := p.curToken
	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	var size ast.Expression
	if !p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		size = p.parseExpression(LOWEST)
	}
	if !p.expectPeek(token.RPAREN) || !p.expectPeek(token.LBRACE) {
		return nil
	}
	return &ast.AreaStmt{Token: areaTok, Size: size, Body: p.parseBlockStmt()}
}

func (p *Parser) parseLockStmt() ast.Statement {
	lockTok := p.curToken
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	return &ast.LockStmt{Token: lockTok, Body: p.parseBlockStmt()}
}

func (p *Parser) parseVarStmt() ast.Statement {
	varTok := p.curToken
	p.nextToken()

	idents := []*ast.Identifier{p.parseIdentifier()}

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		idents = append(idents, p.parseIdentifier())
	}
	p.nextToken()

	var typeExpr ast.TypeExpr = nil
	if !p.curTokenIs(token.ASSIGN) && !p.curTokenIs(token.SEMICOLON) && !p.curTokenIs(token.EOF) {
		typeExpr = p.parseTypeExpr()
		if p.peekTokenIs(token.ASSIGN) {
			p.nextToken()
		}
	}

	lefts := make([]ast.Expression, len(idents))
	for i, id := range idents {
		lefts[i] = id
	}

	if p.curTokenIs(token.ASSIGN) {
		p.nextToken()
		rights := []ast.Expression{p.parseExpression(LOWEST)}
		for p.peekTokenIs(token.COMMA) {
			p.nextToken()
			p.nextToken()
			rights = append(rights, p.parseExpression(LOWEST))
		}
		markShiftWithCarry(lefts, rights)
		return &ast.AssignStmt{
			Token: varTok,
			Left:  lefts,
			Right: rights,
			Type:  typeExpr,
		}
	}

	return &ast.AssignStmt{
		Token: varTok,
		Left:  lefts,
		Right: nil,
		Type:  typeExpr,
	}
}

func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	block := &ast.BlockStmt{Token: p.curToken, Statements: []ast.Statement{}}
	p.nextToken()
	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		for p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
		}
		if p.curTokenIs(token.RBRACE) || p.curTokenIs(token.EOF) {
			break
		}
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		// parseTypeDecl advances to the first token after a simple local type
		// declaration. Do not advance a second time or the next statement would
		// be skipped.
		if _, isTypeDecl := stmt.(*ast.TypeDecl); isTypeDecl && !p.curTokenIs(token.SEMICOLON) {
			continue
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
		p.nextToken()
		p.nextToken()
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

	if p.curTokenIs(token.RANGE) {
		// A range expression is a complete expression. Restore composite
		// literal parsing here so forms such as `range []string{"a", "b"}`
		// are not mistaken for a multi-assignment.
		p.nextToken()
		p.allowStructLit = p.curTokenIs(token.LBRACKET) || p.curTokenIs(token.MAP)
		x := p.parseExpression(LOWEST)
		p.allowStructLit = oldAllow
		if !p.expectPeek(token.LBRACE) {
			return nil
		}
		body := p.parseBlockStmt()
		return &ast.ForRangeStmt{Token: forTok, Key: nil, Value: nil, X: x, Body: body}
	}

	isThreeClause := false
	var init ast.Statement = nil

	if p.curTokenIs(token.SEMICOLON) {
		// 1. 初期化節省略: for ; cond; post
		isThreeClause = true
		init = nil
		p.nextToken() // ';' を消費して cond の先頭へ進む
	} else {
		var firstStmt ast.Statement = nil
		if p.curTokenIs(token.IDENT) && (p.peekTokenIs(token.COMMA) || p.peekTokenIs(token.DEFINE) || p.peekTokenIs(token.ASSIGN)) {
			firstIdent := p.parseIdentifier()

			if p.peekTokenIs(token.COMMA) {
				p.nextToken()
				p.nextToken()
				secondIdent := p.parseIdentifier()

				if p.peekTokenIs(token.DEFINE) || p.peekTokenIs(token.ASSIGN) {
					p.nextToken()
					assignTok := p.curToken
					if p.peekTokenIs(token.RANGE) {
						p.nextToken()
						p.nextToken()
						p.allowStructLit = p.curTokenIs(token.LBRACKET) || p.curTokenIs(token.MAP)
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
						firstStmt = &ast.AssignStmt{Token: assignTok, Left: []ast.Expression{firstIdent, secondIdent}, Right: rights}
					}
				}
			} else if p.peekTokenIs(token.DEFINE) || p.peekTokenIs(token.ASSIGN) {
				assignTok := p.peekToken
				p.nextToken()
				if p.peekTokenIs(token.RANGE) {
					p.nextToken()
					p.nextToken()
					p.allowStructLit = p.curTokenIs(token.LBRACKET) || p.curTokenIs(token.MAP)
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

		if p.peekTokenIs(token.SEMICOLON) {
			isThreeClause = true
			init = firstStmt
			p.nextToken() // ';' に進む
			p.nextToken() // cond の先頭に進む
		} else {
			// 2. while スタイル: for cond {
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
	}

	if isThreeClause {
		var cond ast.Expression = nil
		if !p.curTokenIs(token.SEMICOLON) {
			cond = p.parseExpression(LOWEST)
			if !p.expectPeek(token.SEMICOLON) {
				return nil
			}
		}
		p.nextToken()

		var post ast.Statement = nil
		if !p.curTokenIs(token.LBRACE) && !p.curTokenIs(token.EOF) {
			post = p.parseAssignOrExprStmt()
		}
		p.allowStructLit = oldAllow
		if !p.curTokenIs(token.LBRACE) && p.peekTokenIs(token.LBRACE) {
			p.nextToken()
		}
		body := p.parseBlockStmt()
		return &ast.ForStmt{Token: forTok, Init: init, Cond: cond, Post: post, Body: body}
	}

	return nil
}

func (p *Parser) parseSwitchStmt() ast.Statement {
	switchTok := p.curToken
	p.nextToken()

	var initStmt ast.Statement = nil
	var condExpr ast.Expression = nil
	var isTypeSwitch bool
	var typeSwitchVar *ast.Identifier = nil
	var typeSwitchExpr ast.Expression = nil

	if p.curTokenIs(token.LBRACE) {
		condExpr = &ast.Identifier{
			Token: token.Token{Type: token.IDENT, Literal: "true", Line: switchTok.Line, Col: switchTok.Col},
			Value: "true",
		}
	} else {
		oldAllow := p.allowStructLit
		p.allowStructLit = false

		firstStmt := p.parseAssignOrExprStmt()
		switchGuard := firstStmt

		if p.peekTokenIs(token.SEMICOLON) {
			initStmt = firstStmt
			p.nextToken()
			if p.peekTokenIs(token.LBRACE) {
				condExpr = &ast.Identifier{
					Token: token.Token{Type: token.IDENT, Literal: "true", Line: switchTok.Line, Col: switchTok.Col},
					Value: "true",
				}
				switchGuard = nil
			} else {
				p.nextToken()
				switchGuard = p.parseAssignOrExprStmt()
			}
		}
		p.allowStructLit = oldAllow

		if switchGuard != nil {
			if assignStmt, ok := switchGuard.(*ast.AssignStmt); ok {
				if len(assignStmt.Left) == 1 && len(assignStmt.Right) == 1 {
					if typeAssert, ok := assignStmt.Right[0].(*ast.TypeAssertExpr); ok && typeAssert.Target == nil {
						if ident, ok := assignStmt.Left[0].(*ast.Identifier); ok {
							isTypeSwitch = true
							typeSwitchVar = ident
							typeSwitchExpr = typeAssert.Expr
						}
					}
				}
			} else if exprStmt, ok := switchGuard.(*ast.ExprStmt); ok {
				if typeAssert, ok := exprStmt.Expr.(*ast.TypeAssertExpr); ok && typeAssert.Target == nil {
					isTypeSwitch = true
					typeSwitchExpr = typeAssert.Expr
				} else {
					condExpr = exprStmt.Expr
				}
			}
		}
	}

	if isTypeSwitch {
		stmt := &ast.TypeSwitchStmt{
			Token:    switchTok,
			Init:     initStmt,
			Variable: typeSwitchVar,
			Expr:     typeSwitchExpr,
			Cases:    []*ast.TypeCaseClause{},
		}

		if !p.curTokenIs(token.LBRACE) && !p.expectPeek(token.LBRACE) {
			return nil
		}

		p.nextToken()
		for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
			if p.curTokenIs(token.SEMICOLON) {
				p.nextToken()
				continue
			}
			if p.curTokenIs(token.CASE) || p.curTokenIs(token.DEFAULT) {
				c := &ast.TypeCaseClause{Token: p.curToken, Types: []ast.TypeExpr{}, Body: []ast.Statement{}}
				if p.curTokenIs(token.CASE) {
					p.nextToken()
					for {
						if p.curTokenIs(token.NIL) {
							c.IsNil = true
						} else {
							c.Types = append(c.Types, p.parseTypeExpr())
						}
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
					if p.curTokenIs(token.SEMICOLON) {
						p.nextToken()
						continue
					}
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

	stmt := &ast.SwitchStmt{
		Token: switchTok,
		Init:  initStmt,
		Value: condExpr,
		Cases: []*ast.CaseClause{},
	}

	if !p.curTokenIs(token.LBRACE) && !p.expectPeek(token.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}
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
				if p.curTokenIs(token.SEMICOLON) {
					p.nextToken()
					continue
				}
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
	p.nextToken()
	exp := p.parseExpression(LOWEST)
	if call, ok := exp.(*ast.CallExpr); ok {
		stmt.Call = call
	}
	p.log(fmt.Sprintf("[%d:%d] Parsed defer statement", stmt.Token.Line, stmt.Token.Col))
	return stmt
}

func (p *Parser) parseAssignOrExprStmt() ast.Statement {
	if p.curTokenIs(token.SEMICOLON) {
		return nil
	}
	// Go labels are control-flow metadata. The current Hike AST does not
	// expose labels, but consuming the label keeps the following statement
	// aligned and lets structured lowering handle the loop normally.
	if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.COLON) {
		p.nextToken()
		return nil
	}

	startTok := p.curToken
	leftExpr := p.parseExpression(LOWEST)
	if leftExpr == nil {
		return nil
	}

	if p.peekTokenIs(token.ARROW) {
		p.nextToken()
		arrowTok := p.curToken
		p.nextToken()
		val := p.parseExpression(LOWEST)
		return &ast.SendStmt{
			Token: arrowTok,
			Chan:  leftExpr,
			Value: val,
		}
	}

	if p.peekTokenIs(token.INC) || p.peekTokenIs(token.DEC) {
		p.nextToken()
		opTok := p.curToken
		return &ast.AssignStmt{
			Token: opTok,
			Left:  []ast.Expression{leftExpr},
			Right: []ast.Expression{&ast.IntegerLiteral{Token: opTok, Value: 1}},
		}
	}

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
			markShiftWithCarry(lefts, rights)
			return &ast.AssignStmt{Token: assignTok, Left: lefts, Right: rights}
		}
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] syntax error: unexpected comma in statement, expected assignment", p.curToken.Line, p.curToken.Col))
		return nil
	}

	return &ast.ExprStmt{Token: startTok, Expr: leftExpr}
}

// markShiftWithCarry enables the optional second result when a shift is
// unpacked into two assignment targets, for example: value, carry := x << 1.
func markShiftWithCarry(lefts []ast.Expression, rights []ast.Expression) {
	if len(lefts) == 2 && len(rights) == 1 {
		if shift, ok := rights[0].(*ast.BinaryExpr); ok && (shift.Operator == "<<" || shift.Operator == ">>") {
			shift.WithCarry = true
		}
	}
}
