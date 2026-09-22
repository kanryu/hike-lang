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
	buildTags    map[string]bool
	goHikeMode   bool
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

	loader := &Loader{
		rootDir:      effectiveRoot,
		module:       module,
		visitedFiles: make(map[string]bool),
		visitedPkgs:  make(map[string]bool),
		buildTags:    defaultBuildTags(),
		verbose:      false,
	}
	if loader.goHikeMode {
		loader.loadGoHikeBot()
	}
	return loader
}

func (l *Loader) SetVerbose(v bool) {
	l.verbose = v
}

// SetGoHikeMode enables the self-hosting compatibility mode. In this mode
// .go files are accepted as Hike source files.
func (l *Loader) SetGoHikeMode(enabled bool) {
	l.goHikeMode = enabled
	if enabled {
		l.loadGoHikeBot()
	}
}

// loadGoHikeBot loads root-level GoReplace directives. The root hike.mod is deliberately
// consulted only by compatibility-mode loaders, so ordinary Hike builds cannot
// be affected by self-hosting mappings.
func (l *Loader) loadGoHikeBot() {
	if l.module == nil {
		return
	}
	path := filepath.Join(l.module.RootDir, "hike.mod")
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !strings.HasPrefix(line, "GoReplace ") {
			continue
		}
		parts := strings.Fields(strings.TrimPrefix(line, "GoReplace "))
		if len(parts) >= 3 && parts[1] == "=>" {
			l.module.Replaces[parts[0]] = parts[2]
		}
	}
}

// SetBuildTags overrides the target tags used by source-file selection.
func (l *Loader) SetBuildTags(tags map[string]bool) {
	l.buildTags = tags
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
	packageOrder := make([]string, 0)
	seenPackages := make(map[string]bool)

	fileQueue, err := l.collectEntryFiles(entryPaths)
	if err != nil {
		return nil, err
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
		if err := validateInlineAsmBuildConstraint(curFile, content, l.buildTags); err != nil {
			return nil, err
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
		if !seenPackages[pkgName] {
			seenPackages[pkgName] = true
			packageOrder = append(packageOrder, pkgName)
		}

		pkgDecls[pkgName] = append(pkgDecls[pkgName], fileProg.Decls...)
		for _, decl := range fileProg.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				fn.Filename = curFile
			}
		}
		pkgImports[pkgName] = append(pkgImports[pkgName], fileProg.Imports...)

		fileDir := filepath.Dir(curFile)
		if l.goHikeMode {
			l.applyGoHikeReplacements(string(content))
		}
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

	// Keep package discovery order.  The semantic pass also keeps a short,
	// unqualified alias for declarations in a merged program.  Iterating the
	// map here made that alias depend on Go's randomized map order, so an
	// unqualified type could resolve to the wrong imported package.
	for _, pkgName := range packageOrder {
		decls := pkgDecls[pkgName]
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

func (l *Loader) collectEntryFiles(entryPaths []string) ([]string, error) {
	fileQueue := []string{}
	for _, p := range entryPaths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		l.ensureModuleRoot(absPath)

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
			continue
		}
		if !l.fileAllowed(absPath) {
			continue
		}
		fileQueue = append(fileQueue, absPath)
		dirFiles, _ := l.findHikeFilesInDir(filepath.Dir(absPath))
		for _, df := range dirFiles {
			if df != absPath {
				fileQueue = append(fileQueue, df)
			}
		}
	}
	return fileQueue, nil
}

func (l *Loader) ensureModuleRoot(absPath string) {
	if l.module != nil && l.module.RootDir != "" {
		return
	}
	l.module, _ = mod.FindModuleRoot(filepath.Dir(absPath))
	if l.module != nil {
		l.rootDir = l.module.RootDir
	}
}

func (l *Loader) findHikeFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", dir, err)
	}

	var files []string
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		isSource := strings.HasSuffix(entry.Name(), ".hike")
		if l.goHikeMode {
			isSource = isSource || strings.HasSuffix(entry.Name(), ".go")
		}
		if !entry.IsDir() && isSource && l.fileAllowed(path) {
			files = append(files, path)
		}
	}
	return files, nil
}

// applyGoHikeReplacements reads replacement directives embedded in Go source.
// Both forms are accepted for generated/self-hosted sources.
func (l *Loader) applyGoHikeReplacements(content string) {
	if l.module == nil {
		return
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		for _, prefix := range []string{"//go:replace ", "//hike:go-replace "} {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			parts := strings.Fields(strings.TrimPrefix(line, prefix))
			if len(parts) >= 3 && parts[1] == "=>" {
				l.module.Replaces[parts[0]] = parts[2]
			}
		}
	}
}

