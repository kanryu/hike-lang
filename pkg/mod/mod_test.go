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

func TestResolvePackagePathPrefersSpecificReplacement(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "project")
	specific := filepath.Join(root, "compat", "helpers")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(specific, 0755); err != nil {
		t.Fatal(err)
	}

	m := &Module{
		RootDir: root,
		Replaces: map[string]string{
			"example/project":                  "project",
			"example/project/internal/helpers": "compat/helpers",
		},
	}
	got, err := m.ResolvePackagePath(root, "example/project/internal/helpers")
	if err != nil {
		t.Fatal(err)
	}
	if got != specific {
		t.Fatalf("specific replacement path = %q, want %q", got, specific)
	}
}

func TestFindModuleRootWithoutModFileUsesStartDirectory(t *testing.T) {
	root := t.TempDir()

	m, err := FindModuleRoot(root)
	if err != nil {
		t.Fatalf("FindModuleRoot without hike.mod returned error: %v", err)
	}
	if m.RootDir != root {
		t.Fatalf("synthetic module root = %q, want %q", m.RootDir, root)
	}
	if m.Name != filepath.Base(root) {
		t.Fatalf("synthetic module name = %q, want %q", m.Name, filepath.Base(root))
	}
}

func TestFindModuleRootRejectsMalformedModFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hike.mod"), []byte("hike 0.1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := FindModuleRoot(root)
	if err == nil {
		t.Fatal("FindModuleRoot accepted hike.mod without a module directive")
	}
	if m == nil || m.RootDir != root {
		t.Fatalf("error result did not preserve module root: %#v", m)
	}
}
