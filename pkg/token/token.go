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

	IDENT  = "IDENT"
	INT    = "INT"
	STRING = "STRING"

	ASSIGN    = "="
	PLUS      = "+"
	MINUS     = "-"
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

	PACKAGE   = "PACKAGE"
	IMPORT    = "IMPORT"
	FUNC      = "FUNC"
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
	VAR       = "VAR" // 追加
)

var keywords = map[string]TokenType{
	"package":   PACKAGE,
	"import":    IMPORT,
	"func":      FUNC,
	"return":    RETURN,
	"type":      TYPE,
	"struct":    STRUCT,
	"interface": INTERFACE,
	"const":     CONST,
	"iota":      IOTA,
	"range":     RANGE,
	"break":     BREAK,
	"continue":  CONTINUE,
	"if":        IF,
	"else":      ELSE,
	"for":       FOR,
	"switch":    SWITCH,
	"case":      CASE,
	"default":   DEFAULT,
	"defer":     DEFER,
	"nil":       NIL,
	"var":       VAR, // 追加
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
