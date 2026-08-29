package lexer

import (
	"fmt"
	"hikec-go/pkg/token"
)

type Lexer struct {
	input        string
	position     int  // 現在走査中の文字位置
	readPosition int  // 次に読み込む文字位置
	ch           byte // 現在走査中の文字
	line         int
	col          int
	verbose      bool
}

func New(input string) *Lexer {
	l := &Lexer{
		input:   input,
		line:    1,
		col:     0,
		verbose: false,
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

func (l *Lexer) skipWhitespace() {
	for {
		if l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
			if l.ch == '\n' {
				l.line++
				l.col = 0
			}
			l.readChar()
		} else if l.ch == '/' && l.peekChar() == '/' {
			// 1行コメント (// ...) のスキップ
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
		} else if l.ch == '/' && l.peekChar() == '*' {
			// 複数行コメント (/* ... */) のスキップ
			l.readChar()
			l.readChar()
			for !(l.ch == '*' && l.peekChar() == '/') && l.ch != 0 {
				if l.ch == '\n' {
					l.line++
					l.col = 0
				}
				l.readChar()
			}
			if l.ch == '*' && l.peekChar() == '/' {
				l.readChar()
				l.readChar()
			}
		} else {
			break
		}
	}
}

func (l *Lexer) NextToken() token.Token {
	l.skipWhitespace()

	var tok token.Token
	curLine := l.line
	curCol := l.col

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.EQ, Literal: string(ch) + string(l.ch), Line: curLine, Col: curCol}
		} else {
			tok = token.Token{Type: token.ASSIGN, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	case ':':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.DEFINE, Literal: string(ch) + string(l.ch), Line: curLine, Col: curCol}
		} else {
			tok = token.Token{Type: token.COLON, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	case '+':
		tok = token.Token{Type: token.PLUS, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '-':
		tok = token.Token{Type: token.MINUS, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '*':
		tok = token.Token{Type: token.ASTERISK, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '/':
		tok = token.Token{Type: token.SLASH, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.NEQ, Literal: string(ch) + string(l.ch), Line: curLine, Col: curCol}
		} else {
			tok = token.Token{Type: token.BANG, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	case '<':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LE, Literal: string(ch) + string(l.ch), Line: curLine, Col: curCol}
		} else {
			tok = token.Token{Type: token.LT, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	case '>':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.GE, Literal: string(ch) + string(l.ch), Line: curLine, Col: curCol}
		} else {
			tok = token.Token{Type: token.GT, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	case '&':
		if l.peekChar() == '&' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LAND, Literal: string(ch) + string(l.ch), Line: curLine, Col: curCol}
		} else {
			tok = token.Token{Type: token.AMPERSAND, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	case '|':
		if l.peekChar() == '|' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LOR, Literal: string(ch) + string(l.ch), Line: curLine, Col: curCol}
		} else {
			tok = token.Token{Type: token.ILLEGAL, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	case ',':
		tok = token.Token{Type: token.COMMA, Literal: string(l.ch), Line: curLine, Col: curCol}
	case ';':
		tok = token.Token{Type: token.SEMICOLON, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '.':
		if l.peekChar() == '.' {
			l.readChar()
			if l.peekChar() == '.' {
				l.readChar()
				tok = token.Token{Type: token.ELLIPSIS, Literal: "...", Line: curLine, Col: curCol}
			} else {
				tok = token.Token{Type: token.ILLEGAL, Literal: "..", Line: curLine, Col: curCol}
			}
		} else {
			tok = token.Token{Type: token.DOT, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	case '(':
		tok = token.Token{Type: token.LPAREN, Literal: string(l.ch), Line: curLine, Col: curCol}
	case ')':
		tok = token.Token{Type: token.RPAREN, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '{':
		tok = token.Token{Type: token.LBRACE, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '}':
		tok = token.Token{Type: token.RBRACE, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '[':
		tok = token.Token{Type: token.LBRACKET, Literal: string(l.ch), Line: curLine, Col: curCol}
	case ']':
		tok = token.Token{Type: token.RBRACKET, Literal: string(l.ch), Line: curLine, Col: curCol}
	case '"':
		tok.Type = token.STRING
		tok.Literal = l.readString()
		tok.Line = curLine
		tok.Col = curCol
		l.log(tok)
		return tok
	case 0:
		tok.Literal = ""
		tok.Type = token.EOF
		tok.Line = curLine
		tok.Col = curCol
		l.log(tok)
		return tok
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			tok.Line = curLine
			tok.Col = curCol
			l.log(tok)
			return tok
		} else if isDigit(l.ch) {
			tok.Type = token.INT
			tok.Literal = l.readNumber()
			tok.Line = curLine
			tok.Col = curCol
			l.log(tok)
			return tok
		} else {
			tok = token.Token{Type: token.ILLEGAL, Literal: string(l.ch), Line: curLine, Col: curCol}
		}
	}

	l.readChar()
	l.log(tok)
	return tok
}

func (l *Lexer) readIdentifier() string {
	startPos := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[startPos:l.position]
}

func (l *Lexer) readNumber() string {
	startPos := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[startPos:l.position]
}

func (l *Lexer) readString() string {
	l.readChar() // 開始の " をスキップ
	startPos := l.position
	for l.ch != '"' && l.ch != 0 {
		l.readChar()
	}
	str := l.input[startPos:l.position]
	if l.ch == '"' {
		l.readChar() // 終了の " をスキップ
	}
	return str
}

func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}
