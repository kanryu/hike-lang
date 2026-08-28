package ast

import "hikec-go/pkg/token"

// 基本ノードインターフェース
type Node interface {
	TokenLiteral() string
}

type Statement interface {
	Node
	statementNode()
}

type Expression interface {
	Node
	expressionNode()
}

type Decl interface {
	Node
	declNode()
}

type TypeExpr interface {
	Node
	typeExprNode()
}
type ImportDecl struct {
	Path string
}

func (i *ImportDecl) declNode() {}

// Program 構造体に Imports を追加
type Program struct {
	Package string
	Imports []*ImportDecl
	Decls   []Decl
}

func (p *Program) TokenLiteral() string {
	if len(p.Decls) > 0 {
		return p.Decls[0].TokenLiteral()
	}
	return ""
}

type File struct {
	Filename string
	Package  *Identifier
	Imports  []*ImportDecl
	Decls    []Decl
}

// 識別子・リテラル
type Identifier struct {
	Token token.Token
	Value string
}

func (i *Identifier) expressionNode()      {}
func (i *Identifier) TokenLiteral() string { return i.Token.Literal }

type IntegerLiteral struct {
	Token token.Token
	Value int64
}

func (il *IntegerLiteral) expressionNode()      {}
func (il *IntegerLiteral) TokenLiteral() string { return il.Token.Literal }

type StringLiteral struct {
	Token token.Token
	Value string
}

func (sl *StringLiteral) expressionNode()      {}
func (sl *StringLiteral) TokenLiteral() string { return sl.Token.Literal }

type NilLiteral struct {
	Token token.Token
}

func (nl *NilLiteral) expressionNode()      {}
func (nl *NilLiteral) TokenLiteral() string { return nl.Token.Literal }

// 式 (Expressions)
type PrefixExpr struct {
	Token    token.Token
	Operator string
	Right    Expression
}

func (pe *PrefixExpr) expressionNode()      {}
func (pe *PrefixExpr) TokenLiteral() string { return pe.Token.Literal }

type BinaryExpr struct {
	Token    token.Token
	Left     Expression
	Operator string
	Right    Expression
}

func (be *BinaryExpr) expressionNode()      {}
func (be *BinaryExpr) TokenLiteral() string { return be.Token.Literal }

type IndexExpr struct {
	Token token.Token
	Left  Expression
	Index Expression
}

func (ie *IndexExpr) expressionNode()      {}
func (ie *IndexExpr) TokenLiteral() string { return ie.Token.Literal }

type GenericIndexExpr struct {
	Token token.Token
	Left  Expression
	Index TypeExpr
}

func (ge *GenericIndexExpr) expressionNode()      {}
func (ge *GenericIndexExpr) TokenLiteral() string { return ge.Token.Literal }

type MemberExpr struct {
	Token  token.Token
	Object Expression
	Field  *Identifier
}

func (me *MemberExpr) expressionNode()      {}
func (me *MemberExpr) TokenLiteral() string { return me.Token.Literal }

type CallExpr struct {
	Token    token.Token
	Function Expression
	Args     []Expression
}

func (ce *CallExpr) expressionNode()      {}
func (ce *CallExpr) TokenLiteral() string { return ce.Token.Literal }

// 型表現 (TypeExpr)
type NamedType struct {
	Token   token.Token
	Package *Identifier // "mem.Allocator" の "mem"（修飾がない場合は nil）
	Name    *Identifier // 型名本体
}

func (nt *NamedType) typeExprNode()        {}
func (nt *NamedType) TokenLiteral() string { return nt.Token.Literal }

type PointerType struct {
	Token token.Token
	Base  TypeExpr
}

func (pt *PointerType) typeExprNode()        {}
func (pt *PointerType) TokenLiteral() string { return pt.Token.Literal }

type StructType struct {
	Token  token.Token
	Fields []*FieldDecl
}

func (st *StructType) typeExprNode()        {}
func (st *StructType) TokenLiteral() string { return st.Token.Literal }

type FieldDecl struct {
	Name *Identifier
	Type TypeExpr
}

type ParamDecl struct {
	Name *Identifier
	Type TypeExpr
}

// 宣言 (Declarations)
type TypeDecl struct {
	Token token.Token
	Name  *Identifier
	Type  TypeExpr
}

func (td *TypeDecl) declNode()            {}
func (td *TypeDecl) TokenLiteral() string { return td.Token.Literal }

type FuncDecl struct {
	Token       token.Token
	Receiver    *ParamDecl // 追加: メソッドの場合は非nil (例: b *Builder)、通常関数は nil
	Name        *Identifier
	Params      []*ParamDecl
	IsVariadic  bool
	ReturnTypes []TypeExpr
	Body        *BlockStmt
}

func (fd *FuncDecl) declNode()            {}
func (fd *FuncDecl) TokenLiteral() string { return fd.Token.Literal }

// 文 (Statements)
type BlockStmt struct {
	Token      token.Token
	Statements []Statement
}

func (bs *BlockStmt) statementNode()       {}
func (bs *BlockStmt) TokenLiteral() string { return bs.Token.Literal }

type ExprStmt struct {
	Token token.Token
	Expr  Expression
}

func (es *ExprStmt) statementNode()       {}
func (es *ExprStmt) TokenLiteral() string { return es.Token.Literal }

type AssignStmt struct {
	Left  []Expression
	Right []Expression
}

func (as *AssignStmt) statementNode()       {}
func (as *AssignStmt) TokenLiteral() string { return "=" }

type ReturnStmt struct {
	Token  token.Token
	Values []Expression
}

func (rs *ReturnStmt) statementNode()       {}
func (rs *ReturnStmt) TokenLiteral() string { return rs.Token.Literal }

type DeferStmt struct {
	Token token.Token
	Call  *CallExpr
}

func (ds *DeferStmt) statementNode()       {}
func (ds *DeferStmt) TokenLiteral() string { return ds.Token.Literal }

type IfStmt struct {
	Token       token.Token
	Condition   Expression
	Consequence *BlockStmt
	Alternative Statement // *BlockStmt (else) または *IfStmt (else if)
}

func (is *IfStmt) statementNode()       {}
func (is *IfStmt) TokenLiteral() string { return is.Token.Literal }

type ForStmt struct {
	Token token.Token
	Init  Statement
	Cond  Expression
	Post  Statement
	Body  *BlockStmt
}

func (fs *ForStmt) statementNode()       {}
func (fs *ForStmt) TokenLiteral() string { return fs.Token.Literal }

type CaseClause struct {
	Token  token.Token  // 'case' または 'default'
	Values []Expression // default の場合は空
	Body   []Statement
}

func (cc *CaseClause) statementNode()       {}
func (cc *CaseClause) TokenLiteral() string { return cc.Token.Literal }

type SwitchStmt struct {
	Token token.Token // 'switch'
	Value Expression
	Cases []*CaseClause
}

func (ss *SwitchStmt) statementNode()       {}
func (ss *SwitchStmt) TokenLiteral() string { return ss.Token.Literal }
