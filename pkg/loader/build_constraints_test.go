package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

func TestFilenameAndExpressionBuildConstraints(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"selected_windows_amd64.hike": "package main\n",
		"excluded_linux_amd64.hike":   "package main\n",
		"expr.hike":                   "//go:build (windows && amd64) || (darwin && !cgo)\npackage main\n",
		"expr_linux.hike":             "//hike:build linux && amd64\npackage main\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	l := New(dir)
	win, _ := target.ParseTarget("windows")
	l.SetTarget(win)
	if !l.fileAllowed(filepath.Join(dir, "selected_windows_amd64.hike")) || !l.fileAllowed(filepath.Join(dir, "expr.hike")) {
		t.Fatal("windows/amd64 files should be selected")
	}
	if l.fileAllowed(filepath.Join(dir, "excluded_linux_amd64.hike")) || l.fileAllowed(filepath.Join(dir, "expr_linux.hike")) {
		t.Fatal("linux-only files should be excluded for windows")
	}

	linux, _ := target.ParseTarget("linux")
	l.SetTarget(linux)
	if !l.fileAllowed(filepath.Join(dir, "excluded_linux_amd64.hike")) || !l.fileAllowed(filepath.Join(dir, "expr_linux.hike")) {
		t.Fatal("linux/amd64 files should be selected")
	}
	if l.fileAllowed(filepath.Join(dir, "selected_windows_amd64.hike")) || l.fileAllowed(filepath.Join(dir, "expr.hike")) {
		t.Fatal("windows-only files should be excluded for linux")
	}
}

func TestInlineAsmRequiresArchitectureConstraint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unsafe.hike")
	source := "package main\nfunc f() { __asm__{ params: \"aesenc\", \"\", \"\", \"\" } }\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	l := New(dir)
	win, _ := target.ParseTarget("windows")
	l.SetTarget(win)
	_, err := l.Load(path)
	if err == nil || !strings.Contains(err.Error(), "requires a matching //go:build amd64 constraint") {
		t.Fatalf("expected architecture build constraint error, got %v", err)
	}
}
