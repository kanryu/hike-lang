package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/mod"
	"hikec-go/pkg/parser"
)

type Loader struct {
	rootDir      string
	module       *mod.Module
	visitedFiles map[string]bool
	visitedPkgs  map[string]bool
	verbose      bool
}

func New(rootDir string) *Loader {
	effectiveRoot := rootDir
	if effectiveRoot == "" {
		effectiveRoot = "."
	}

	module, _ := mod.FindModuleRoot(effectiveRoot)
	if module != nil && module.RootDir != "" {
		effectiveRoot = module.RootDir
	}

	return &Loader{
		rootDir:      effectiveRoot,
		module:       module,
		visitedFiles: make(map[string]bool),
		visitedPkgs:  make(map[string]bool),
		verbose:      false,
	}
}

func (l *Loader) SetVerbose(v bool) {
	l.verbose = v
}

func (l *Loader) log(msg string) {
	if l.verbose {
		fmt.Printf("[LOADER] %s\n", msg)
	}
}

func (l *Loader) Load(entryPaths ...string) (*ast.Program, error) {
	combinedProg := &ast.Program{
		Package: "main",
		Imports: []*ast.ImportDecl{},
		Decls:   []ast.Decl{},
	}

	pkgDecls := make(map[string][]ast.Decl)
	pkgImports := make(map[string][]*ast.ImportDecl)

	fileQueue := []string{}
	for _, p := range entryPaths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}

		if l.module == nil || l.module.RootDir == "" {
			l.module, _ = mod.FindModuleRoot(filepath.Dir(absPath))
			if l.module != nil {
				l.rootDir = l.module.RootDir
			}
		}

		fi, err := os.Stat(absPath)
		if err != nil {
			return nil, err
		}

		if fi.IsDir() {
			files, err := l.findHikeFilesInDir(absPath)
			if err != nil {
				return nil, err
			}
			fileQueue = append(fileQueue, files...)
		} else {
			fileQueue = append(fileQueue, absPath)
			dirFiles, _ := l.findHikeFilesInDir(filepath.Dir(absPath))
			for _, df := range dirFiles {
				if df != absPath {
					fileQueue = append(fileQueue, df)
				}
			}
		}
	}

	for len(fileQueue) > 0 {
		curFile := fileQueue[0]
		fileQueue = fileQueue[1:]

		if l.visitedFiles[curFile] {
			continue
		}
		l.visitedFiles[curFile] = true
		l.log(fmt.Sprintf("Parsing file: %s", curFile))

		content, err := os.ReadFile(curFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", curFile, err)
		}

		lx := lexer.New(string(content))
		p := parser.New(lx)
		p.SetVerbose(l.verbose)
		fileProg := p.ParseProgram()

		if len(p.Errors()) > 0 {
			return nil, fmt.Errorf("parse error in %s:\n%s", curFile, strings.Join(p.Errors(), "\n"))
		}

		pkgName := fileProg.Package
		if pkgName == "" {
			pkgName = "main"
		}

		pkgDecls[pkgName] = append(pkgDecls[pkgName], fileProg.Decls...)
		pkgImports[pkgName] = append(pkgImports[pkgName], fileProg.Imports...)

		fileDir := filepath.Dir(curFile)
		for _, imp := range fileProg.Imports {
			if l.module == nil {
				continue
			}
			pkgDir, err := l.module.ResolvePackagePath(fileDir, imp.Path)
			if err == nil && pkgDir != "" && !l.visitedPkgs[pkgDir] {
				l.visitedPkgs[pkgDir] = true
				hikeFiles, err := l.findHikeFilesInDir(pkgDir)
				if err != nil {
					return nil, err
				}
				fileQueue = append(fileQueue, hikeFiles...)
			}
		}
	}

	for pkgName, decls := range pkgDecls {
		if pkgName != "main" {
			mangledDecls := l.manglePackageDecls(pkgName, decls)
			combinedProg.Decls = append(combinedProg.Decls, mangledDecls...)
		} else {
			combinedProg.Decls = append(combinedProg.Decls, decls...)
		}
	}

	for _, imps := range pkgImports {
		combinedProg.Imports = append(combinedProg.Imports, imps...)
	}

	return combinedProg, nil
}

