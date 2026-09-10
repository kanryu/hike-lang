package lexer

import (
	"fmt"
	"strings"

	"hikec-go/pkg/token"
)

type Lexer struct {
	input        string
	position     int
	readPosition int
	ch           byte
	line         int
	col          int
	verbose      bool
	prevToken    token.TokenType // Go仕様のセミコロン自動挿入(ASI)判定用
}

func New(input string) *Lexer {
	l := &Lexer{
		input:     input,
		line:      1,
		col:       0,
		verbose:   false,
		prevToken: token.ILLEGAL,
	}
	l.readChar()
	return l
}

func (l *Lexer) SetVerbose(v bool) {
	l.verbose = v
}

func (l *Lexer) log(tok token.Token) {
	if l.verbose {
		fmt.Printf("[LEXER] [Line %d:%d] Token: %-10s Literal: %q\n", tok.Line, tok.Col, tok.Type, tok.Literal)
	}
}

func (l *Lexer) emitToken(tok token.Token) token.Token {
	l.prevToken = tok.Type
	l.log(tok)
	return tok
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++
	l.col++
}

func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

func (l *Lexer) peekAhead(offset int) byte {
	pos := l.position + offset
	if pos >= len(l.input) {
		return 0
	}
	return l.input[pos]
}

func canInsertSemi(tokType token.TokenType) bool {
	switch tokType {
	case token.IDENT, token.INT, token.FLOAT, token.CHAR, token.STRING:
		return true
	case token.BREAK, token.CONTINUE, token.RETURN:
		return true
	case token.INC, token.DEC, token.RPAREN, token.RBRACKET, token.RBRACE:
		return true
	default:
		return false
	}
}

func (l *Lexer) skipSingleLineComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

func (l *Lexer) skipMultiLineComment() bool {
	hasNewline := false
	l.readChar() // '/'
	l.readChar() // '*'
	for {
		if l.ch == 0 {
			break
		}
		if l.ch == '\n' {
			hasNewline = true
			l.line++
			l.col = 0
		}
		if l.ch == '*' && l.peekChar() == '/' {
			l.readChar()
			l.readChar()
			break
		}
		l.readChar()
	}
	return hasNewline
}

func newToken(tokenType token.TokenType, ch byte, line, col int) token.Token {
	return token.Token{Type: tokenType, Literal: string(ch), Line: line, Col: col}
}

func (l *Lexer) NextToken() token.Token {
	var tok token.Token

	for {
		for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
			l.readChar()
		}

		if l.ch == '/' && l.peekChar() == '/' {
			l.skipSingleLineComment()
		}

		if l.ch == '/' && l.peekChar() == '*' {
			startLine := l.line
			startCol := l.col
			hasNewline := l.skipMultiLineComment()
			if hasNewline && canInsertSemi(l.prevToken) {
				tok = token.Token{Type: token.SEMICOLON, Literal: ";", Line: startLine, Col: startCol}
				return l.emitToken(tok)
			}
			continue
		}

		if l.ch == '\n' {
			startLine := l.line
			startCol := l.col
			l.line++
			l.col = 0
			l.readChar()

			if canInsertSemi(l.prevToken) {
				tok = token.Token{Type: token.SEMICOLON, Literal: ";", Line: startLine, Col: startCol}
				return l.emitToken(tok)
			}
			continue
		}

		break
	}

	startCol := l.col
	startLine := l.line

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.EQ, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.ASSIGN, l.ch, startLine, startCol)
		}
	case '+':
		if l.peekChar() == '+' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.INC, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.PLUS_ASSIGN, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.PLUS, l.ch, startLine, startCol)
		}
	case '-':
		if l.peekChar() == '-' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.DEC, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.MINUS_ASSIGN, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.MINUS, l.ch, startLine, startCol)
		}
	case '*':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.ASTERISK_ASSIGN, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.ASTERISK, l.ch, startLine, startCol)
		}
	case '/':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.SLASH_ASSIGN, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.SLASH, l.ch, startLine, startCol)
		}
	case '%':
		tok = newToken(token.PERCENT, l.ch, startLine, startCol)
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.NEQ, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.BANG, l.ch, startLine, startCol)
		}
	case '<':
		if l.peekChar() == '<' {
			l.readChar()
			if l.peekChar() == '=' {
				l.readChar()
				tok = token.Token{Type: token.SHL_ASSIGN, Literal: "<<=", Line: startLine, Col: startCol}
			} else {
				tok = token.Token{Type: token.SHL, Literal: "<<", Line: startLine, Col: startCol}
			}
		} else if l.peekChar() == '-' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.ARROW, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LE, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.LT, l.ch, startLine, startCol)
		}
	case '>':
		if l.peekChar() == '>' {
			l.readChar()
			if l.peekChar() == '=' {
				l.readChar()
				tok = token.Token{Type: token.SHR_ASSIGN, Literal: ">>=", Line: startLine, Col: startCol}
			} else {
				tok = token.Token{Type: token.SHR, Literal: ">>", Line: startLine, Col: startCol}
			}
		} else if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.GE, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.GT, l.ch, startLine, startCol)
		}
	case '&':
		if l.peekChar() == '&' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LAND, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.AND_ASSIGN, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.AMPERSAND, l.ch, startLine, startCol)
		}
	case '|':
		if l.peekChar() == '|' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LOR, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.OR_ASSIGN, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.OR, l.ch, startLine, startCol)
		}
	case '^':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.XOR_ASSIGN, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.CARET, l.ch, startLine, startCol)
		}
	case ':':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.DEFINE, Literal: string(ch) + string(l.ch), Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.COLON, l.ch, startLine, startCol)
		}
	case '.':
		if l.peekChar() == '.' && l.peekAhead(2) == '.' {
			l.readChar()
			l.readChar()
			tok = token.Token{Type: token.ELLIPSIS, Literal: "...", Line: startLine, Col: startCol}
		} else {
			tok = newToken(token.DOT, l.ch, startLine, startCol)
		}
	case ';':
		tok = newToken(token.SEMICOLON, l.ch, startLine, startCol)
	case ',':
		tok = newToken(token.COMMA, l.ch, startLine, startCol)
	case '(':
		tok = newToken(token.LPAREN, l.ch, startLine, startCol)
	case ')':
		tok = newToken(token.RPAREN, l.ch, startLine, startCol)
	case '{':
		tok = newToken(token.LBRACE, l.ch, startLine, startCol)
	case '}':
		tok = newToken(token.RBRACE, l.ch, startLine, startCol)
	case '[':
		tok = newToken(token.LBRACKET, l.ch, startLine, startCol)
	case ']':
		tok = newToken(token.RBRACKET, l.ch, startLine, startCol)
	case '"':
		tok.Type = token.STRING
		tok.Literal = l.readString()
		tok.Line = startLine
		tok.Col = startCol
		return l.emitToken(tok)
	case '\'':
		tok.Type = token.CHAR
		tok.Literal = l.readCharLiteral()
		tok.Line = startLine
		tok.Col = startCol
		return l.emitToken(tok)
	case '`':
		tok.Type = token.STRING
		tok.Literal = l.readRawString()
		tok.Line = startLine
		tok.Col = startCol
		return l.emitToken(tok)
	case 0:
		if canInsertSemi(l.prevToken) {
			tok = token.Token{Type: token.SEMICOLON, Literal: ";", Line: startLine, Col: startCol}
			return l.emitToken(tok)
		}
		tok.Literal = ""
		tok.Type = token.EOF
		tok.Line = startLine
		tok.Col = startCol
		return l.emitToken(tok)
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			tok.Line = startLine
			tok.Col = startCol
			return l.emitToken(tok)
		} else if isDigit(l.ch) {
			lit, tokType := l.readNumber()
			tok.Literal = lit
			tok.Type = tokType
			tok.Line = startLine
			tok.Col = startCol
			return l.emitToken(tok)
		} else {
			tok = newToken(token.ILLEGAL, l.ch, startLine, startCol)
		}
	}

	l.readChar()
	return l.emitToken(tok)
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}

