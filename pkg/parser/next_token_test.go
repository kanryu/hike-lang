package parser

import (
	"testing"

	"hikec-go/pkg/token"
)

func TestNextTokenClampsAtEndOfFiniteSlice(t *testing.T) {
	p := &Parser{
		tokens: []token.Token{{Type: token.IDENT, Literal: "x"}},
		pos:    0,
	}

	p.nextToken()
	if p.curToken.Type != "" || !p.peekTokenIs(token.IDENT) {
		t.Fatalf("initial advance: cur=%q peek=%q, want zero/IDENT", p.curToken.Type, p.peekToken.Type)
	}

	p.nextToken()
	if p.pos != len(p.tokens) {
		t.Fatalf("pos = %d, want %d", p.pos, len(p.tokens))
	}
	if !p.curTokenIs(token.IDENT) || !p.peekTokenIs(token.EOF) {
		t.Fatalf("after consuming final token: cur=%q peek=%q, want IDENT/EOF", p.curToken.Type, p.peekToken.Type)
	}

	p.nextToken()
	if !p.curTokenIs(token.EOF) || !p.peekTokenIs(token.EOF) {
		t.Fatalf("after repeated advance: cur=%q peek=%q, want EOF/EOF", p.curToken.Type, p.peekToken.Type)
	}

	p.pos = len(p.tokens) + 1
	p.nextToken()
	if p.pos != len(p.tokens) || !p.curTokenIs(token.EOF) || !p.peekTokenIs(token.EOF) {
		t.Fatalf("after overrun: pos=%d cur=%q peek=%q, want len/EOF/EOF", p.pos, p.curToken.Type, p.peekToken.Type)
	}
}
