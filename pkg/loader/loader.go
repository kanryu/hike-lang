package loader

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

type ModuleInfo struct {
	Name     string
	RootDir  string
	Replaces map[string]string
}

type Loader struct {
	rootDir      string
	moduleInfo   *ModuleInfo
	visitedFiles map[string]bool
	visitedPkgs  map[string]bool
	verbose      bool
}

func findModuleRoot(startDir string) string {
	absDir, err := filepath.Abs(startDir)
	if err != nil {
		cwd, _ := os.Getwd()
		return cwd
	}

	cur := absDir
	for {
		modFile := filepath.Join(cur, "hike.mod")
		if fi, err := os.Stat(modFile); err == nil && !fi.IsDir() {
			return cur
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	cwd, _ := os.Getwd()
	return cwd
}

func readModuleInfo(startDir string) *ModuleInfo {
	rootDir := findModuleRoot(startDir)
	modFile := filepath.Join(rootDir, "hike.mod")
	modName := filepath.Base(rootDir)
	replaces := make(map[string]string)

	if f, err := os.Open(modFile); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "module ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					modName = fields[1]
				}
			} else if strings.HasPrefix(line, "replace ") {
				// replace std/json => ../../std/json
				fields := strings.Fields(line)
				if len(fields) >= 4 && fields[2] == "=>" {
					replaces[fields[1]] = fields[3]
				} else if len(fields) >= 3 {
					replaces[fields[1]] = fields[2]
				}
			}
		}
	}

	return &ModuleInfo{
		Name:     modName,
		RootDir:  rootDir,
		Replaces: replaces,
	}
}