func (l *Loader) findHikeFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", dir, err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".hike") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

func (l *Loader) manglePackageDecls(pkgName string, decls []ast.Decl) []ast.Decl {
	localTypes := make(map[string]bool)
	localFuncs := make(map[string]bool)
	localGlobals := make(map[string]bool)
	localConsts := make(map[string]bool)

	for _, d := range decls {
		switch node := d.(type) {
		case *ast.TypeDecl:
			localTypes[node.Name.Value] = true
		case *ast.FuncDecl:
			if node.Receiver == nil && node.Body != nil {
				localFuncs[node.Name.Value] = true
			}
		case *ast.VarDecl:
			localGlobals[node.Name.Value] = true
		case *ast.ConstDecl:
			localConsts[node.Name.Value] = true
		}
	}

	var rewriteType func(t ast.TypeExpr) ast.TypeExpr
	var rewriteExpr func(e ast.Expression) ast.Expression
	var rewriteStmt func(s ast.Statement) ast.Statement

	rewriteType = func(t ast.TypeExpr) ast.TypeExpr {
		if t == nil {
			return nil
		}
		switch node := t.(type) {
		case *ast.NamedType:
			typeArgs := make([]ast.TypeExpr, len(node.TypeArgs))
			for i, arg := range node.TypeArgs {
				typeArgs[i] = rewriteType(arg)
			}
			if node.Package == nil && localTypes[node.Name.Value] {
				return &ast.NamedType{
					Token:    node.Token,
					Package:  nil,
					Name:     &ast.Identifier{Token: node.Name.Token, Value: pkgName + "_" + node.Name.Value},
					TypeArgs: typeArgs,
				}
			}
			return &ast.NamedType{
				Token:    node.Token,
				Package:  node.Package,
				Name:     node.Name,
				TypeArgs: typeArgs,
			}
		case *ast.PointerType:
			return &ast.PointerType{Token: node.Token, Base: rewriteType(node.Base)}
		case *ast.SliceType:
			return &ast.SliceType{Token: node.Token, Elem: rewriteType(node.Elem)}
		case *ast.ArrayType:
			return &ast.ArrayType{Token: node.Token, Len: node.Len, Elem: rewriteType(node.Elem)}
		case *ast.MapType:
			return &ast.MapType{
				Token: node.Token,
				Key:   rewriteType(node.Key),
				Value: rewriteType(node.Value),
			}
		case *ast.FuncType:
			pts := make([]ast.TypeExpr, len(node.ParamTypes))
			for i, pt := range node.ParamTypes {
				pts[i] = rewriteType(pt)
			}
			rts := make([]ast.TypeExpr, len(node.ReturnTypes))
			for i, rt := range node.ReturnTypes {
				rts[i] = rewriteType(rt)
			}
			return &ast.FuncType{Token: node.Token, ParamTypes: pts, ReturnTypes: rts, IsVariadic: node.IsVariadic}
		case *ast.InterfaceType:
			methods := make([]*ast.MethodSig, len(node.Methods))
			for i, m := range node.Methods {
				pts := make([]ast.TypeExpr, len(m.ParamTypes))
				for j, pt := range m.ParamTypes {
					pts[j] = rewriteType(pt)
				}
				rts := make([]ast.TypeExpr, len(m.ReturnTypes))
				for j, rt := range m.ReturnTypes {
					rts[j] = rewriteType(rt)
				}
				methods[i] = &ast.MethodSig{
					Token:       m.Token,
					Name:        m.Name,
					ParamTypes:  pts,
					ReturnTypes: rts,
				}
			}
			return &ast.InterfaceType{Token: node.Token, Methods: methods}
		}
		return t
	}

	rewriteExpr = func(e ast.Expression) ast.Expression {
		if e == nil {
			return nil
		}
		switch node := e.(type) {
		case *ast.Identifier:
			switch node.Value {
			case "int", "byte", "bool", "float32", "float64", "float", "string", "void", "any", "error",
				"true", "false", "nil", "make", "len", "cap", "append", "delete":
				return node
			}
			if localFuncs[node.Value] || localGlobals[node.Value] || localConsts[node.Value] || localTypes[node.Value] {
				return &ast.Identifier{Token: node.Token, Value: pkgName + "_" + node.Value}
			}
			return node
		case *ast.BinaryExpr:
			return &ast.BinaryExpr{
				Token:    node.Token,
				Operator: node.Operator,
				Left:     rewriteExpr(node.Left),
				Right:    rewriteExpr(node.Right),
			}
		case *ast.PrefixExpr:
			return &ast.PrefixExpr{
				Token:    node.Token,
				Operator: node.Operator,
				Right:    rewriteExpr(node.Right),
			}
		case *ast.CallExpr:
			args := make([]ast.Expression, len(node.Args))
			for i, arg := range node.Args {
				args[i] = rewriteExpr(arg)
			}
			return &ast.CallExpr{
				Token:       node.Token,
				Function:    rewriteExpr(node.Function),
				Args:        args,
				HasEllipsis: node.HasEllipsis,
			}
		case *ast.MemberExpr:
			return &ast.MemberExpr{
				Token:  node.Token,
				Object: rewriteExpr(node.Object),
				Field:  node.Field,
			}
		case *ast.IndexExpr:
			return &ast.IndexExpr{
				Token: node.Token,
				Left:  rewriteExpr(node.Left),
				Index: rewriteExpr(node.Index),
			}
		case *ast.GenericInstExpr:
			args := make([]ast.TypeExpr, len(node.TypeArgs))
			for i, arg := range node.TypeArgs {
				args[i] = rewriteType(arg)
			}
			return &ast.GenericInstExpr{
				Token:    node.Token,
				Left:     rewriteExpr(node.Left),
				TypeArgs: args,
			}
		case *ast.SliceExpr:
			return &ast.SliceExpr{
				Token: node.Token,
				Left:  rewriteExpr(node.Left),
				Low:   rewriteExpr(node.Low),
				High:  rewriteExpr(node.High),
			}
		case *ast.StructLiteral:
			t := rewriteType(node.Type)
			namedT, _ := t.(*ast.NamedType)
			fields := make([]*ast.StructFieldValue, len(node.Fields))
			for i, f := range node.Fields {
				fields[i] = &ast.StructFieldValue{
					Name:  f.Name,
					Value: rewriteExpr(f.Value),
				}
			}
			return &ast.StructLiteral{
				Token:  node.Token,
				Type:   namedT,
				Fields: fields,
			}
		case *ast.ArrayLiteral:
			elements := make([]ast.Expression, len(node.Elements))
			for i, el := range node.Elements {
				elements[i] = rewriteExpr(el)
			}
			return &ast.ArrayLiteral{
				Token:    node.Token,
				Type:     rewriteType(node.Type).(*ast.ArrayType),
				Elements: elements,
			}
		case *ast.SliceLiteral:
			elements := make([]ast.Expression, len(node.Elements))
			for i, el := range node.Elements {
				elements[i] = rewriteExpr(el)
			}
			return &ast.SliceLiteral{
				Token:    node.Token,
				Type:     rewriteType(node.Type).(*ast.SliceType),
				Elements: elements,
			}
		case *ast.TypeAssertExpr:
			return &ast.TypeAssertExpr{
				Token:  node.Token,
				Expr:   rewriteExpr(node.Expr),
				Target: rewriteType(node.Target),
			}
		case *ast.FuncLit:
			params := make([]*ast.ParamDecl, len(node.Params))
			for i, p := range node.Params {
				params[i] = &ast.ParamDecl{
					Token:     p.Token,
					Name:      p.Name,
					Type:      rewriteType(p.Type),
					IsEscaped: p.IsEscaped,
				}
			}
			rts := make([]ast.TypeExpr, len(node.ReturnTypes))
			for i, rt := range node.ReturnTypes {
				rts[i] = rewriteType(rt)
			}
			var body *ast.BlockStmt = nil
			if node.Body != nil {
				body = rewriteStmt(node.Body).(*ast.BlockStmt)
			}
			return &ast.FuncLit{
				Token:       node.Token,
				Params:      params,
				ReturnTypes: rts,
				Body:        body,
				IsVariadic:  node.IsVariadic,
			}
		}
		return e
	}

	rewriteStmt = func(s ast.Statement) ast.Statement {
		if s == nil {
			return nil
		}
		switch node := s.(type) {
		case *ast.ExprStmt:
			return &ast.ExprStmt{Token: node.Token, Expr: rewriteExpr(node.Expr)}
		case *ast.VarDecl:
			return &ast.VarDecl{
				Token:     node.Token,
				Name:      node.Name,
				Type:      rewriteType(node.Type),
				Value:     rewriteExpr(node.Value),
				IsEscaped: node.IsEscaped,
			}
		case *ast.AssignStmt:
			lefts := make([]ast.Expression, len(node.Left))
			for i, l := range node.Left {
				lefts[i] = rewriteExpr(l)
			}
			rights := make([]ast.Expression, len(node.Right))
			for i, r := range node.Right {
				rights[i] = rewriteExpr(r)
			}
			return &ast.AssignStmt{
				Token: node.Token,
				Left:  lefts,
				Right: rights,
				Type:  rewriteType(node.Type),
			}
		case *ast.ReturnStmt:
			vals := make([]ast.Expression, len(node.Values))
			for i, v := range node.Values {
				vals[i] = rewriteExpr(v)
			}
			return &ast.ReturnStmt{Token: node.Token, Values: vals}
		case *ast.DeferStmt:
			var call *ast.CallExpr = nil
			if node.Call != nil {
				call = rewriteExpr(node.Call).(*ast.CallExpr)
			}
			return &ast.DeferStmt{Token: node.Token, Call: call}
		case *ast.BlockStmt:
			stmts := make([]ast.Statement, len(node.Statements))
			for i, st := range node.Statements {
				stmts[i] = rewriteStmt(st)
			}
			return &ast.BlockStmt{Token: node.Token, Statements: stmts}
		case *ast.IfStmt:
			return &ast.IfStmt{
				Token:       node.Token,
				Init:        rewriteStmt(node.Init),
				Condition:   rewriteExpr(node.Condition),
				Consequence: rewriteStmt(node.Consequence).(*ast.BlockStmt),
				Alternative: rewriteStmt(node.Alternative),
			}
		case *ast.ForStmt:
			return &ast.ForStmt{
				Token: node.Token,
				Init:  rewriteStmt(node.Init),
				Cond:  rewriteExpr(node.Cond),
				Post:  rewriteStmt(node.Post),
				Body:  rewriteStmt(node.Body).(*ast.BlockStmt),
			}
		case *ast.ForRangeStmt:
			return &ast.ForRangeStmt{
				Token: node.Token,
				Key:   rewriteExpr(node.Key),
				Value: rewriteExpr(node.Value),
				X:     rewriteExpr(node.X),
				Body:  rewriteStmt(node.Body).(*ast.BlockStmt),
			}
		case *ast.SwitchStmt:
			cases := make([]*ast.CaseClause, len(node.Cases))
			for i, cc := range node.Cases {
				vals := make([]ast.Expression, len(cc.Values))
				for j, v := range cc.Values {
					vals[j] = rewriteExpr(v)
				}
				body := make([]ast.Statement, len(cc.Body))
				for j, bs := range cc.Body {
					body[j] = rewriteStmt(bs)
				}
				cases[i] = &ast.CaseClause{Token: cc.Token, Values: vals, Body: body}
			}
			return &ast.SwitchStmt{
				Token: node.Token,
				Init:  rewriteStmt(node.Init),
				Value: rewriteExpr(node.Value),
				Cases: cases,
			}
		case *ast.TypeSwitchStmt:
			cases := make([]*ast.TypeCaseClause, len(node.Cases))
			for i, cc := range node.Cases {
				types := make([]ast.TypeExpr, len(cc.Types))
				for j, te := range cc.Types {
					types[j] = rewriteType(te)
				}
				body := make([]ast.Statement, len(cc.Body))
				for j, bs := range cc.Body {
					body[j] = rewriteStmt(bs)
				}
				cases[i] = &ast.TypeCaseClause{Token: cc.Token, Types: types, Body: body}
			}
			return &ast.TypeSwitchStmt{
				Token:    node.Token,
				Init:     rewriteStmt(node.Init),
				Variable: node.Variable,
				Expr:     rewriteExpr(node.Expr),
				Cases:    cases,
			}
		}
		return s
	}

	mangledDecls := []ast.Decl{}
	for _, d := range decls {
		switch node := d.(type) {
		case *ast.TypeDecl:
			mangledName := pkgName + "_" + node.Name.Value
			if st, ok := node.Type.(*ast.StructType); ok {
				fields := make([]*ast.FieldDecl, len(st.Fields))
				for i, f := range st.Fields {
					fields[i] = &ast.FieldDecl{
						Token:      f.Token,
						Name:       f.Name,
						Type:       rewriteType(f.Type),
						IsEmbedded: f.IsEmbedded,
					}
				}
				mangledDecls = append(mangledDecls, &ast.TypeDecl{
					Token:      node.Token,
					Name:       &ast.Identifier{Token: node.Name.Token, Value: mangledName},
					TypeParams: node.TypeParams,
					Type:       &ast.StructType{Token: st.Token, Fields: fields},
				})
			} else {
				mangledDecls = append(mangledDecls, &ast.TypeDecl{
					Token:      node.Token,
					Name:       &ast.Identifier{Token: node.Name.Token, Value: mangledName},
					TypeParams: node.TypeParams,
					Type:       rewriteType(node.Type),
				})
			}

		case *ast.FuncDecl:
			var recv *ast.ParamDecl = nil
			if node.Receiver != nil {
				recv = &ast.ParamDecl{
					Token:     node.Receiver.Token,
					Name:      node.Receiver.Name,
					Type:      rewriteType(node.Receiver.Type),
					IsEscaped: node.Receiver.IsEscaped,
				}
			}

			fnName := node.Name.Value
			if recv == nil && node.Body != nil {
				fnName = pkgName + "_" + fnName
			}

			params := make([]*ast.ParamDecl, len(node.Params))
			for i, p := range node.Params {
				params[i] = &ast.ParamDecl{
					Token:     p.Token,
					Name:      p.Name,
					Type:      rewriteType(p.Type),
					IsEscaped: p.IsEscaped,
				}
			}

			rts := make([]ast.TypeExpr, len(node.ReturnTypes))
			for i, rt := range node.ReturnTypes {
				rts[i] = rewriteType(rt)
			}

			var body *ast.BlockStmt = nil
			if node.Body != nil {
				body = rewriteStmt(node.Body).(*ast.BlockStmt)
			}

			mangledDecls = append(mangledDecls, &ast.FuncDecl{
				Token:       node.Token,
				Receiver:    recv,
				Name:        &ast.Identifier{Token: node.Name.Token, Value: fnName},
				TypeParams:  node.TypeParams,
				Params:      params,
				ReturnTypes: rts,
				Body:        body,
				IsVariadic:  node.IsVariadic,
			})

		case *ast.ConstDecl:
			mangledDecls = append(mangledDecls, &ast.ConstDecl{
				Token: node.Token,
				Name:  &ast.Identifier{Token: node.Name.Token, Value: pkgName + "_" + node.Name.Value},
				Value: rewriteExpr(node.Value),
			})

		case *ast.VarDecl:
			mangledDecls = append(mangledDecls, &ast.VarDecl{
				Token:     node.Token,
				Name:      &ast.Identifier{Token: node.Name.Token, Value: pkgName + "_" + node.Name.Value},
				Type:      rewriteType(node.Type),
				Value:     rewriteExpr(node.Value),
				IsEscaped: node.IsEscaped,
			})
		}
	}

	return mangledDecls
}
