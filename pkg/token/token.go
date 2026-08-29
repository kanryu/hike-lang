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

	// 識別子・リテラル
	IDENT  = "IDENT"
	INT    = "INT"
	STRING = "STRING"

	// 演算子
	ASSIGN          = "="
	PLUS            = "+"
	MINUS           = "-"
	BANG            = "!"
	ASTERISK        = "*"
	SLASH           = "/"
	LT              = "<"
	GT              = ">"
	EQ              = "=="
	NEQ             = "!="
	LE              = "<="
	GE              = ">="
	LAND            = "&&"
	LOR             = "||"
	AMPERSAND       = "&"
	INC             = "++"
	DEC             = "--"
	PLUS_ASSIGN     = "+="
	MINUS_ASSIGN    = "-="
	ASTERISK_ASSIGN = "*="
	SLASH_ASSIGN    = "/="

	// 区切り文字
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

	// キーワード
	PACKAGE  = "PACKAGE"
	IMPORT   = "IMPORT"
	FUNC     = "FUNC"
	RETURN   = "RETURN"
	TYPE     = "TYPE"
	STRUCT   = "STRUCT"
	CONST    = "CONST"
	IOTA     = "IOTA"
	RANGE    = "RANGE"
	BREAK    = "BREAK"    // 追加
	CONTINUE = "CONTINUE" // 追加
	IF       = "IF"
	ELSE     = "ELSE"
	FOR      = "FOR"
	SWITCH   = "SWITCH"
	CASE     = "CASE"
	DEFAULT  = "DEFAULT"
	DEFER    = "DEFER"
	NIL      = "NIL"
)

var keywords = map[string]TokenType{
	"package":  PACKAGE,
	"import":   IMPORT,
	"func":     FUNC,
	"return":   RETURN,
	"type":     TYPE,
	"struct":   STRUCT,
	"const":    CONST,
	"iota":     IOTA,
	"range":    RANGE,
	"break":    BREAK,    // 追加
	"continue": CONTINUE, // 追加
	"if":       IF,
	"else":     ELSE,
	"for":      FOR,
	"switch":   SWITCH,
	"case":     CASE,
	"default":  DEFAULT,
	"defer":    DEFER,
	"nil":      NIL,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
