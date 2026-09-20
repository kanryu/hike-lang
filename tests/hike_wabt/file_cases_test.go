package hike_wabt_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestHikeWabtIntegerCases runs programs whose main function returns int.
func TestHikeWabtIntegerCases(t *testing.T) {
	root := filepath.Join(projectRoot, "tests", "hike_wabt", "cases_int")
	paths, err := filepath.Glob(filepath.Join(root, "*.hike"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no file-based Hike cases found")
	}

	for _, path := range paths {
		path := path
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wantBytes, err := os.ReadFile(strings.TrimSuffix(path, ".hike") + ".want")
			if err != nil {
				t.Fatalf("read expected result: %v", err)
			}
			want := string(wantBytes)
			if got := buildAndRunHikeWabt(t, string(source)); got != want {
				t.Fatalf("Wasmtime int result = %q, want %q", got, want)
			}
		})
	}
}

// TestHikeWabtStringCases runs a user-defined function and validates the
// Hike string view read from Wasm linear memory by the Wasmtime runner.
func TestHikeWabtStringCases(t *testing.T) {
	root := filepath.Join(projectRoot, "tests", "hike_wabt", "cases_string")
	paths, err := filepath.Glob(filepath.Join(root, "*.hike"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no string file-based Hike cases found")
	}

	for _, path := range paths {
		path := path
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wantBytes, err := os.ReadFile(strings.TrimSuffix(path, ".hike") + ".want")
			if err != nil {
				t.Fatalf("read expected string: %v", err)
			}
			want := string(wantBytes)
			got := buildAndRunHikeWabtStringMode(t, string(source), "testOutput", isStringWantRecord(want))
			if got != want {
				t.Fatalf("Wasmtime string result = %q, want %q", got, string(wantBytes))
			}
		})
	}
}

// Structured string expectations use offset,length,"text".  The quoted
// field keeps commas and newlines in the returned string unambiguous.
func isStringWantRecord(want string) bool {
	parts := strings.SplitN(strings.TrimSpace(want), ",", 3)
	if len(parts) != 3 {
		return false
	}
	if !strings.HasPrefix(parts[2], "\"") {
		return false
	}
	if _, err := strconv.ParseUint(parts[0], 10, 32); err != nil {
		return false
	}
	if _, err := strconv.ParseUint(parts[1], 10, 32); err != nil {
		return false
	}
	return true
}

func TestHikeWabtBuildConstraintsAcrossFiles(t *testing.T) {
	mainSource := `package main

func main() int { return selectedPlatform() + selectedConstraint() }
`
	files := map[string]string{
		"platform_wasm32.hike": `//go:build wasm32

package main

func selectedPlatform() int { return 40 }
`,
		"platform_windows_amd64.hike": `//go:build windows && amd64

package main

func selectedPlatform() int { return 99 }
`,
		"constraint_wasip1.hike": `//hike:build wasip1

package main

func selectedConstraint() int { return 2 }
`,
		"constraint_linux.hike": `//go:build linux && amd64

package main

func selectedConstraint() int { return 100 }
`,
	}
	if got, want := buildAndRunHikeWabtProject(t, mainSource, files), "42\n"; got != want {
		t.Fatalf("build-constraint result = %q, want %q", got, want)
	}
}
