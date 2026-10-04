package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/logger"
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
	compileFork  bool
	mu           sync.Mutex
	packageJobs  map[string]*packageJob
	packageOrder []string
}

// packageJob represents an imported package that is being parsed in a
// goroutine.  Closing done publishes every field in the job to the caller.
type packageJob struct {
	dir   string
	done  chan struct{}
	files []loadedFile
	err   error
}

type loadedFile struct {
	path    string
	program *ast.Program
}

func New(rootDir string) *Loader {
	effectiveRoot := rootDir
	if effectiveRoot == "" {
		effectiveRoot = "."
	}

	module, err := mod.FindModuleRoot(effectiveRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[loader] failed to find module root from %s: %v\n", effectiveRoot, err)
	}
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
		packageJobs:  make(map[string]*packageJob),
	}
	if loader.goHikeMode {
		LoadGoHikeBot(loader)
	}
	return loader
}

func (l *Loader) SetVerbose(v bool) {
	l.verbose = v
}

// SetCompileFork enables concurrent parsing of imported packages. The default
// remains the historical sequential loader for reproducible builds and for
// callers that do not opt into the experimental parallel path.
func (l *Loader) SetCompileFork(enabled bool) { l.compileFork = enabled }

// SetGoHikeMode enables the self-hosting compatibility mode. In this mode
// .go files are accepted as Hike source files.
func (l *Loader) SetGoHikeMode(enabled bool) {
	l.goHikeMode = enabled
	if enabled {
		LoadGoHikeBot(l)
	}
}

// loadGoHikeBot loads root-level GoReplace directives. The root hike.mod is deliberately
// consulted only by compatibility-mode loaders, so ordinary Hike builds cannot
// be affected by self-hosting mappings.
func LoadGoHikeBot(l *Loader) {
	if l.module == nil {
		return
	}
	path := filepath.Join(l.module.RootDir, "hike.mod")
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, parts := range scanGoHikeModTokens([]byte(content)) {
		if len(parts) >= 2 && parts[0] == "module" {
			l.module.Name = cloneLoaderString(parts[1])
			continue
		}
		if len(parts) >= 4 && parts[0] == "GoReplace" && parts[2] == "=>" {
			key := cloneLoaderString(parts[1])
			target := cloneLoaderString(parts[3])
			if _, exists := l.module.Replaces[key]; !exists {
				if !filepath.IsAbs(target) {
					target = filepath.Join(l.module.RootDir, target)
				}
				l.module.Replaces[key] = filepath.Clean(target)
			}
		}
	}
}

func scanGoHikeModTokens(data []byte) [][]string {
	lines := make([][]string, 0)
	lineStart := 0
	for lineStart <= len(data) {
		lineEnd := lineStart
		for lineEnd < len(data) && data[lineEnd] != '\n' {
			lineEnd++
		}
		parts := make([]string, 0, 4)
		tokenStart := -1
		for i := lineStart; i <= lineEnd; i++ {
			separator := i == lineEnd || data[i] == ' ' || data[i] == '\t' || data[i] == '\r'
			if !separator {
				if tokenStart < 0 {
					tokenStart = i
				}
				continue
			}
			if tokenStart >= 0 {
				parts = append(parts, cloneLoaderBytes(data, tokenStart, i))
				tokenStart = -1
			}
		}
		if len(parts) > 0 && parts[0] != "#" && !strings.HasPrefix(parts[0], "//") {
			lines = append(lines, parts)
		}
		if lineEnd == len(data) {
			break
		}
		lineStart = lineEnd + 1
	}
	return lines
}

func cloneLoaderString(value string) string {
	return cloneLoaderRange(value, 0, len(value))
}

func cloneLoaderRange(value string, start int, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(value) {
		end = len(value)
	}
	if start >= end {
		return ""
	}
	result := ""
	for i := start; i < end; i++ {
		result = result + string(value[i])
	}
	return result
}

func cloneLoaderBytes(value []byte, start int, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(value) {
		end = len(value)
	}
	result := ""
	for i := start; i < end; i++ {
		result = result + string(value[i])
	}
	return result
}

