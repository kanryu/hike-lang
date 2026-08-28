package token

type TokenType string

const (
	ILLEGAL TokenType = "ILLEGAL"
	EOF     TokenType = "EOF"

	IDENT  TokenType = "IDENT"
	INT    TokenType = "INT"
	STRING TokenType = "STRING"

	ASSIGN   TokenType = "="
	DEFINE   TokenType = ":="
	PLUS     TokenType = "+"
	MINUS    TokenType = "-"
	ASTERISK TokenType = "*"
	SLASH    TokenType = "/"
	NOT      TokenType = "!"

	LAND      TokenType = "&&"
	AMPERSAND TokenType = "&"
	LOR       TokenType = "||"

	EQ  TokenType = "=="
	NEQ TokenType = "!="
	LT  TokenType = "<"
	GT  TokenType = ">"
	LE  TokenType = "<="
	GE  TokenType = ">="

	COMMA     TokenType = ","
	SEMICOLON TokenType = ";"
	COLON     TokenType = ":"
	DOT       TokenType = "."
	ELLIPSIS  TokenType = "..."

	LPAREN   TokenType = "("
	RPAREN   TokenType = ")"
	LBRACE   TokenType = "{"
	RBRACE   TokenType = "}"
	LBRACKET TokenType = "["
	RBRACKET TokenType = "]"

	PACKAGE TokenType = "package"
	IMPORT  TokenType = "import"
	TYPE    TokenType = "type"
	STRUCT  TokenType = "struct"
	FUNC    TokenType = "func"
	CONST   TokenType = "const" // 追加
	RETURN  TokenType = "return"
	DEFER   TokenType = "defer"
	IF      TokenType = "if"
	ELSE    TokenType = "else"
	FOR     TokenType = "for"
	SWITCH  TokenType = "switch"
	CASE    TokenType = "case"
	DEFAULT TokenType = "default"
	NIL     TokenType = "nil"
)

var keywords = map[string]TokenType{
	"package": PACKAGE,
	"import":  IMPORT,
	"type":    TYPE,
	"struct":  STRUCT,
	"func":    FUNC,
	"const":   CONST, // 追加
	"return":  RETURN,
	"defer":   DEFER,
	"if":      IF,
	"else":    ELSE,
	"for":     FOR,
	"switch":  SWITCH,
	"case":    CASE,
	"default": DEFAULT,
	"nil":     NIL,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}