// manglePackageDecls はパッケージ内のトップレベル宣言を名前空間修飾（マングル）します。
func (l *Loader) manglePackageDecls(pkgName string, decls []ast.Decl) []ast.Decl {
	var mangled []ast.Decl
	localTypes := make(map[string]bool)
	for _, decl := range decls {
		if td, ok := decl.(*ast.TypeDecl); ok && td.Name != nil {
			localTypes[td.Name.Value] = true
		}
	}

	for _, decl := range decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			qualifyLocalReceiver(pkgName, d.Receiver, localTypes)
			for _, param := range d.Params {
				qualifyLocalTypeExpr(pkgName, param.Type, localTypes)
			}
			for _, ret := range d.ReturnTypes {
				qualifyLocalTypeExpr(pkgName, ret, localTypes)
			}
			for _, param := range d.TypeParams {
				qualifyLocalTypeExpr(pkgName, param.Constraint, localTypes)
			}
			// 外部 C 関数（Body == nil の extern 宣言）は C ライブラリのシンボルであるためマングルしない。
			// 実体を持つ関数（Body != nil）のみ、main パッケージ以外でパッケージ名を付与してマングルする。
			if d.Body != nil && (pkgName != "main" || d.Name.Value != "main") {
				d.Name.Value = pkgName + "_" + d.Name.Value
			}
			mangled = append(mangled, d)

		case *ast.CFuncDecl:
			// cfunc は C リンケージおよび外部スタブと直結するため、パッケージ名によるマングルを行わない
			mangled = append(mangled, d)

		case *ast.TypeDecl:
			qualifyLocalTypeExpr(pkgName, d.Type, localTypes)
			for _, param := range d.TypeParams {
				qualifyLocalTypeExpr(pkgName, param.Constraint, localTypes)
			}
			if pkgName != "main" {
				d.Name.Value = pkgName + "_" + d.Name.Value
			}
			mangled = append(mangled, d)

		case *ast.VarDecl:
			if pkgName != "main" {
				d.Name.Value = pkgName + "_" + d.Name.Value
			}
			mangled = append(mangled, d)

		case *ast.MemoryBlockDecl:
			if pkgName != "main" {
				for _, variable := range d.Vars {
					variable.Name.Value = pkgName + "_" + variable.Name.Value
				}
			}
			mangled = append(mangled, d)

		case *ast.ConstDecl:
			if pkgName != "main" {
				d.Name.Value = pkgName + "_" + d.Name.Value
			}
			mangled = append(mangled, d)

		default:
			mangled = append(mangled, d)
		}
	}

	return mangled
}

// qualifyLocalReceiver keeps methods attached to the package-local type after
// declarations are namespace-mangled.  Without this, a receiver such as
// *StructType remains unqualified in the merged program and can be resolved to
// an imported ast.StructType with the same short name.
func qualifyLocalReceiver(pkgName string, receiver *ast.ParamDecl, localTypes map[string]bool) {
	if receiver == nil || receiver.Type == nil || pkgName == "" || pkgName == "main" {
		return
	}
	var named *ast.NamedType
	switch typ := receiver.Type.(type) {
	case *ast.NamedType:
		named = typ
	case *ast.PointerType:
		named, _ = typ.Base.(*ast.NamedType)
	}
	if named == nil || named.Package != nil || named.Name == nil || !localTypes[named.Name.Value] {
		return
	}
	named.Name.Value = pkgName + "_" + named.Name.Value
}

func qualifyLocalTypeExpr(pkgName string, typ ast.TypeExpr, localTypes map[string]bool) {
	if typ == nil || pkgName == "" || pkgName == "main" {
		return
	}
	switch t := typ.(type) {
	case *ast.NamedType:
		if t.Package == nil && t.Name != nil && localTypes[t.Name.Value] {
			t.Name.Value = pkgName + "_" + t.Name.Value
		}
		for _, arg := range t.TypeArgs {
			qualifyLocalTypeExpr(pkgName, arg, localTypes)
		}
	case *ast.PointerType:
		qualifyLocalTypeExpr(pkgName, t.Base, localTypes)
	case *ast.SliceType:
		qualifyLocalTypeExpr(pkgName, t.Elem, localTypes)
	case *ast.EllipsisType:
		qualifyLocalTypeExpr(pkgName, t.Elem, localTypes)
	case *ast.ArrayType:
		qualifyLocalTypeExpr(pkgName, t.Elem, localTypes)
	case *ast.MapType:
		qualifyLocalTypeExpr(pkgName, t.Key, localTypes)
		qualifyLocalTypeExpr(pkgName, t.Value, localTypes)
	case *ast.ChanType:
		qualifyLocalTypeExpr(pkgName, t.Elem, localTypes)
	case *ast.FutureType:
		for _, ret := range t.ReturnTypes {
			qualifyLocalTypeExpr(pkgName, ret, localTypes)
		}
	case *ast.FuncType:
		for _, param := range t.ParamTypes {
			qualifyLocalTypeExpr(pkgName, param, localTypes)
		}
		for _, ret := range t.ReturnTypes {
			qualifyLocalTypeExpr(pkgName, ret, localTypes)
		}
	case *ast.InterfaceType:
		for _, method := range t.Methods {
			for _, param := range method.ParamTypes {
				qualifyLocalTypeExpr(pkgName, param, localTypes)
			}
			for _, ret := range method.ReturnTypes {
				qualifyLocalTypeExpr(pkgName, ret, localTypes)
			}
		}
		for _, embedded := range t.Embedded {
			qualifyLocalTypeExpr(pkgName, embedded, localTypes)
		}
	case *ast.StructType:
		for _, field := range t.Fields {
			qualifyLocalTypeExpr(pkgName, field.Type, localTypes)
		}
	}
}