// SetBuildTags overrides the target tags used by source-file selection.
func (l *Loader) SetBuildTags(tags map[string]bool) {
	l.buildTags = tags
}

func (l *Loader) log(msg string) {
	if l.verbose {
		fmt.Println("[LOADER] " + msg)
	}
}

func (l *Loader) Load(entryPaths ...string) (*ast.Program, error) {
	if l.compileFork {
		return l.loadWithCompileFork(entryPaths...)
	}
	return l.loadSequential(entryPaths...)
}

func (l *Loader) loadSequential(entryPaths ...string) (*ast.Program, error) {
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
	// The module root is discovered while collecting entry files. In Go-Hike
	// mode, load GoReplace mappings only after that discovery so imports in the
	// entry file (including text/template) are resolved before queuing packages.
	if l.goHikeMode {
		LoadGoHikeBot(l)
	}

	for len(fileQueue) > 0 {
		curFile := fileQueue[0]
		fileQueue = fileQueue[1:]

		if l.visitedFiles[curFile] {
			continue
		}
		l.visitedFiles[curFile] = true
		if l.verbose {
			l.log(fmt.Sprintf("Parsing file: %s", curFile))
		}

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
		p.SetGoHikeMode(l.goHikeMode)
		p.SetCompileFork(l.compileFork)
		fileProg := p.ParseProgram()
		parserFunctionCount := 0
		for _, decl := range fileProg.Decls {
			switch decl.(type) {
			case *ast.FuncDecl, *ast.CFuncDecl, *ast.ExternFuncDecl, *ast.JFuncDecl:
				parserFunctionCount++
			}
		}
		logger.LogVerbose("[Verbose] parser file=%s functions=%d\n", curFile, parserFunctionCount)
		if logger.IsVerbose2() {
			logger.LogVerbose2("[Verbose2] parser %s\n", fileProg.String())
		}

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
		logger.LogVerbose("[Verbose] loader collected package=%s file=%s fileDecls=%d packageDecls=%d\n",
			pkgName, curFile, len(fileProg.Decls), len(pkgDecls[pkgName]))
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
				if l.goHikeMode {
					return nil, fmt.Errorf("cannot resolve import %q from %s: module information is unavailable", imp.Path, fileDir)
				}
				continue
			}
			pkgDir, err := l.module.ResolvePackagePath(fileDir, imp.Path)
			if err != nil {
				if l.goHikeMode {
					return nil, fmt.Errorf("cannot resolve import %q from %s: %w", imp.Path, fileDir, err)
				}
				continue
			}
			if pkgDir != "" && !l.visitedPkgs[pkgDir] {
				l.visitedPkgs[pkgDir] = true
				hikeFiles, err := l.findHikeFilesInDir(pkgDir)
				if err != nil {
					return nil, err
				}
				if l.goHikeMode && len(hikeFiles) == 0 {
					return nil, fmt.Errorf("import %q resolved to %s, but no Hike/Go source files were found", imp.Path, pkgDir)
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
		logger.LogVerbose("[Verbose] loader merging package=%s decls=%d combinedBefore=%d\n",
			pkgName, len(decls), len(combinedProg.Decls))
		if pkgName != "main" {
			mangledDecls := l.manglePackageDecls(pkgName, decls)
			combinedProg.Decls = append(combinedProg.Decls, mangledDecls...)
		} else {
			combinedProg.Decls = append(combinedProg.Decls, decls...)
		}
		logger.LogVerbose("[Verbose] loader merged package=%s combinedAfter=%d\n",
			pkgName, len(combinedProg.Decls))
	}

	for _, imps := range pkgImports {
		combinedProg.Imports = append(combinedProg.Imports, imps...)
	}

	return combinedProg, nil
}

// loadWithCompileFork keeps the entry package on the caller's goroutine while
// imported packages are parsed independently. An import starts a job and the
// caller only waits on that job when collecting its result. Jobs may discover
// and start more jobs, so the same mechanism works recursively.
func (l *Loader) loadWithCompileFork(entryPaths ...string) (*ast.Program, error) {
	combinedProg := &ast.Program{Package: "main", Imports: []*ast.ImportDecl{}, Decls: []ast.Decl{}}
	pkgDecls := make(map[string][]ast.Decl)
	pkgImports := make(map[string][]*ast.ImportDecl)
	packageOrder := make([]string, 0)
	seenPackages := make(map[string]bool)

	fileQueue, err := l.collectEntryFiles(entryPaths)
	if err != nil {
		return nil, err
	}
	if l.goHikeMode {
		LoadGoHikeBot(l)
	}

	rootFiles, err := l.parseFilesAndStartImports(fileQueue)
	if err != nil {
		return nil, err
	}
	appendLoadedFiles(rootFiles, pkgDecls, pkgImports, &packageOrder, seenPackages, l)

	// Jobs can append more jobs while their parent is running. Taking one job
	// at a time from the growing list gives every package a completion point
	// without racing with WaitGroup.Add/Wait.
	for index := 0; ; index++ {
		l.mu.Lock()
		if index >= len(l.packageOrder) {
			l.mu.Unlock()
			break
		}
		job := l.packageJobs[l.packageOrder[index]]
		l.mu.Unlock()
		<-job.done
		if job.err != nil {
			return nil, job.err
		}
		appendLoadedFiles(job.files, pkgDecls, pkgImports, &packageOrder, seenPackages, l)
	}

	for _, pkgName := range packageOrder {
		decls := pkgDecls[pkgName]
		if pkgName != "main" {
			combinedProg.Decls = append(combinedProg.Decls, l.manglePackageDecls(pkgName, decls)...)
		} else {
			combinedProg.Decls = append(combinedProg.Decls, decls...)
		}
	}
	for _, pkgName := range packageOrder {
		combinedProg.Imports = append(combinedProg.Imports, pkgImports[pkgName]...)
	}
	return combinedProg, nil
}

func (l *Loader) parseFilesAndStartImports(paths []string) ([]loadedFile, error) {
	files := make([]loadedFile, 0, len(paths))
	for _, path := range paths {
		loaded, err := l.parseFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, loaded)
		if err := l.startImports(filepath.Dir(path), loaded.program.Imports); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (l *Loader) startImports(fromDir string, imports []*ast.ImportDecl) error {
	for _, imp := range imports {
		if l.module == nil {
			if l.goHikeMode {
				return fmt.Errorf("cannot resolve import %q from %s: module information is unavailable", imp.Path, fromDir)
			}
			continue
		}
		l.mu.Lock()
		pkgDir, err := l.module.ResolvePackagePath(fromDir, imp.Path)
		l.mu.Unlock()
		if err != nil || pkgDir == "" {
			if !l.goHikeMode {
				continue
			}
			if err == nil {
				err = fmt.Errorf("resolved package directory is empty")
			}
			return fmt.Errorf("cannot resolve import %q from %s: %w", imp.Path, fromDir, err)
		}
		pkgDir = filepath.Clean(pkgDir)
		l.mu.Lock()
		if _, exists := l.packageJobs[pkgDir]; exists {
			l.mu.Unlock()
			continue
		}
		job := &packageJob{dir: pkgDir, done: make(chan struct{})}
		l.packageJobs[pkgDir] = job
		l.packageOrder = append(l.packageOrder, pkgDir)
		l.mu.Unlock()
		go func() {
			defer close(job.done)
			files, err := l.findHikeFilesInDir(job.dir)
			if err != nil {
				job.err = err
				return
			}
			if l.goHikeMode && len(files) == 0 {
				job.err = fmt.Errorf("import package %s contains no Hike/Go source files", job.dir)
				return
			}
			job.files, job.err = l.parseFilesAndStartImports(files)
		}()
	}
	return nil
}

func (l *Loader) parseFile(path string) (loadedFile, error) {
	l.mu.Lock()
	if l.visitedFiles[path] {
		l.mu.Unlock()
		return loadedFile{path: path, program: &ast.Program{}}, nil
	}
	l.visitedFiles[path] = true
	l.mu.Unlock()
	if l.verbose {
		l.log(fmt.Sprintf("Parsing file: %s", path))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return loadedFile{}, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	if err := validateInlineAsmBuildConstraint(path, content, l.buildTags); err != nil {
		return loadedFile{}, err
	}
	p := parser.New(lexer.New(string(content)))
	p.SetVerbose(l.verbose)
	p.SetGoHikeMode(l.goHikeMode)
	p.SetCompileFork(l.compileFork)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return loadedFile{}, fmt.Errorf("parse error in %s:\n%s", path, strings.Join(p.Errors(), "\n"))
	}
	for _, decl := range program.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			fn.Filename = path
		}
	}
	if l.goHikeMode {
		l.mu.Lock()
		l.applyGoHikeReplacements(string(content))
		l.mu.Unlock()
	}
	return loadedFile{path: path, program: program}, nil
}

