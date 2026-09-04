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

// manglePackageDecls はパッケージ内のトップレベル宣言を名前空間修飾（マングル）します。
func (l *Loader) manglePackageDecls(pkgName string, decls []ast.Decl) []ast.Decl {
	var mangled []ast.Decl

	for _, decl := range decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
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
			if pkgName != "main" {
				d.Name.Value = pkgName + "_" + d.Name.Value
			}
			mangled = append(mangled, d)

		case *ast.VarDecl:
			if pkgName != "main" {
				d.Name.Value = pkgName + "_" + d.Name.Value
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