func isHexDigit(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func (l *Lexer) readNumber() (string, token.TokenType) {
	position := l.position

	// 1. 16進数リテラル (0x / 0X)
	if l.ch == '0' && (l.peekChar() == 'x' || l.peekChar() == 'X') {
		l.readChar() // '0'
		l.readChar() // 'x' or 'X'
		for isHexDigit(l.ch) || l.ch == '_' {
			l.readChar()
		}
		return l.input[position:l.position], token.INT
	}

	// 2. 2進数リテラル (0b / 0B)
	if l.ch == '0' && (l.peekChar() == 'b' || l.peekChar() == 'B') {
		l.readChar() // '0'
		l.readChar() // 'b' or 'B'
		for l.ch == '0' || l.ch == '1' || l.ch == '_' {
			l.readChar()
		}
		return l.input[position:l.position], token.INT
	}

	// 3. 8進数リテラル (0o / 0O)
	if l.ch == '0' && (l.peekChar() == 'o' || l.peekChar() == 'O') {
		l.readChar() // '0'
		l.readChar() // 'o' or 'O'
		for (l.ch >= '0' && l.ch <= '7') || l.ch == '_' {
			l.readChar()
		}
		return l.input[position:l.position], token.INT
	}

	// 4. 10進数および浮動小数点数リテラル
	isFloat := false
	for isDigit(l.ch) || l.ch == '.' || l.ch == '_' {
		if l.ch == '.' {
			if isFloat {
				break
			}
			if isDigit(l.peekChar()) {
				isFloat = true
			} else {
				break
			}
		}
		l.readChar()
	}
	literal := l.input[position:l.position]
	if isFloat {
		return literal, token.FLOAT
	}
	return literal, token.INT
}

func (l *Lexer) readString() string {
	var sb strings.Builder
	for {
		l.readChar()
		if l.ch == '"' || l.ch == 0 {
			break
		}
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case '0':
				sb.WriteByte(0)
			default:
				sb.WriteByte('\\')
				sb.WriteByte(l.ch)
			}
		} else {
			sb.WriteByte(l.ch)
		}
	}
	str := sb.String()
	l.readChar()
	return str
}

func (l *Lexer) readCharLiteral() string {
	var sb strings.Builder
	for {
		l.readChar()
		if l.ch == '\'' || l.ch == '\n' || l.ch == 0 {
			break
		}
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case '\'':
				sb.WriteByte('\'')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case '0':
				sb.WriteByte(0)
			default:
				sb.WriteByte('\\')
				sb.WriteByte(l.ch)
			}
		} else {
			sb.WriteByte(l.ch)
		}
	}
	str := sb.String()
	if l.ch == '\'' {
		l.readChar()
	}
	return str
}

func (l *Lexer) readRawString() string {
	var sb strings.Builder
	for {
		l.readChar()
		if l.ch == '`' || l.ch == 0 {
			break
		}
		if l.ch == '\n' {
			l.line++
			l.col = 0
		}
		sb.WriteByte(l.ch)
	}
	str := sb.String()
	l.readChar()
	return str
}

func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}
