package hike_wabt_test

import (
	"os"
	"os/exec"
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
	roots := []string{root}
	if os.Getenv("HIKE_WABT_INCLUDE_STUBS") == "1" {
		roots = append(roots, filepath.Join(root, "stub"))
	}
	var paths []string
	for _, caseRoot := range roots {
		matched, err := filepath.Glob(filepath.Join(caseRoot, "*.hike"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range matched {
			if isConcurrentStringCase(path) {
				continue
			}
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		t.Fatal("no string file-based Hike cases found")
	}

	for _, path := range paths {
		path := path
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if filepath.Dir(path) != root {
			name = filepath.Join(filepath.Base(filepath.Dir(path)), name)
		}
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
				if os.Getenv("HIKE_WABT_IGNORE_STRING_OFFSETS") == "1" && sameStringPayload(got, want) {
					return
				}
				t.Fatalf("Wasmtime string result = %q, want %q", got, string(wantBytes))
			}
		})
	}
}

// TestHikeWabtConcurrentStringCases runs the cases whose execution model
// requires the concurrent Wasm runtime and checker bootstrap.
func TestHikeWabtConcurrentStringCases(t *testing.T) {
	if os.Getenv("HIKE_WABT_INCLUDE_STUBS") != "1" {
		t.Skip("concurrent hike_wabt cases are opt-in; set HIKE_WABT_INCLUDE_STUBS=1")
	}
	root := filepath.Join(projectRoot, "tests", "hike_wabt", "cases_string", "stub")
	paths, err := filepath.Glob(filepath.Join(root, "TestConcurrent_EventLoop_*.hike"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Skip("no concurrent string cases found")
	}
	if !checkerSupportsConcurrentMode() {
		t.Skip("wasm-checker does not support --mode=concurrent; rebuild bin/wasm-checker")
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
			got := buildAndRunHikeWabtStringWithCheckerMode(t, string(source), "testOutput", isStringWantRecord(want), "concurrent")
			if got != want && !sameStringPayload(got, want) {
				t.Fatalf("Wasmtime concurrent string result = %q, want %q", got, want)
			}
		})
	}
}

func TestHikeWabtConcurrentMultipleWorkers(t *testing.T) {
	if os.Getenv("HIKE_WABT_INCLUDE_STUBS") != "1" {
		t.Skip("multi-worker checker cases are opt-in; set HIKE_WABT_INCLUDE_STUBS=1")
	}
	if !checkerSupportsConcurrentMode() || !checkerSupportsWorkers() {
		t.Skip("wasm-checker does not support concurrent multi-worker mode")
	}
	source := `package main

func testOutput() string {
    first := Async(func() string { return "A" })
    second := Async(func() string { return "B" })
    third := Async(func() string { return "C" })
    fourth := Async(func() string { return "D" })
    return <-first + <-second + <-third + <-fourth
}
`
	got := buildAndRunHikeWabtStringWithCheckerModeAndWorkers(t, source, "testOutput", false, "concurrent", 2)
	if got != "ABCD" {
		t.Fatalf("multi-worker Wasmtime result = %q, want %q", got, "ABCD")
	}
}

func checkerSupportsConcurrentMode() bool {
	cmd := exec.Command(wasmtimeBin)
	out, _ := cmd.CombinedOutput()
	return strings.Contains(string(out), "--mode=normal|concurrent")
}

func checkerSupportsWorkers() bool {
	cmd := exec.Command(wasmtimeBin)
	out, _ := cmd.CombinedOutput()
	return strings.Contains(string(out), "--workers=N")
}

func isConcurrentStringCase(path string) bool {
	return strings.Contains(filepath.Base(path), "TestConcurrent_EventLoop_")
}

func sameStringPayload(got, want string) bool {
	gotParts := strings.SplitN(strings.TrimSpace(got), ",", 3)
	wantParts := strings.SplitN(strings.TrimSpace(want), ",", 3)
	if len(gotParts) != 3 || len(wantParts) != 3 {
		return false
	}
	gotLength, err := strconv.ParseUint(gotParts[1], 10, 32)
	if err != nil {
		return false
	}
	wantLength, err := strconv.ParseUint(wantParts[1], 10, 32)
	if err != nil || gotLength != wantLength {
		return false
	}
	gotText, err := strconv.Unquote(gotParts[2])
	if err != nil {
		return false
	}
	wantText, err := strconv.Unquote(wantParts[2])
	return err == nil && gotText == wantText
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
