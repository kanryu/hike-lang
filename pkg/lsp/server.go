// Package lsp implements the small, dependency-free JSON-RPC surface used by
// hike-lsp. Keeping the protocol layer here makes the command usable from
// editors other than VS Code as well.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/diag"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
	"hikec-go/pkg/sema"
	"hikec-go/pkg/token"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}
type TextDocumentItem struct {
	URI     string `json:"uri"`
	Text    string `json:"text"`
	Version int    `json:"version"`
}
type TextDocumentContentChangeEvent struct {
	Text  string `json:"text"`
	Range *Range `json:"range,omitempty"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
type SymbolInformation struct {
	Name     string   `json:"name"`
	Detail   string   `json:"detail,omitempty"`
	Kind     int      `json:"kind"`
	Location Location `json:"location"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type symbol struct {
	Name, Detail, Documentation string
	Kind                        int
	URI                         string
	Range                       Range
	Selection                   Range
}
type document struct {
	URI, Text   string
	Program     *ast.Program
	Tokens      []token.Token
	Symbols     []symbol
	Definitions map[string]symbol
	Diagnostics []Diagnostic
}

type Server struct {
	in       *bufio.Reader
	out      io.Writer
	mu       sync.Mutex
	docs     map[string]*document
	shutdown bool
}

func New(in io.Reader, out io.Writer) *Server {
	return &Server{in: bufio.NewReader(in), out: out, docs: make(map[string]*document)}
}

func (s *Server) Run() error {
	for {
		body, err := readFrame(s.in)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		var msg rpcMessage
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		result, requestErr, exit := s.handle(msg)
		if len(msg.ID) != 0 && string(msg.ID) != "null" {
			s.respond(msg.ID, result, requestErr)
		}
		if exit {
			return nil
		}
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			length, _ = strconv.Atoi(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	b := make([]byte, length)
	_, err := io.ReadFull(r, b)
	return b, err
}

func (s *Server) respond(id json.RawMessage, result any, e *rpcError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: e})
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(b), b)
}
func (s *Server) notify(method string, params any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(b), b)
}

