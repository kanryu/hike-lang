package mod

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRequireAndResolveDownloadedDependency(t *testing.T) {
	root := t.TempDir()
	dep := filepath.Join(root, ".hike", "deps", "github.com", "example", "library")
	if err := os.MkdirAll(dep, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("module sample\nhike 0.1.0\nrequire github.com/example/library v1.2.3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := FindModuleRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Requires["github.com/example/library"]; got != "v1.2.3" {
		t.Fatalf("require version = %q, want v1.2.3", got)
	}
	got, err := m.ResolvePackagePath(root, "github.com/example/library")
	if err != nil {
		t.Fatal(err)
	}
	if got != dep {
		t.Fatalf("dependency path = %q, want %q", got, dep)
	}
}
