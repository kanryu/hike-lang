package parser

import (
	"fmt"
	"strconv"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/token"
)

// -----------------------------------------------------------------------------
// 演算子優先順位テーブル
// -----------------------------------------------------------------------------

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
	token.PERCENT:   PRODUCT,
	token.AMPERSAND: PRODUCT,
	token.SHL:       PRODUCT,
	token.SHR:       PRODUCT,
	token.LPAREN:    CALL,
	token.LBRACKET:  INDEX,
	token.DOT:       INDEX,
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

// -----------------------------------------------------------------------------
// 型式パース (TypeExpr)
// -----------------------------------------------------------------------------

func (p *Parser) parseTypeExpr() ast.TypeExpr {
	if p.curTokenIs(token.ELLIPSIS) {
		tok := p.curToken
		p.nextToken()
		elem := p.parseTypeExpr()
		return &ast.EllipsisType{Token: tok, Elem: elem}
	} else if p.curTokenIs(token.INTERFACE) || (p.curTokenIs(token.IDENT) && p.curToken.Literal == "interface" && p.peekTokenIs(token.LBRACE)) {
		tok := p.curToken
		if p.peekTokenIs(token.LBRACE) {
			p.nextToken()
		}
		it := &ast.InterfaceType{Token: tok, Methods: []*ast.MethodSig{}}
		for !p.peekTokenIs(token.RBRACE) && !p.peekTokenIs(token.EOF) {
			p.nextToken()
			if p.curTokenIs(token.SEMICOLON) {
				continue
			}
			methodName := p.parseIdentifier()
			p.expectPeek(token.LPAREN)
			paramTypes := []ast.TypeExpr{}
			isVariadic := false
			if !p.peekTokenIs(token.RPAREN) {
				p.nextToken()
				for {
					if p.curTokenIs(token.ELLIPSIS) {
						isVariadic = true
						p.nextToken()
						elem := p.parseTypeExpr()
						paramTypes = append(paramTypes, &ast.EllipsisType{Token: p.curToken, Elem: elem})
						break
					}
					if p.curTokenIs(token.IDENT) && !p.peekTokenIs(token.COMMA) && !p.peekTokenIs(token.RPAREN) && !p.peekTokenIs(token.DOT) {
						p.nextToken()
					}
					if p.curTokenIs(token.ELLIPSIS) {
						isVariadic = true
						p.nextToken()
						elem := p.parseTypeExpr()
						paramTypes = append(paramTypes, &ast.EllipsisType{Token: p.curToken, Elem: elem})
						break
					}
					paramTypes = append(paramTypes, p.parseTypeExpr())
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
					p.nextToken()
					p.nextToken()
					for {
						returnTypes = append(returnTypes, p.parseTypeExpr())
						if p.peekTokenIs(token.COMMA) {
							p.nextToken()
							p.nextToken()
						} else {
							break
						}
					}
					p.expectPeek(token.RPAREN)
				} else {
					p.nextToken()
					returnTypes = append(returnTypes, p.parseTypeExpr())
				}
			}
			it.Methods = append(it.Methods, &ast.MethodSig{
				Token:       methodName.Token,
				Name:        methodName,
				ParamTypes:  paramTypes,
				IsVariadic:  isVariadic,
				ReturnTypes: returnTypes,
			})
		}
		p.expectPeek(token.RBRACE)
		return it
	} else if p.curTokenIs(token.IDENT) {
		ident := p.parseIdentifier()

		var pkgIdent *ast.Identifier = nil
		targetIdent := ident

		if p.peekTokenIs(token.DOT) {
			p.nextToken()
			p.nextToken()
			field := p.parseIdentifier()
			pkgIdent = ident
			targetIdent = field
		}

		typeArgs := []ast.TypeExpr{}
		if p.peekTokenIs(token.LBRACKET) {
			p.nextToken()
			p.nextToken()
			for !p.curTokenIs(token.RBRACKET) && !p.curTokenIs(token.EOF) {
				typeArgs = append(typeArgs, p.parseTypeExpr())
				if p.peekTokenIs(token.COMMA) {
					p.nextToken()
					p.nextToken()
				} else {
					break
				}
			}
			p.expectPeek(token.RBRACKET)
		}

		return &ast.NamedType{
			Token:    targetIdent.Token,
			Package:  pkgIdent,
			Name:     targetIdent,
			TypeArgs: typeArgs,
		}
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
	} else if p.curTokenIs(token.FUNC) {
		tok := p.curToken
		if !p.expectPeek(token.LPAREN) {
			return nil
		}

		paramTypes := []ast.TypeExpr{}
		isVariadic := false
		if !p.peekTokenIs(token.RPAREN) {
			p.nextToken()
			for {
				if p.curTokenIs(token.ELLIPSIS) {
					isVariadic = true
					p.nextToken()
					elem := p.parseTypeExpr()
					paramTypes = append(paramTypes, &ast.EllipsisType{Token: p.curToken, Elem: elem})
					break
				}

				firstType := p.parseTypeExpr()
				if p.peekTokenIs(token.IDENT) || p.peekTokenIs(token.ASTERISK) || p.peekTokenIs(token.LBRACKET) || p.peekTokenIs(token.MAP) || p.peekTokenIs(token.FUNC) {
					p.nextToken()
					actualType := p.parseTypeExpr()
					paramTypes = append(paramTypes, actualType)
				} else {
					paramTypes = append(paramTypes, firstType)
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
		if !p.peekTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.RBRACE) && !p.peekTokenIs(token.COMMA) && !p.peekTokenIs(token.RPAREN) && !p.peekTokenIs(token.ASSIGN) && !p.peekTokenIs(token.EOF) {
			if p.peekTokenIs(token.LPAREN) {
				p.nextToken()
				p.nextToken()
				for {
					returnTypes = append(returnTypes, p.parseTypeExpr())
					if p.peekTokenIs(token.COMMA) {
						p.nextToken()
						p.nextToken()
					} else {
						break
					}
				}
				p.expectPeek(token.RPAREN)
			} else {
				p.nextToken()
				returnTypes = append(returnTypes, p.parseTypeExpr())
			}
		}
		return &ast.FuncType{Token: tok, ParamTypes: paramTypes, IsVariadic: isVariadic, ReturnTypes: returnTypes}
	} else if p.curTokenIs(token.MAP) {
		tok := p.curToken
		p.nextToken()
		p.expectCurrent(token.LBRACKET)
		p.nextToken()
		keyType := p.parseTypeExpr()
		p.expectPeek(token.RBRACKET)
		p.nextToken()
		valType := p.parseTypeExpr()
		return &ast.MapType{Token: tok, Key: keyType, Value: valType}
	} else if p.curTokenIs(token.CHAN) {
		tok := p.curToken
		p.nextToken()
		elem := p.parseTypeExpr()
		return &ast.ChanType{Token: tok, Elem: elem}
	}
	return &ast.NamedType{Token: p.curToken, Package: nil, Name: &ast.Identifier{Token: p.curToken, Value: "int"}}
}

// -----------------------------------------------------------------------------
// 式パース (Expression / Pratt Parsing)
// -----------------------------------------------------------------------------

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
	case token.FLOAT:
		leftExp = p.parseFloatLiteral()
	case token.STRING:
		leftExp = p.parseStringLiteral()
	case token.NIL:
		leftExp = &ast.NilLiteral{Token: p.curToken}
	case token.BANG, token.MINUS, token.ASTERISK, token.AMPERSAND, token.CARET:
		leftExp = p.parsePrefixExpr()

	case token.ARROW:
		tok := p.curToken
		p.nextToken()
		right := p.parseExpression(PREFIX)
		leftExp = &ast.ReceiveExpr{Token: tok, Expr: right}

	case token.ASYNC:
		tok := p.curToken
		if !p.expectPeek(token.LPAREN) {
			return nil
		}
		p.nextToken()
		fn := p.parseExpression(LOWEST)
		if !p.expectPeek(token.RPAREN) {
			return nil
		}
		leftExp = &ast.AsyncExpr{Token: tok, Fn: fn}

	case token.FUNC:
		tok := p.curToken
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
					if p.peekTokenIs(token.RPAREN) {
						break
					}
					p.nextToken()
				} else {
					pName := p.parseIdentifier()
					p.nextToken()
					paramIsVariadic := false
					if p.curTokenIs(token.ELLIPSIS) {
						paramIsVariadic = true
						isVariadic = true
						p.nextToken()
						elemType := p.parseTypeExpr()
						pType := &ast.EllipsisType{Token: p.curToken, Elem: elemType}
						params = append(params, &ast.ParamDecl{Token: pName.Token, Name: pName, Type: pType, IsVariadic: paramIsVariadic})
					} else {
						pType := p.parseTypeExpr()
						params = append(params, &ast.ParamDecl{Token: pName.Token, Name: pName, Type: pType, IsVariadic: false})
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
		}
		p.expectPeek(token.RPAREN)

		returnTypes := []ast.TypeExpr{}
		if !p.peekTokenIs(token.LBRACE) && !p.peekTokenIs(token.SEMICOLON) && !p.peekTokenIs(token.EOF) {
			if p.peekTokenIs(token.LPAREN) {
				p.nextToken()
				p.nextToken()
				for {
					returnTypes = append(returnTypes, p.parseTypeExpr())
					if p.peekTokenIs(token.COMMA) {
						p.nextToken()
						p.nextToken()
					} else {
						break
					}
				}
				p.expectPeek(token.RPAREN)
			} else {
				p.nextToken()
				returnTypes = append(returnTypes, p.parseTypeExpr())
			}
		}

		if !p.expectPeek(token.LBRACE) {
			return nil
		}

		// クロージャ本体を直接ブロックパースして構文木を完成
		body := p.parseBlockStmt()
		leftExp = &ast.FuncLit{Token: tok, Params: params, IsVariadic: isVariadic, ReturnTypes: returnTypes, Body: body}

	case token.MAP:
		if expr, ok := p.parseTypeExpr().(ast.Expression); ok {
			leftExp = expr
		}

	case token.CHAN:
		if expr, ok := p.parseTypeExpr().(ast.Expression); ok {
			leftExp = expr
		}

	case token.ELLIPSIS:
		leftExp = &ast.Identifier{Token: p.curToken, Value: "..."}

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

	case token.DOT:
		p.nextToken()
		if p.peekTokenIs(token.LPAREN) {
			p.nextToken()
			if p.peekTokenIs(token.TYPE) {
				p.nextToken()
				p.expectPeek(token.RPAREN)
				return &ast.TypeAssertExpr{Token: p.curToken, Expr: leftExp, Target: nil}
			}
			p.nextToken()
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
		case token.PLUS, token.MINUS, token.SLASH, token.ASTERISK, token.PERCENT,
			token.EQ, token.NEQ, token.LT, token.GT, token.LE, token.GE,
			token.LAND, token.LOR, token.OR, token.CARET, token.AMPERSAND,
			token.SHL, token.SHR:
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

func (p *Parser) parseFloatLiteral() *ast.FloatLiteral {
	val, err := strconv.ParseFloat(p.curToken.Literal, 64)
	if err != nil {
		p.errors = append(p.errors, fmt.Sprintf("could not parse %q as float", p.curToken.Literal))
		return nil
	}
	return &ast.FloatLiteral{Token: p.curToken, Value: val}
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
			if p.curTokenIs(token.ELLIPSIS) {
				args = append(args, &ast.Identifier{Token: p.curToken, Value: "..."})
				hasEllipsis = true
				if p.peekTokenIs(token.COMMA) {
					p.nextToken()
				}
				if p.peekTokenIs(token.RPAREN) {
					break
				}
				p.nextToken()
				continue
			}

			arg := p.parseExpression(LOWEST)
			if p.peekTokenIs(token.ELLIPSIS) {
				p.nextToken()
				hasEllipsis = true
				args = append(args, arg)
				if p.peekTokenIs(token.COMMA) {
					p.nextToken()
				}
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

func (p *Parser) parseGenericStructLiteral(genExpr *ast.GenericInstExpr) ast.Expression {
	var namedType *ast.NamedType

	if ident, ok := genExpr.Left.(*ast.Identifier); ok {
		namedType = &ast.NamedType{
			Token:    ident.Token,
			Name:     ident,
			TypeArgs: genExpr.TypeArgs,
		}
	} else if mem, ok := genExpr.Left.(*ast.MemberExpr); ok {
		if pkgIdent, okPkg := mem.Object.(*ast.Identifier); okPkg {
			namedType = &ast.NamedType{
				Token:    pkgIdent.Token,
				Package:  pkgIdent,
				Name:     mem.Field,
				TypeArgs: genExpr.TypeArgs,
			}
		}
	}

	if namedType == nil {
		return genExpr
	}

	p.nextToken()
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
	return &ast.StructLiteral{Token: namedType.Token, Type: namedType, Fields: fields}
}

func exprToTypeExpr(e ast.Expression) ast.TypeExpr {
	if e == nil {
		return nil
	}
	if te, ok := e.(ast.TypeExpr); ok {
		return te
	}
	if id, ok := e.(*ast.Identifier); ok {
		return &ast.NamedType{Token: id.Token, Name: id}
	}
	if pref, ok := e.(*ast.PrefixExpr); ok && pref.Operator == "*" {
		base := exprToTypeExpr(pref.Right)
		if base != nil {
			return &ast.PointerType{Token: pref.Token, Base: base}
		}
	}
	if mem, ok := e.(*ast.MemberExpr); ok {
		if pkgId, okPkg := mem.Object.(*ast.Identifier); okPkg {
			return &ast.NamedType{
				Token:   pkgId.Token,
				Package: pkgId,
				Name:    mem.Field,
			}
		}
	}
	if fl, ok := e.(*ast.FuncLit); ok {
		pts := make([]ast.TypeExpr, len(fl.Params))
		for i, p := range fl.Params {
			pts[i] = p.Type
		}
		return &ast.FuncType{
			Token:       fl.Token,
			ParamTypes:  pts,
			ReturnTypes: fl.ReturnTypes,
		}
	}
	return nil
}

func (p *Parser) parseIndexExpr(left ast.Expression) ast.Expression {
	tok := p.curToken
	p.nextToken()

	if p.curTokenIs(token.COLON) {
		p.nextToken()
		var high ast.Expression = nil
		if !p.curTokenIs(token.RBRACKET) {
			high = p.parseExpression(LOWEST)
			p.nextToken()
		}
		p.expectCurrent(token.RBRACKET)
		return &ast.SliceExpr{Token: tok, Left: left, Low: nil, High: high}
	}

	indexOrLow := p.parseExpression(LOWEST)

	if p.peekTokenIs(token.COMMA) {
		args := []ast.TypeExpr{}
		if tArg := exprToTypeExpr(indexOrLow); tArg != nil {
			args = append(args, tArg)
		}
		for p.peekTokenIs(token.COMMA) {
			p.nextToken()
			p.nextToken()
			args = append(args, p.parseTypeExpr())
		}
		p.expectPeek(token.RBRACKET)
		genExpr := &ast.GenericInstExpr{Token: tok, Left: left, TypeArgs: args}

		if p.allowStructLit && p.peekTokenIs(token.LBRACE) {
			return p.parseGenericStructLiteral(genExpr)
		}
		return genExpr
	}

	if p.peekTokenIs(token.COLON) {
		p.nextToken()
		p.nextToken()
		var high ast.Expression = nil
		if !p.curTokenIs(token.RBRACKET) {
			high = p.parseExpression(LOWEST)
			p.nextToken()
		}
		p.expectCurrent(token.RBRACKET)
		return &ast.SliceExpr{Token: tok, Left: left, Low: indexOrLow, High: high}
	}

	p.expectPeek(token.RBRACKET)

	if p.allowStructLit && p.peekTokenIs(token.LBRACE) {
		if typeArg := exprToTypeExpr(indexOrLow); typeArg != nil {
			genExpr := &ast.GenericInstExpr{Token: tok, Left: left, TypeArgs: []ast.TypeExpr{typeArg}}
			return p.parseGenericStructLiteral(genExpr)
		}
	}

	return &ast.IndexExpr{Token: tok, Left: left, Index: indexOrLow}
}

func (p *Parser) parseMemberExpr(obj ast.Expression) ast.Expression {
	tok := p.curToken
	if p.peekTokenIs(token.LPAREN) {
		p.nextToken()
		if p.peekTokenIs(token.TYPE) {
			p.nextToken()
			p.expectPeek(token.RPAREN)
			return &ast.TypeAssertExpr{Token: tok, Expr: obj, Target: nil}
		}
		p.nextToken()
		targetType := p.parseTypeExpr()
		p.expectPeek(token.RPAREN)
		return &ast.TypeAssertExpr{Token: tok, Expr: obj, Target: targetType}
	}
	p.nextToken()
	field := p.parseIdentifier()

	if p.allowStructLit && p.peekTokenIs(token.LBRACE) {
		if pkgIdent, ok := obj.(*ast.Identifier); ok {
			p.nextToken()
			namedType := &ast.NamedType{
				Token:   pkgIdent.Token,
				Package: pkgIdent,
				Name:    field,
			}
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
			return &ast.StructLiteral{Token: pkgIdent.Token, Type: namedType, Fields: fields}
		}
	}

	return &ast.MemberExpr{Token: tok, Object: obj, Field: field}
}