func New(rootDir string) *Loader {
	modInfo := readModuleInfo(rootDir)
	effectiveRoot := rootDir
	if effectiveRoot == "" || effectiveRoot == "." {
		effectiveRoot = modInfo.RootDir
	}

	return &Loader{
		rootDir:      effectiveRoot,
		moduleInfo:   modInfo,
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

		if l.moduleInfo == nil || l.moduleInfo.RootDir == "" {
			l.moduleInfo = readModuleInfo(filepath.Dir(absPath))
			l.rootDir = l.moduleInfo.RootDir
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
			pkgDir := l.resolveImportDir(imp.Path, fileDir)
			if pkgDir != "" && !l.visitedPkgs[pkgDir] {
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

func (l *Loader) resolveImportDir(importPath string, currentFileDir string) string {
	// 1. 相対パス指定 (./ または ../)
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		return filepath.Clean(filepath.Join(currentFileDir, importPath))
	}

	// 2. hike.mod の replace ディレクティブ判定
	if l.moduleInfo != nil && l.moduleInfo.Replaces != nil {
		for fromMod, targetRel := range l.moduleInfo.Replaces {
			if importPath == fromMod || strings.HasPrefix(importPath, fromMod+"/") {
				relSub := strings.TrimPrefix(importPath, fromMod)
				relSub = strings.TrimPrefix(relSub, "/")
				targetPath := filepath.Clean(filepath.Join(l.moduleInfo.RootDir, targetRel, relSub))
				if fi, err := os.Stat(targetPath); err == nil && fi.IsDir() {
					return targetPath
				}
			}
		}
	}

	cleanPath := importPath
	if l.moduleInfo != nil && l.moduleInfo.Name != "" && strings.HasPrefix(cleanPath, l.moduleInfo.Name+"/") {
		cleanPath = strings.TrimPrefix(cleanPath, l.moduleInfo.Name+"/")
	}

	// 3. モジュールルート直下探索
	if l.moduleInfo != nil && l.moduleInfo.RootDir != "" {
		candidateMod := filepath.Join(l.moduleInfo.RootDir, cleanPath)
		if fi, err := os.Stat(candidateMod); err == nil && fi.IsDir() {
			return candidateMod
		}
	}

	// 4. Loader rootDir 探索
	if l.rootDir != "" {
		candidateRoot := filepath.Join(l.rootDir, cleanPath)
		if fi, err := os.Stat(candidateRoot); err == nil && fi.IsDir() {
			return candidateRoot
		}
	}

	// 5. コンパイラ隣接探索
	if exePath, err := os.Executable(); err == nil {
		exeStdCandidate := filepath.Join(filepath.Dir(exePath), "..", cleanPath)
		if fi, err := os.Stat(exeStdCandidate); err == nil && fi.IsDir() {
			return exeStdCandidate
		}
	}

	// 6. カレントファイルディレクトリ探索
	candidateCurr := filepath.Join(currentFileDir, importPath)
	if fi, err := os.Stat(candidateCurr); err == nil && fi.IsDir() {
		return candidateCurr
	}

	return ""
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
			if node.Package == nil && localTypes[node.Name.Value] {
				return &ast.NamedType{
					Token:   node.Token,
					Package: nil,
					Name:    &ast.Identifier{Token: node.Name.Token, Value: pkgName + "_" + node.Name.Value},
				}
			}
			return node
		case *ast.PointerType:
			return &ast.PointerType{Token: node.Token, Base: rewriteType(node.Base)}
		case *ast.SliceType:
			return &ast.SliceType{Token: node.Token, Elem: rewriteType(node.Elem)}
		case *ast.ArrayType:
			return &ast.ArrayType{Token: node.Token, Len: node.Len, Elem: rewriteType(node.Elem)}
		case *ast.FuncType:
			pts := []ast.TypeExpr{}
			for _, pt := range node.ParamTypes {
				pts = append(pts, rewriteType(pt))
			}
			rts := []ast.TypeExpr{}
			for _, rt := range node.ReturnTypes {
				rts = append(rts, rewriteType(rt))
			}
			return &ast.FuncType{Token: node.Token, ParamTypes: pts, ReturnTypes: rts}
		case *ast.InterfaceType:
			methods := []*ast.MethodSig{}
			for _, m := range node.Methods {
				pts := []ast.TypeExpr{}
				for _, pt := range m.ParamTypes {
					pts = append(pts, rewriteType(pt))
				}
				rts := []ast.TypeExpr{}
				for _, rt := range m.ReturnTypes {
					rts = append(rts, rewriteType(rt))
				}
				methods = append(methods, &ast.MethodSig{
					Token:       m.Token,
					Name:        m.Name,
					ParamTypes:  pts,
					ReturnTypes: rts,
				})
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
			// 組み込み基本型およびキーワードはリライトしない
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
			args := []ast.Expression{}
			for _, arg := range node.Args {
				args = append(args, rewriteExpr(arg))
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
			fields := []*ast.StructFieldValue{}
			for _, f := range node.Fields {
				fields = append(fields, &ast.StructFieldValue{
					Name:  f.Name,
					Value: rewriteExpr(f.Value),
				})
			}
			return &ast.StructLiteral{
				Token:  node.Token,
				Type:   namedT,
				Fields: fields,
			}
		case *ast.TypeAssertExpr:
			return &ast.TypeAssertExpr{
				Token:  node.Token,
				Expr:   rewriteExpr(node.Expr),
				Target: rewriteType(node.Target),
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
				Token: node.Token,
				Name:  node.Name,
				Type:  rewriteType(node.Type),
				Value: rewriteExpr(node.Value),
			}
		case *ast.AssignStmt:
			lefts := []ast.Expression{}
			for _, l := range node.Left {
				lefts = append(lefts, rewriteExpr(l))
			}
			rights := []ast.Expression{}
			for _, r := range node.Right {
				rights = append(rights, rewriteExpr(r))
			}
			return &ast.AssignStmt{
				Token: node.Token,
				Left:  lefts,
				Right: rights,
				Type:  rewriteType(node.Type),
			}
		case *ast.ReturnStmt:
			vals := []ast.Expression{}
			for _, v := range node.Values {
				vals = append(vals, rewriteExpr(v))
			}
			return &ast.ReturnStmt{Token: node.Token, Values: vals}
		case *ast.BlockStmt:
			stmts := []ast.Statement{}
			for _, st := range node.Statements {
				stmts = append(stmts, rewriteStmt(st))
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
			cases := []*ast.CaseClause{}
			for _, cc := range node.Cases {
				vals := []ast.Expression{}
				for _, v := range cc.Values {
					vals = append(vals, rewriteExpr(v))
				}
				body := []ast.Statement{}
				for _, bs := range cc.Body {
					body = append(body, rewriteStmt(bs))
				}
				cases = append(cases, &ast.CaseClause{Token: cc.Token, Values: vals, Body: body})
			}
			return &ast.SwitchStmt{
				Token: node.Token,
				Init:  rewriteStmt(node.Init),
				Value: rewriteExpr(node.Value),
				Cases: cases,
			}
		case *ast.TypeSwitchStmt:
			cases := []*ast.TypeCaseClause{}
			for _, cc := range node.Cases {
				types := []ast.TypeExpr{}
				for _, te := range cc.Types {
					types = append(types, rewriteType(te))
				}
				body := []ast.Statement{}
				for _, bs := range cc.Body {
					body = append(body, rewriteStmt(bs))
				}
				cases = append(cases, &ast.TypeCaseClause{Token: cc.Token, Types: types, Body: body})
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
				fields := []*ast.FieldDecl{}
				for _, f := range st.Fields {
					fields = append(fields, &ast.FieldDecl{
						Token:      f.Token,
						Name:       f.Name,
						Type:       rewriteType(f.Type),
						IsEmbedded: f.IsEmbedded,
					})
				}
				mangledDecls = append(mangledDecls, &ast.TypeDecl{
					Token: node.Token,
					Name:  &ast.Identifier{Token: node.Name.Token, Value: mangledName},
					Type:  &ast.StructType{Token: st.Token, Fields: fields},
				})
			} else {
				mangledDecls = append(mangledDecls, &ast.TypeDecl{
					Token: node.Token,
					Name:  &ast.Identifier{Token: node.Name.Token, Value: mangledName},
					Type:  rewriteType(node.Type),
				})
			}

		case *ast.FuncDecl:
			var recv *ast.ParamDecl = nil
			if node.Receiver != nil {
				recv = &ast.ParamDecl{
					Token: node.Receiver.Token,
					Name:  node.Receiver.Name,
					Type:  rewriteType(node.Receiver.Type),
				}
			}

			fnName := node.Name.Value
			if recv == nil && node.Body != nil {
				fnName = pkgName + "_" + fnName
			}

			params := []*ast.ParamDecl{}
			for _, p := range node.Params {
				params = append(params, &ast.ParamDecl{
					Token: p.Token,
					Name:  p.Name,
					Type:  rewriteType(p.Type),
				})
			}

			rts := []ast.TypeExpr{}
			for _, rt := range node.ReturnTypes {
				rts = append(rts, rewriteType(rt))
			}

			var body *ast.BlockStmt = nil
			if node.Body != nil {
				body = rewriteStmt(node.Body).(*ast.BlockStmt)
			}

			mangledDecls = append(mangledDecls, &ast.FuncDecl{
				Token:       node.Token,
				Receiver:    recv,
				Name:        &ast.Identifier{Token: node.Name.Token, Value: fnName},
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
				Token: node.Token,
				Name:  &ast.Identifier{Token: node.Name.Token, Value: pkgName + "_" + node.Name.Value},
				Type:  rewriteType(node.Type),
				Value: rewriteExpr(node.Value),
			})
		}
	}

	return mangledDecls
}