func appendLoadedFiles(files []loadedFile, pkgDecls map[string][]ast.Decl, pkgImports map[string][]*ast.ImportDecl, packageOrder *[]string, seen map[string]bool, l *Loader) {
	for _, file := range files {
		if file.program == nil || file.program.Package == "" && len(file.program.Decls) == 0 && len(file.program.Imports) == 0 {
			continue
		}
		pkgName := file.program.Package
		if pkgName == "" {
			pkgName = "main"
		}
		if !seen[pkgName] {
			seen[pkgName] = true
			*packageOrder = append(*packageOrder, pkgName)
		}
		pkgDecls[pkgName] = append(pkgDecls[pkgName], file.program.Decls...)
		pkgImports[pkgName] = append(pkgImports[pkgName], file.program.Imports...)
		logger.LogVerbose("[Verbose] loader collected package=%s file=%s fileDecls=%d packageDecls=%d\n", pkgName, file.path, len(file.program.Decls), len(pkgDecls[pkgName]))
	}
}

func (l *Loader) collectEntryFiles(entryPaths []string) ([]string, error) {
	fileQueue := []string{}
	for _, p := range entryPaths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		l.ensureModuleRoot(absPath)
		// The entry path is the first point at which the module root is
		// guaranteed to be known. Load GoReplace directives before resolving
		// any imports discovered in the entry package.
		if l.goHikeMode {
			LoadGoHikeBot(l)
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
	module, err := mod.FindModuleRoot(filepath.Dir(absPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[loader] failed to find module root from %s: %v\n", filepath.Dir(absPath), err)
	}
	l.module = module
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
		// Match Go package discovery: hidden and underscore-prefixed source
		// files are excluded. This keeps editor/debugging scratch files such
		// as .tmp-logger-probe.go out of Go-Hike self-hosting builds.
		if strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
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
				if _, exists := l.module.Replaces[parts[0]]; !exists {
					l.module.Replaces[parts[0]] = parts[2]
				}
			}
		}
	}
}