func (s *Server) handle(m rpcMessage) (any, *rpcError, bool) {
	switch m.Method {
	case "initialize":
		return map[string]any{"capabilities": map[string]any{"textDocumentSync": map[string]any{"openClose": true, "change": 1}, "definitionProvider": true, "hoverProvider": true, "documentSymbolProvider": true}}, nil, false
	case "shutdown":
		s.shutdown = true
		return nil, nil, false
	case "exit":
		return nil, nil, true
	case "initialized":
		return nil, nil, false
	case "textDocument/didOpen":
		var p struct {
			TextDocument TextDocumentItem `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		s.update(p.TextDocument.URI, p.TextDocument.Text)
		return nil, nil, false
	case "textDocument/didChange":
		var p struct {
			TextDocument   TextDocumentIdentifier           `json:"textDocument"`
			ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
		}
		json.Unmarshal(m.Params, &p)
		if len(p.ContentChanges) > 0 {
			s.update(p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text)
		}
		return nil, nil, false
	case "textDocument/didClose":
		var p struct {
			TextDocument TextDocumentIdentifier `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		delete(s.docs, p.TextDocument.URI)
		s.notify("textDocument/publishDiagnostics", map[string]any{"uri": p.TextDocument.URI, "diagnostics": []Diagnostic{}})
		return nil, nil, false
	case "textDocument/definition":
		var p struct {
			TextDocument TextDocumentIdentifier `json:"textDocument"`
			Position     Position               `json:"position"`
		}
		json.Unmarshal(m.Params, &p)
		return s.definition(p.TextDocument.URI, p.Position), nil, false
	case "textDocument/hover":
		var p struct {
			TextDocument TextDocumentIdentifier `json:"textDocument"`
			Position     Position               `json:"position"`
		}
		json.Unmarshal(m.Params, &p)
		return s.hover(p.TextDocument.URI, p.Position), nil, false
	case "textDocument/documentSymbol":
		var p struct {
			TextDocument TextDocumentIdentifier `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		return s.symbols(p.TextDocument.URI), nil, false
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + m.Method}, false
	}
}

func (s *Server) update(uri, text string) {
	d := analyze(uri, text)
	s.docs[uri] = d
	s.notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": d.Diagnostics})
}

func analyze(uri, text string) *document {
	d := &document{URI: uri, Text: text, Definitions: map[string]symbol{}}
	lx := lexer.New(text)
	for {
		t := lx.NextToken()
		d.Tokens = append(d.Tokens, t)
		if t.Type == token.EOF {
			break
		}
	}
	p := parser.New(lexer.New(text))
	d.Program = p.ParseProgram()
	for _, raw := range p.Errors() {
		x := diag.ParseDiagnostic(uri, raw)
		d.Diagnostics = append(d.Diagnostics, makeDiagnostic(x, text))
	}
	for _, t := range d.Tokens {
		if t.Type == token.ILLEGAL {
			d.Diagnostics = append(d.Diagnostics, Diagnostic{Range: tokenRange(text, t), Severity: 1, Source: "hike-lsp", Message: "illegal token: " + t.Literal})
		}
	}
	if d.Program != nil {
		r := diag.NewReporter()
		func() { defer func() { recover() }(); sema.AnalyzeWithReporter(d.Program, r, uri) }()
		for _, x := range r.Diagnostics() {
			d.Diagnostics = append(d.Diagnostics, makeDiagnostic(x, text))
		}
		collectSymbols(d)
	}
	return d
}

func makeDiagnostic(x diag.Diagnostic, text string) Diagnostic {
	line, col := x.Line-1, x.Col-1
	if line < 0 {
		line = 0
	}
	if col < 0 {
		col = 0
	}
	return Diagnostic{Range: Range{Start: Position{line, col}, End: Position{line, col + 1}}, Severity: 1, Source: "hike-lsp", Message: x.Message}
}
func tokenRange(text string, t token.Token) Range {
	l := max(t.Line-1, 0)
	c := max(t.Col-1, 0)
	return Range{Start: Position{l, c}, End: Position{l, c + max(len([]rune(t.Literal)), 1)}}
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func collectSymbols(d *document) {
	if d.Program == nil {
		return
	}
	for _, decl := range d.Program.Decls {
		switch x := decl.(type) {
		case *ast.TypeDecl:
			if x.Name != nil {
				addSymbol(d, x.Name.Value, "struct/type", 23, x.Name.Token, "type "+x.Name.Value)
			}
		case *ast.VarDecl:
			if x.Name != nil {
				addSymbol(d, x.Name.Value, "variable", 13, x.Name.Token, "var "+x.Name.Value)
			}
		case *ast.ConstDecl:
			if x.Name != nil {
				addSymbol(d, x.Name.Value, "constant", 14, x.Name.Token, "const "+x.Name.Value)
			}
		case *ast.FuncDecl:
			if x.Name != nil {
				kind := 12
				detail := "func " + x.Name.Value
				if x.Receiver != nil {
					kind = 6
					detail = "method " + x.Name.Value + " (receiver)"
					if x.Receiver.Name != nil {
						addSymbol(d, x.Receiver.Name.Value, "receiver", 13, x.Receiver.Name.Token, "receiver")
					}
				}
				addSymbol(d, x.Name.Value, detail, kind, x.Name.Token, detail)
				collectParams(d, x.Params)
				collectBlock(d, x.Body)
			}
		case *ast.CFuncDecl:
			if x.Name != nil {
				addSymbol(d, x.Name.Value, "cfunc "+x.Name.Value, 12, x.Name.Token, "cfunc "+x.Name.Value)
				collectParams(d, x.Params)
				collectBlock(d, x.Body)
			}
		case *ast.JFuncDecl:
			if x.Name != nil {
				addSymbol(d, x.Name.Value, "jfunc "+x.Name.Value, 12, x.Name.Token, "jfunc "+x.Name.Value)
				collectParams(d, x.Params)
			}
		}
	}
	for i := range d.Symbols {
		for j := i + 1; j < len(d.Symbols); j++ {
			if d.Symbols[j].Name < d.Symbols[i].Name {
				d.Symbols[i], d.Symbols[j] = d.Symbols[j], d.Symbols[i]
			}
		}
	}
}
func collectParams(d *document, ps []*ast.ParamDecl) {
	for _, p := range ps {
		if p != nil && p.Name != nil {
			addSymbol(d, p.Name.Value, "parameter", 13, p.Name.Token, "parameter "+p.Name.Value)
		}
	}
}
func collectBlock(d *document, b *ast.BlockStmt) {
	if b == nil {
		return
	}
	for _, st := range b.Statements {
		switch x := st.(type) {
		case *ast.VarDecl:
			if x.Name != nil {
				addSymbol(d, x.Name.Value, "local variable", 13, x.Name.Token, "var "+x.Name.Value)
			}
		case *ast.AssignStmt:
			if x.Type != nil {
				for _, e := range x.Left {
					if id, ok := e.(*ast.Identifier); ok {
						addSymbol(d, id.Value, "local variable", 13, id.Token, "var "+id.Value)
					}
				}
			}
		case *ast.IfStmt:
			collectBlock(d, x.Consequence)
			if x.Alternative != nil {
				collectStatement(d, x.Alternative)
			}
		case *ast.ForStmt:
			collectStatement(d, x.Init)
			collectStatement(d, x.Post)
			collectBlock(d, x.Body)
		case *ast.ForRangeStmt:
			collectBlock(d, x.Body)
		case *ast.BlockStmt:
			collectBlock(d, x)
		case *ast.LockStmt:
			collectBlock(d, x.Body)
		case *ast.AreaStmt:
			collectBlock(d, x.Body)
		case *ast.SwitchStmt:
			for _, c := range x.Cases {
				for _, n := range c.Body {
					collectStatement(d, n)
				}
			}
		case *ast.TypeSwitchStmt:
			for _, c := range x.Cases {
				for _, n := range c.Body {
					collectStatement(d, n)
				}
			}
		}
	}
}
func collectStatement(d *document, s ast.Statement) {
	if s == nil {
		return
	}
	switch x := s.(type) {
	case *ast.BlockStmt:
		collectBlock(d, x)
	case *ast.VarDecl:
		if x.Name != nil {
			addSymbol(d, x.Name.Value, "local variable", 13, x.Name.Token, "var "+x.Name.Value)
		}
	case *ast.IfStmt:
		collectBlock(d, x.Consequence)
	case *ast.ForStmt:
		collectBlock(d, x.Body)
	}
}
func addSymbol(d *document, name, detail string, kind int, t token.Token, doc string) {
	if name == "" {
		return
	}
	r := tokenRange(d.Text, t)
	if sourceDoc := docForToken(d.Text, t); sourceDoc != "" {
		doc = sourceDoc
	}
	x := symbol{Name: name, Detail: detail, Documentation: doc, Kind: kind, URI: d.URI, Range: r, Selection: r}
	d.Symbols = append(d.Symbols, x)
	if _, ok := d.Definitions[name]; !ok {
		d.Definitions[name] = x
	}
}

// docForToken follows the convention used by Go tooling: consecutive // lines
// immediately above a declaration are presented as its documentation.
func docForToken(text string, t token.Token) string {
	lines := strings.Split(text, "\n")
	idx := t.Line - 2
	if idx < 0 || idx >= len(lines) {
		return ""
	}
	var comments []string
	for idx >= 0 {
		line := strings.TrimSpace(lines[idx])
		if !strings.HasPrefix(line, "//") {
			break
		}
		comments = append(comments, strings.TrimSpace(strings.TrimPrefix(line, "//")))
		idx--
	}
	for i, j := 0, len(comments)-1; i < j; i, j = i+1, j-1 {
		comments[i], comments[j] = comments[j], comments[i]
	}
	return strings.Join(comments, "\n")
}

func (s *Server) get(uri string) *document {
	if d := s.docs[uri]; d != nil {
		return d
	}
	if strings.HasPrefix(uri, "file:") {
		if p, err := uriPath(uri); err == nil {
			if b, err := os.ReadFile(p); err == nil {
				d := analyze(uri, string(b))
				s.docs[uri] = d
				return d
			}
		}
	}
	return nil
}
func uriPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", err
	}
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}
func (s *Server) wordAt(uri string, pos Position) (string, *document) {
	d := s.get(uri)
	if d == nil {
		return "", nil
	}
	lines := strings.Split(d.Text, "\n")
	if pos.Line < 0 || pos.Line >= len(lines) {
		return "", d
	}
	line := []rune(lines[pos.Line])
	if pos.Character > len(line) {
		pos.Character = len(line)
	}
	a := pos.Character
	for a > 0 && isWord(line[a-1]) {
		a--
	}
	b := pos.Character
	for b < len(line) && isWord(line[b]) {
		b++
	}
	return string(line[a:b]), d
}
func isWord(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}
func (s *Server) definition(uri string, pos Position) []Location {
	w, d := s.wordAt(uri, pos)
	if d == nil || w == "" {
		return nil
	}
	var best *symbol
	for i := range d.Symbols {
		x := &d.Symbols[i]
		if x.Name != w || x.Range.Start.Line > pos.Line {
			continue
		}
		if best == nil || x.Range.Start.Line > best.Range.Start.Line ||
			(x.Range.Start.Line == best.Range.Start.Line && x.Range.Start.Character > best.Range.Start.Character) {
			best = x
		}
	}
	if best == nil {
		if x, ok := d.Definitions[w]; ok {
			best = &x
		}
	}
	if best != nil {
		return []Location{{URI: best.URI, Range: best.Selection}}
	}
	return nil
}
func (s *Server) hover(uri string, pos Position) *Hover {
	w, d := s.wordAt(uri, pos)
	if d == nil || w == "" {
		return nil
	}
	if x, ok := d.Definitions[w]; ok {
		return &Hover{Contents: MarkupContent{Kind: "markdown", Value: "```hike\n" + x.Detail + "\n```\n\n" + x.Documentation}, Range: &x.Range}
	}
	return nil
}
func (s *Server) symbols(uri string) []SymbolInformation {
	d := s.get(uri)
	if d == nil {
		return nil
	}
	out := make([]SymbolInformation, 0, len(d.Symbols))
	for _, x := range d.Symbols {
		out = append(out, SymbolInformation{Name: x.Name, Detail: x.Detail, Kind: x.Kind, Location: Location{URI: x.URI, Range: x.Range}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
