package ast

import "hikec-go/pkg/token"

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
	Token token.Token
	Path  string
}

func (i *ImportDecl) declNode()            {}
func (i *ImportDecl) TokenLiteral() string { return i.Token.Literal }

type ConstDecl struct {
	Token token.Token
	Name  *Identifier
	Value Expression
}

func (cd *ConstDecl) declNode()            {}
func (cd *ConstDecl) TokenLiteral() string { return cd.Token.Literal }

// トップレベルおよびローカルの変数宣言ノード
type VarDecl struct {
	Token token.Token
	Name  *Identifier
	Type  TypeExpr
	Value Expression
}

func (vd *VarDecl) declNode()            {}
func (vd *VarDecl) statementNode()       {}
func (vd *VarDecl) TokenLiteral() string { return vd.Token.Literal }

// AssignStmt にも declNode を実装
func (as *AssignStmt) declNode() {}

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

func (f *File) TokenLiteral() string {
	if f.Package != nil {
		return f.Package.TokenLiteral()
	}
	if len(f.Decls) > 0 {
		return f.Decls[0].TokenLiteral()
	}
	return ""
}

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

type FloatLiteral struct {
	Token token.Token
	Value float64
}

func (fl *FloatLiteral) expressionNode()      {}
func (fl *FloatLiteral) TokenLiteral() string { return fl.Token.Literal }
func (fl *FloatLiteral) String() string       { return fl.Token.Literal }

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
	Token       token.Token
	Function    Expression
	Args        []Expression
	HasEllipsis bool
}

func (ce *CallExpr) expressionNode()      {}
func (ce *CallExpr) TokenLiteral() string { return ce.Token.Literal }

type NamedType struct {
	Token   token.Token
	Package *Identifier
	Name    *Identifier
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
	Token      token.Token
	Name       *Identifier
	Type       TypeExpr
	IsEmbedded bool
}

func (fd *FieldDecl) statementNode()       {}
func (fd *FieldDecl) TokenLiteral() string { return fd.Token.Literal }

type ParamDecl struct {
	Token token.Token
	Name  *Identifier
	Type  TypeExpr
}

func (pd *ParamDecl) TokenLiteral() string {
	if pd.Name != nil {
		return pd.Name.TokenLiteral()
	}
	return pd.Token.Literal
}

type TypeDecl struct {
	Token token.Token
	Name  *Identifier
	Type  TypeExpr
}

func (td *TypeDecl) declNode()            {}
func (td *TypeDecl) TokenLiteral() string { return td.Token.Literal }

type FuncDecl struct {
	Token       token.Token
	Receiver    *ParamDecl
	Name        *Identifier
	Params      []*ParamDecl
	IsVariadic  bool
	ReturnTypes []TypeExpr
	Body        *BlockStmt
}

func (fd *FuncDecl) declNode()            {}
func (fd *FuncDecl) TokenLiteral() string { return fd.Token.Literal }

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
	Token token.Token
	Left  []Expression
	Right []Expression
	Type  TypeExpr // var x any = 42 などの型指定
}

func (as *AssignStmt) statementNode() {}
func (as *AssignStmt) TokenLiteral() string {
	if as.Token.Literal != "" {
		return as.Token.Literal
	}
	return "="
}

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

type BreakStmt struct {
	Token token.Token
}

func (bs *BreakStmt) statementNode()       {}
func (bs *BreakStmt) TokenLiteral() string { return bs.Token.Literal }

type ContinueStmt struct {
	Token token.Token
}

func (cs *ContinueStmt) statementNode()       {}
func (cs *ContinueStmt) TokenLiteral() string { return cs.Token.Literal }

