package token

type TokenType string

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}

const (
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"

	// Literals
	IDENT  = "IDENT"
	INT    = "INT"
	FLOAT  = "FLOAT"
	STRING = "STRING"

	// Operators
	ASSIGN    = "="
	PLUS      = "+"
	MINUS     = "-"
	PERCENT   = "%"
	BANG      = "!"
	ASTERISK  = "*"
	SLASH     = "/"
	LT        = "<"
	GT        = ">"
	EQ        = "=="
	NEQ       = "!="
	LE        = "<="
	GE        = ">="
	LAND      = "&&"
	LOR       = "||"
	AMPERSAND = "&"

	OR    = "|"
	CARET = "^"
	SHL   = "<<"
	SHR   = ">>"

	INC             = "++"
	DEC             = "--"
	PLUS_ASSIGN     = "+="
	MINUS_ASSIGN    = "-="
	ASTERISK_ASSIGN = "*="
	SLASH_ASSIGN    = "/="
	AND_ASSIGN      = "&="
	OR_ASSIGN       = "|="
	XOR_ASSIGN      = "^="
	SHL_ASSIGN      = "<<="
	SHR_ASSIGN      = ">>="

	COMMA     = ","
	SEMICOLON = ";"
	COLON     = ":"
	DOT       = "."
	ELLIPSIS  = "..."
	DEFINE    = ":="

	LPAREN   = "("
	RPAREN   = ")"
	LBRACE   = "{"
	RBRACE   = "}"
	LBRACKET = "["
	RBRACKET = "]"

	// Keywords
	PACKAGE   = "PACKAGE"
	IMPORT    = "IMPORT"
	FUNC      = "FUNC"
	MAP       = "MAP"
	RETURN    = "RETURN"
	TYPE      = "TYPE"
	STRUCT    = "STRUCT"
	INTERFACE = "INTERFACE"
	CONST     = "CONST"
	IOTA      = "IOTA"
	RANGE     = "RANGE"
	BREAK     = "BREAK"
	CONTINUE  = "CONTINUE"
	IF        = "IF"
	ELSE      = "ELSE"
	FOR       = "FOR"
	SWITCH    = "SWITCH"
	CASE      = "CASE"
	DEFAULT   = "DEFAULT"
	DEFER     = "DEFER"
	NIL       = "NIL"
	VAR       = "VAR"

	// Internal Synthetic Tokens (AST変換・意味決定フェーズ用)
	IMPLICIT_CAST = "IMPLICIT_CAST"
)

var keywords = map[string]TokenType{
	"package":   PACKAGE,
	"import":    IMPORT,
	"func":      FUNC,
	"var":       VAR,
	"const":     CONST,
	"type":      TYPE,
	"struct":    STRUCT,
	"interface": INTERFACE,
	"map":       MAP,
	"if":        IF,
	"else":      ELSE,
	"for":       FOR,
	"range":     RANGE,
	"switch":    SWITCH,
	"case":      CASE,
	"default":   DEFAULT,
	"return":    RETURN,
	"defer":     DEFER,
	"break":     BREAK,
	"continue":  CONTINUE,
	"nil":       NIL,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