// manglePackageDecls はパッケージ内のトップレベル宣言を名前空間修飾（マングル）します。
func (l *Loader) manglePackageDecls(pkgName string, decls []ast.Decl) []ast.Decl {
	var mangled []ast.Decl
	localTypes := make(map[string]bool)
	localConstants := make(map[string]bool)
	for _, decl := range decls {
		if td, ok := decl.(*ast.TypeDecl); ok && td.Name != nil {
			localTypes[td.Name.Value] = true
		}
		if cd, ok := decl.(*ast.ConstDecl); ok && cd.Name != nil {
			localConstants[cd.Name.Value] = true
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
			// Declarations and all references in the body must use the same
			// package-qualified names. Leaving a composite literal such as
			// &Parser{} unqualified allows it to bind to another package's
			// Parser after declarations have been merged.
			qualifyLocalBlock(pkgName, d.Body, localTypes, localConstants)
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
			qualifyLocalTypeExpr(pkgName, d.Type, localTypes)
			qualifyLocalExpr(pkgName, d.Value, localTypes, localConstants)
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
	if typ == nil || isNilTypeExpr(typ) || pkgName == "" || pkgName == "main" {
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

func isNilTypeExpr(typ ast.TypeExpr) bool {
	switch t := typ.(type) {
	case *ast.NamedType:
		return t == nil
	case *ast.PointerType:
		return t == nil
	case *ast.SliceType:
		return t == nil
	case *ast.EllipsisType:
		return t == nil
	case *ast.ArrayType:
		return t == nil
	case *ast.MapType:
		return t == nil
	case *ast.ChanType:
		return t == nil
	case *ast.FutureType:
		return t == nil
	case *ast.FuncType:
		return t == nil
	case *ast.InterfaceType:
		return t == nil
	case *ast.StructType:
		return t == nil
	case *ast.ConstArg:
		return t == nil
	default:
		return false
	}
}

// qualifyLocalBlock applies the same package qualification to type names used
// inside function bodies.  Top-level declarations were already qualified by
// manglePackageDecls, but a composite literal such as &Parser{} used to remain
// unqualified.  Once another package also defined Parser, semantic lookup
// could bind that literal to the wrong package's struct.
func qualifyLocalBlock(pkgName string, block *ast.BlockStmt, localTypes map[string]bool, localConstants map[string]bool) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		qualifyLocalStmt(pkgName, stmt, localTypes, localConstants)
	}
}

func qualifyLocalStmt(pkgName string, stmt ast.Statement, localTypes map[string]bool, localConstants map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		qualifyLocalBlock(pkgName, s, localTypes, localConstants)
	case *ast.ExprStmt:
		qualifyLocalExpr(pkgName, s.Expr, localTypes, localConstants)
	case *ast.AssignStmt:
		for _, expr := range s.Left {
			qualifyLocalExpr(pkgName, expr, localTypes, localConstants)
		}
		for _, expr := range s.Right {
			qualifyLocalExpr(pkgName, expr, localTypes, localConstants)
		}
		qualifyLocalTypeExpr(pkgName, s.Type, localTypes)
	case *ast.VarDecl:
		qualifyLocalTypeExpr(pkgName, s.Type, localTypes)
		qualifyLocalExpr(pkgName, s.Value, localTypes, localConstants)
	case *ast.ReturnStmt:
		for _, expr := range s.Values {
			qualifyLocalExpr(pkgName, expr, localTypes, localConstants)
		}
	case *ast.IfStmt:
		qualifyLocalStmt(pkgName, s.Init, localTypes, localConstants)
		qualifyLocalExpr(pkgName, s.Condition, localTypes, localConstants)
		qualifyLocalBlock(pkgName, s.Consequence, localTypes, localConstants)
		qualifyLocalStmt(pkgName, s.Alternative, localTypes, localConstants)
	case *ast.ForStmt:
		qualifyLocalStmt(pkgName, s.Init, localTypes, localConstants)
		qualifyLocalExpr(pkgName, s.Cond, localTypes, localConstants)
		qualifyLocalStmt(pkgName, s.Post, localTypes, localConstants)
		qualifyLocalBlock(pkgName, s.Body, localTypes, localConstants)
	case *ast.ForRangeStmt:
		qualifyLocalExpr(pkgName, s.Key, localTypes, localConstants)
		qualifyLocalExpr(pkgName, s.Value, localTypes, localConstants)
		qualifyLocalExpr(pkgName, s.X, localTypes, localConstants)
		qualifyLocalBlock(pkgName, s.Body, localTypes, localConstants)
	case *ast.DeferStmt:
		qualifyLocalExpr(pkgName, s.Call, localTypes, localConstants)
	case *ast.LockStmt:
		qualifyLocalBlock(pkgName, s.Body, localTypes, localConstants)
	case *ast.AreaStmt:
		qualifyLocalExpr(pkgName, s.Size, localTypes, localConstants)
		qualifyLocalBlock(pkgName, s.Body, localTypes, localConstants)
	case *ast.SwitchStmt:
		qualifyLocalStmt(pkgName, s.Init, localTypes, localConstants)
		qualifyLocalExpr(pkgName, s.Value, localTypes, localConstants)
		for _, clause := range s.Cases {
			for _, value := range clause.Values {
				qualifyLocalExpr(pkgName, value, localTypes, localConstants)
			}
			for _, child := range clause.Body {
				qualifyLocalStmt(pkgName, child, localTypes, localConstants)
			}
		}
	}
}

func qualifyLocalExpr(pkgName string, expr ast.ASTExpression, localTypes map[string]bool, localConstants map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if pkgName != "main" && e.Value != "_" && localConstants[e.Value] {
			e.Value = pkgName + "_" + e.Value
		}
	case *ast.StructLiteral:
		qualifyLocalTypeExpr(pkgName, e.Type, localTypes)
		for _, field := range e.Fields {
			if field != nil {
				qualifyLocalExpr(pkgName, field.Value, localTypes, localConstants)
			}
		}
	case *ast.ArrayLiteral:
		qualifyLocalTypeExpr(pkgName, e.Type, localTypes)
		for _, value := range e.Elements {
			qualifyLocalExpr(pkgName, value, localTypes, localConstants)
		}
	case *ast.SliceLiteral:
		qualifyLocalTypeExpr(pkgName, e.Type, localTypes)
		for _, value := range e.Elements {
			qualifyLocalExpr(pkgName, value, localTypes, localConstants)
		}
	case *ast.MapLiteral:
		qualifyLocalTypeExpr(pkgName, e.Type, localTypes)
		for _, entry := range e.Entries {
			if entry != nil {
				qualifyLocalExpr(pkgName, entry.Key, localTypes, localConstants)
				qualifyLocalExpr(pkgName, entry.Value, localTypes, localConstants)
			}
		}
	case *ast.CallExpr:
		qualifyLocalExpr(pkgName, e.Function, localTypes, localConstants)
		for _, arg := range e.Args {
			qualifyLocalExpr(pkgName, arg, localTypes, localConstants)
		}
	case *ast.MemberExpr:
		qualifyLocalExpr(pkgName, e.Object, localTypes, localConstants)
	case *ast.IndexExpr:
		qualifyLocalExpr(pkgName, e.Left, localTypes, localConstants)
		qualifyLocalExpr(pkgName, e.Index, localTypes, localConstants)
	case *ast.SliceExpr:
		qualifyLocalExpr(pkgName, e.Left, localTypes, localConstants)
		qualifyLocalExpr(pkgName, e.Low, localTypes, localConstants)
		qualifyLocalExpr(pkgName, e.High, localTypes, localConstants)
	case *ast.BinaryExpr:
		qualifyLocalExpr(pkgName, e.Left, localTypes, localConstants)
		qualifyLocalExpr(pkgName, e.Right, localTypes, localConstants)
	case *ast.PrefixExpr:
		qualifyLocalExpr(pkgName, e.Right, localTypes, localConstants)
	case *ast.ReceiveExpr:
		qualifyLocalExpr(pkgName, e.Expr, localTypes, localConstants)
	case *ast.AsyncExpr:
		qualifyLocalExpr(pkgName, e.Fn, localTypes, localConstants)
	case *ast.GenericInstExpr:
		qualifyLocalExpr(pkgName, e.Left, localTypes, localConstants)
		for _, arg := range e.TypeArgs {
			qualifyLocalTypeExpr(pkgName, arg, localTypes)
		}
	case *ast.TypeAssertExpr:
		qualifyLocalExpr(pkgName, e.Expr, localTypes, localConstants)
		qualifyLocalTypeExpr(pkgName, e.Target, localTypes)
	case *ast.ConstArg:
		qualifyLocalExpr(pkgName, e.Expr, localTypes, localConstants)
	case *ast.InlineAsmExpr:
		for _, operand := range e.Operands {
			qualifyLocalExpr(pkgName, operand, localTypes, localConstants)
		}
	}
}
