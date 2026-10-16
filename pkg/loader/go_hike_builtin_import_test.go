package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoHikeResolvesBuiltinUnsafeImport(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module builtin-import\nhike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nimport \"unsafe\"\n\nvar _ unsafe.Pointer\n"), 0644); err != nil {
		t.Fatal(err)
	}

	l := New(root)
	l.SetGoHikeMode(true)
	l.SetBuildTags(map[string]bool{"windows": true, "amd64": true})
	if _, err := l.Load(filepath.Join(root, "main.go")); err != nil {
		t.Fatalf("Go-Hike loader rejected builtin unsafe import: %v", err)
	}
}
