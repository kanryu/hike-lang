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
	CHAR   = "CHAR" // シングルクォート文字リテラル ('a', '\n', 'あ' 等)
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

	ARROW = "<-" // 受信・チャネル演算子

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
	PACKAGE     = "PACKAGE"
	IMPORT      = "IMPORT"
	FUNC        = "FUNC"
	CFUNC       = "CFUNC"
	EXTERN      = "EXTERN" // 外部C言語等の関数宣言用 (extern func)
	JFUNC       = "JFUNC"  // WASM向けインラインJavaScript関数用 (jfunc)
	PASSTHROUGH = "PASSTHROUGH"
	MAP         = "MAP"
	CHAN        = "CHAN"
	ASYNC       = "ASYNC"
	RETURN      = "RETURN"
	TYPE        = "TYPE"
	STRUCT      = "STRUCT"
	INTERFACE   = "INTERFACE"
	CONST       = "CONST"
	IOTA        = "IOTA"
	RANGE       = "RANGE"
	BREAK       = "BREAK"
	CONTINUE    = "CONTINUE"
	IF          = "IF"
	ELSE        = "ELSE"
	FOR         = "FOR"
	SWITCH      = "SWITCH"
	CASE        = "CASE"
	DEFAULT     = "DEFAULT"
	DEFER       = "DEFER"
	NIL         = "NIL"
	VAR         = "VAR"

	// Internal Synthetic Tokens (AST変換・意味決定フェーズ用)
	IMPLICIT_CAST = "IMPLICIT_CAST"
)

var keywords = map[string]TokenType{
	"package":     PACKAGE,
	"import":      IMPORT,
	"func":        FUNC,
	"cfunc":       CFUNC,
	"extern":      EXTERN,
	"jfunc":       JFUNC,
	"passthrough": PASSTHROUGH,
	"var":         VAR,
	"const":       CONST,
	"type":        TYPE,
	"struct":      STRUCT,
	"interface":   INTERFACE,
	"map":         MAP,
	"chan":        CHAN,
	"Async":       ASYNC,
	"async":       ASYNC,
	"if":          IF,
	"else":        ELSE,
	"for":         FOR,
	"range":       RANGE,
	"switch":      SWITCH,
	"case":        CASE,
	"default":     DEFAULT,
	"return":      RETURN,
	"defer":       DEFER,
	"break":       BREAK,
	"continue":    CONTINUE,
	"nil":         NIL,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