type IfStmt struct {
	Token       token.Token
	Init        Statement // 追加: if init; cond
	Condition   Expression
	Consequence *BlockStmt
	Alternative Statement
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

type ForRangeStmt struct {
	Token token.Token
	Key   Expression
	Value Expression
	X     Expression
	Body  *BlockStmt
}

func (fr *ForRangeStmt) statementNode()       {}
func (fr *ForRangeStmt) TokenLiteral() string { return fr.Token.Literal }

type IotaExpr struct {
	Token token.Token
	Value int64
}

func (ie *IotaExpr) expressionNode()      {}
func (ie *IotaExpr) TokenLiteral() string { return ie.Token.Literal }

type CaseClause struct {
	Token  token.Token
	Values []Expression
	Body   []Statement
}

func (cc *CaseClause) statementNode()       {}
func (cc *CaseClause) TokenLiteral() string { return cc.Token.Literal }

type SwitchStmt struct {
	Token token.Token
	Init  Statement // 追加: switch init; expr
	Value Expression
	Cases []*CaseClause
}

func (ss *SwitchStmt) statementNode()       {}
func (ss *SwitchStmt) TokenLiteral() string { return ss.Token.Literal }

type SliceType struct {
	Token token.Token
	Elem  TypeExpr
}

func (s *SliceType) typeExprNode()        {}
func (s *SliceType) TokenLiteral() string { return s.Token.Literal }
func (s *SliceType) expressionNode()      {}
func (pt *PointerType) expressionNode()   {}
func (nt *NamedType) expressionNode()     {}

type SliceExpr struct {
	Token token.Token
	Left  Expression
	Low   Expression
	High  Expression
}

func (s *SliceExpr) expressionNode()      {}
func (s *SliceExpr) TokenLiteral() string { return s.Token.Literal }

type SliceLiteral struct {
	Token    token.Token
	Type     *SliceType
	Elements []Expression
}

func (sl *SliceLiteral) expressionNode()      {}
func (sl *SliceLiteral) TokenLiteral() string { return sl.Token.Literal }

type StructFieldValue struct {
	Name  *Identifier
	Value Expression
}

type StructLiteral struct {
	Token  token.Token
	Type   *NamedType
	Fields []*StructFieldValue
}

func (sl *StructLiteral) expressionNode()      {}
func (sl *StructLiteral) TokenLiteral() string { return sl.Token.Literal }

type ArrayType struct {
	Token token.Token
	Len   int64
	Elem  TypeExpr
}

func (at *ArrayType) typeExprNode()        {}
func (at *ArrayType) expressionNode()      {}
func (at *ArrayType) TokenLiteral() string { return at.Token.Literal }

type ArrayLiteral struct {
	Token    token.Token
	Type     *ArrayType
	Elements []Expression
}

func (al *ArrayLiteral) expressionNode()      {}
func (al *ArrayLiteral) TokenLiteral() string { return al.Token.Literal }

// インターフェースのメソッドシグネチャノード
type MethodSig struct {
	Token       token.Token
	Name        *Identifier
	ParamTypes  []TypeExpr
	ReturnTypes []TypeExpr
}

func (ms *MethodSig) expressionNode()      {}
func (ms *MethodSig) TokenLiteral() string { return ms.Token.Literal }

// インターフェース型ノード
type InterfaceType struct {
	Token   token.Token
	Methods []*MethodSig
}

func (it *InterfaceType) typeExprNode()        {}
func (it *InterfaceType) expressionNode()      {}
func (it *InterfaceType) TokenLiteral() string { return it.Token.Literal }

// 型アサーション: x.(TargetType)
type TypeAssertExpr struct {
	Token  token.Token
	Expr   Expression
	Target TypeExpr
}

func (tae *TypeAssertExpr) expressionNode()      {}
func (tae *TypeAssertExpr) TokenLiteral() string { return tae.Token.Literal }

// 関数型ノード: func(int, int) int
type FuncType struct {
	Token       token.Token
	ParamTypes  []TypeExpr
	ReturnTypes []TypeExpr
}

func (ft *FuncType) typeExprNode()        {}
func (ft *FuncType) expressionNode()      {}
func (ft *FuncType) TokenLiteral() string { return ft.Token.Literal }

// 無名関数リテラルノード: func(a int) int { return a * 2 }
type FuncLit struct {
	Token       token.Token
	Params      []*ParamDecl
	ReturnTypes []TypeExpr
	Body        *BlockStmt
}

func (fl *FuncLit) expressionNode()      {}
func (fl *FuncLit) TokenLiteral() string { return fl.Token.Literal }

type TypeCaseClause struct {
	Token token.Token
	Types []TypeExpr
	Body  []Statement
}

func (tcc *TypeCaseClause) statementNode()       {}
func (tcc *TypeCaseClause) TokenLiteral() string { return tcc.Token.Literal }

type TypeSwitchStmt struct {
	Token    token.Token
	Init     Statement
	Variable *Identifier
	Expr     Expression
	Cases    []*TypeCaseClause
}

func (tss *TypeSwitchStmt) statementNode()       {}
func (tss *TypeSwitchStmt) TokenLiteral() string { return tss.Token.Literal }
