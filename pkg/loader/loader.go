package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
	"hikec-go/pkg/parser"
)

type Loader struct {
	rootDir     string
	searchPaths []string
	loadedFiles map[string]*ast.Program
	loading     map[string]bool
	fileOrder   []string
}

func New(rootDir string, extraSearchPaths ...string) *Loader {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		absRoot = rootDir
	}

	searchPaths := []string{absRoot}
	for _, p := range extraSearchPaths {
		absP, err := filepath.Abs(p)
		if err == nil {
			searchPaths = append(searchPaths, absP)
		}
	}

	return &Loader{
		rootDir:     absRoot,
		searchPaths: searchPaths,
		loadedFiles: make(map[string]*ast.Program),
		loading:     make(map[string]bool),
		fileOrder:   make([]string, 0),
	}
}

func (l *Loader) LoadProgram(entryFile string) ([]*ast.Program, error) {
	absEntry, err := filepath.Abs(entryFile)
	if err != nil {
		return nil, fmt.Errorf("invalid entry file path: %w", err)
	}

	if err := l.loadFileRecursive(absEntry); err != nil {
		return nil, err
	}

	var result []*ast.Program
	for _, path := range l.fileOrder {
		if prog, ok := l.loadedFiles[path]; ok {
			result = append(result, prog)
		}
	}

	return result, nil
}

func (l *Loader) loadFileRecursive(absPath string) error {
	if _, ok := l.loadedFiles[absPath]; ok {
		return nil
	}

	if l.loading[absPath] {
		return fmt.Errorf("circular import detected at file: %s", absPath)
	}
	l.loading[absPath] = true
	defer func() { l.loading[absPath] = false }()

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", absPath, err)
	}

	lex := lexer.New(string(content))
	p := parser.New(lex)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return fmt.Errorf("parse error in %s: %s", absPath, strings.Join(p.Errors(), "; "))
	}

	for _, imp := range prog.Imports {
		importPath := strings.Trim(imp.Path, "\"")
		resolvedFiles, err := l.resolveImport(importPath)
		if err != nil {
			return fmt.Errorf("in %s: %w", absPath, err)
		}

		for _, targetFile := range resolvedFiles {
			if err := l.loadFileRecursive(targetFile); err != nil {
				return err
			}
		}
	}

	l.loadedFiles[absPath] = prog
	l.fileOrder = append(l.fileOrder, absPath)
	return nil
}

func (l *Loader) resolveImport(importPath string) ([]string, error) {
	for _, searchRoot := range l.searchPaths {
		candidateDir := filepath.Join(searchRoot, importPath)

		// 1. パッケージディレクトリの場合（例: std/fmt 内の全 .hike ファイルを収集）
		info, err := os.Stat(candidateDir)
		if err == nil && info.IsDir() {
			entries, err := os.ReadDir(candidateDir)
			if err != nil {
				return nil, err
			}

			var files []string
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".hike") {
					absF, _ := filepath.Abs(filepath.Join(candidateDir, entry.Name()))
					files = append(files, absF)
				}
			}
			if len(files) > 0 {
				return files, nil
			}
		}

		// 2. 単一ファイルの場合（例: std/fmt.hike）
		candidateFile := filepath.Join(searchRoot, importPath+".hike")
		info, err = os.Stat(candidateFile)
		if err == nil && !info.IsDir() {
			absF, _ := filepath.Abs(candidateFile)
			return []string{absF}, nil
		}
	}

	return nil, fmt.Errorf("package or file not found: %s", importPath)
}
