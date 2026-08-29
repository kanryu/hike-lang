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

	// 算術・論理・比較演算子
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

	// ビット演算子
	OR    = "|"
	CARET = "^"
	SHL   = "<<"
	SHR   = ">>"

	// 複合代入 / インクリメント / デクリメント
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
	BREAK    = "BREAK"
	CONTINUE = "CONTINUE"
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
	"break":    BREAK,
	"continue": CONTINUE,
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
