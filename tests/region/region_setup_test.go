package region_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var testRoot string
var hikecBin string

func TestMain(m *testing.M) {
	root, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "std")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			os.Exit(1)
		}
		root = parent
	}
	testRoot = root
	// Keep the temporary module on the same volume as std so Windows can form
	// a relative replace path (the module resolver intentionally rejects
	// cross-volume relative paths).
	base, err := os.MkdirTemp(root, ".region-test-")
	if err != nil {
		os.Exit(1)
	}
	hikecBin = filepath.Join(base, "hikec")
	if runtime.GOOS == "windows" {
		hikecBin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", hikecBin, filepath.Join(root, "cmd", "hikec"))
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "hikec build failed: %v\n%s", err, out)
		_ = os.RemoveAll(base)
		os.Exit(1)
	}
	status := m.Run()
	// os.Exit does not run deferred calls, so remove the temporary module
	// explicitly after all region tests have completed.
	_ = os.RemoveAll(base)
	os.Exit(status)
}

type regionCase struct {
	source string
	want   string
}

func runRegionCase(t *testing.T, tc regionCase) {
	runRegionCaseWithMode(t, tc, true)
}

func runRegionCaseWithMode(t *testing.T, tc regionCase, enabled bool) {
	t.Helper()
	dir, err := os.MkdirTemp(testRoot, ".region-case-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "main.hike")
	if err := os.WriteFile(path, []byte(tc.source), 0644); err != nil {
		t.Fatal(err)
	}
	stdDir := filepath.Join(testRoot, "std")
	relStd, err := filepath.Rel(dir, stdDir)
	if err != nil {
		relStd = stdDir
	}
	mod := "module region-test\n\nhike 0.1.0\n\nreplace std => " + filepath.ToSlash(relStd) + "\n"
	mod += "replace std/alloc => " + filepath.ToSlash(filepath.Join(relStd, "alloc")) + "\n"
	mod += "replace std/alloc/region => " + filepath.ToSlash(filepath.Join(relStd, "alloc", "region")) + "\n"
	if entries, err := os.ReadDir(stdDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				mod += "replace std/" + entry.Name() + " => " + filepath.ToSlash(filepath.Join(relStd, entry.Name())) + "\n"
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	args := []string{"run"}
	if enabled {
		args = append(args, "--alloc=region")
	}
	args = append(args, path)
	cmd := exec.Command(hikecBin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); !enabled {
		if err == nil || !strings.Contains(stderr.String(), "requires --alloc=region") {
			t.Fatalf("region API was not rejected without --alloc=region: err=%v stderr=%s", err, stderr.String())
		}
		return
	} else if err != nil {
		t.Fatalf("region program failed: %v\n%s", err, stderr.String())
	}
	got := strings.TrimSpace(strings.ReplaceAll(stdout.String(), "\r\n", "\n"))
	if got != tc.want {
		t.Fatalf("output mismatch: want %q, got %q\nstderr: %s", tc.want, got, stderr.String())
	}
}
