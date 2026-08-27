package lexer

import (
	"fmt"

	"hikec-go/pkg/token"
)

type Lexer struct {
	input        string
	position     int
	readPosition int
	ch           byte
	line         int
	col          int
}

func New(input string) *Lexer {
	l := &Lexer{input: input, line: 1, col: 0}
	l.readChar()
	return l
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
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
		} else if l.ch == '/' && l.peekChar() == '*' {
			l.readChar()
			l.readChar()
			for {
				if l.ch == 0 {
					break
				}
				if l.ch == '\n' {
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
		} else {
			break
		}
	}
}

func (l *Lexer) NextToken() token.Token {
	var tok token.Token

	l.skipWhitespace()

	line := l.line
	col := l.col

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.EQ, Literal: string(ch) + string(l.ch), Line: line, Col: col}
		} else {
			tok = token.Token{Type: token.ASSIGN, Literal: string(l.ch), Line: line, Col: col}
		}
	case ':':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.DEFINE, Literal: string(ch) + string(l.ch), Line: line, Col: col}
		} else {
			tok = token.Token{Type: token.COLON, Literal: string(l.ch), Line: line, Col: col}
		}
	case '+':
		tok = token.Token{Type: token.PLUS, Literal: string(l.ch), Line: line, Col: col}
	case '-':
		tok = token.Token{Type: token.MINUS, Literal: string(l.ch), Line: line, Col: col}
	case '*':
		tok = token.Token{Type: token.ASTERISK, Literal: string(l.ch), Line: line, Col: col}
	case '/':
		tok = token.Token{Type: token.SLASH, Literal: string(l.ch), Line: line, Col: col}
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.NEQ, Literal: string(ch) + string(l.ch), Line: line, Col: col}
		} else {
			tok = token.Token{Type: token.NOT, Literal: string(l.ch), Line: line, Col: col}
		}
	case '<':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LE, Literal: string(ch) + string(l.ch), Line: line, Col: col}
		} else {
			tok = token.Token{Type: token.LT, Literal: string(l.ch), Line: line, Col: col}
		}
	case '>':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.GE, Literal: string(ch) + string(l.ch), Line: line, Col: col}
		} else {
			tok = token.Token{Type: token.GT, Literal: string(l.ch), Line: line, Col: col}
		}
	case '&':
		if l.peekChar() == '&' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LAND, Literal: string(ch) + string(l.ch), Line: line, Col: col}
		} else {
			tok = token.Token{Type: token.ILLEGAL, Literal: string(l.ch), Line: line, Col: col}
		}
	case '|':
		if l.peekChar() == '|' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LOR, Literal: string(ch) + string(l.ch), Line: line, Col: col}
		} else {
			tok = token.Token{Type: token.ILLEGAL, Literal: string(l.ch), Line: line, Col: col}
		}
	case ';':
		tok = token.Token{Type: token.SEMICOLON, Literal: string(l.ch), Line: line, Col: col}
	case ',':
		tok = token.Token{Type: token.COMMA, Literal: string(l.ch), Line: line, Col: col}
	case '.':
		if l.peekChar() == '.' && l.readPosition < len(l.input) && l.input[l.readPosition] == '.' {
			l.readChar()
			l.readChar()
			tok = token.Token{Type: token.ELLIPSIS, Literal: "...", Line: line, Col: col}
		} else {
			tok = token.Token{Type: token.DOT, Literal: string(l.ch), Line: line, Col: col}
		}
	case '(':
		tok = token.Token{Type: token.LPAREN, Literal: string(l.ch), Line: line, Col: col}
	case ')':
		tok = token.Token{Type: token.RPAREN, Literal: string(l.ch), Line: line, Col: col}
	case '{':
		tok = token.Token{Type: token.LBRACE, Literal: string(l.ch), Line: line, Col: col}
	case '}':
		tok = token.Token{Type: token.RBRACE, Literal: string(l.ch), Line: line, Col: col}
	case '[':
		tok = token.Token{Type: token.LBRACKET, Literal: string(l.ch), Line: line, Col: col}
	case ']':
		tok = token.Token{Type: token.RBRACKET, Literal: string(l.ch), Line: line, Col: col}
	case '\'':
		l.readChar()
		var charVal byte
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				charVal = '\n'
			case 't':
				charVal = '\t'
			case 'r':
				charVal = '\r'
			case '0':
				charVal = 0
			case '\\':
				charVal = '\\'
			case '\'':
				charVal = '\''
			default:
				charVal = l.ch
			}
		} else {
			charVal = l.ch
		}
		l.readChar()
		if l.ch == '\'' {
			l.readChar()
		}
		return token.Token{
			Type:    token.INT,
			Literal: fmt.Sprintf("%d", int(charVal)),
			Line:    line,
			Col:     col,
		}
	case '"':
		tok.Type = token.STRING
		tok.Literal = l.readString()
		tok.Line = line
		tok.Col = col
		return tok
	case 0:
		tok.Literal = ""
		tok.Type = token.EOF
		tok.Line = line
		tok.Col = col
		return tok
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			tok.Line = line
			tok.Col = col
			return tok
		} else if isDigit(l.ch) {
			tok.Literal = l.readNumber()
			tok.Type = token.INT
			tok.Line = line
			tok.Col = col
			return tok
		} else {
			tok = token.Token{Type: token.ILLEGAL, Literal: string(l.ch), Line: line, Col: col}
		}
	}

	l.readChar()
	return tok
}

func (l *Lexer) readString() string {
	l.readChar()
	position := l.position
	for l.ch != '"' && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
		}
		l.readChar()
	}
	str := l.input[position:l.position]
	l.readChar()
	return str
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}

func (l *Lexer) readNumber() string {
	position := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}

func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}
