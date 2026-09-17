package parser

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/lexer"
)

// TestLoaderBodySyntaxReproduction keeps the loader's failing control-flow
// shape isolated from semantic and lowering tests. It is intentionally close
// to loader.go: package discovery uses range loops, short declarations,
// multiple assignment, and a negated map lookup in a compound condition.
func TestLoaderBodySyntaxReproduction(t *testing.T) {
	source := `package loader

type Loader struct {
    visitedFiles map[string]bool
    visitedPkgs map[string]bool
}

func (l *Loader) Load(entryPaths []string) error {
    fileQueue := []string{}
    for _, path := range entryPaths {
        absPath, err := filepath.Abs(path)
        if err != nil {
            return err
        }
        isDir, err := isDirectory(absPath)
        if err != nil {
            return err
        }
        if isDir {
            files, err := l.findHikeFilesInDir(absPath)
            if err != nil {
                return err
            }
            fileQueue = append(fileQueue, files...)
        } else {
            fileQueue = append(fileQueue, absPath)
        }
    }

    for len(fileQueue) > 0 {
        curFile := fileQueue[0]
        fileQueue = fileQueue[1:]
        if l.visitedFiles[curFile] {
            continue
        }
        l.visitedFiles[curFile] = true
        content, err := readFile(curFile)
        if err != nil {
            return err
        }
        for _, imp := range imports(content) {
            if module == nil {
                continue
            }
            pkgDir, err := resolvePackage(module, curFile, imp)
            if err == nil && pkgDir != "" && !l.visitedPkgs[pkgDir] {
                l.visitedPkgs[pkgDir] = true
                files, err := l.findHikeFilesInDir(pkgDir)
                if err != nil {
                    return err
                }
                fileQueue = append(fileQueue, files...)
            }
        }
    }
    return nil
}

func (l *Loader) findHikeFilesInDir(dir string) ([]string, error) {
    entries, err := readDir(dir)
    if err != nil {
        return nil, err
    }
    files := []string{}
    for _, entry := range entries {
        path := join(dir, entry)
        isSource := hasSuffix(entry, ".hike")
        if !entryIsDir(entry) && isSource && l.visitedFiles[path] {
            files = append(files, path)
        }
    }
    return files, nil
}
`
	p := New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("loader reproduction must parse: %v", p.Errors())
	}
	if len(program.Decls) != 3 {
		t.Fatalf("expected Loader plus two methods, got %d declarations", len(program.Decls))
	}
	if _, ok := program.Decls[1].(*ast.FuncDecl); !ok {
		t.Fatalf("expected Load function, got %T", program.Decls[1])
	}
}
