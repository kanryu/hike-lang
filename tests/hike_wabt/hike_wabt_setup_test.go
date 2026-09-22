package hike_wabt_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

var (
	projectRoot string
	hikeHikeBin string
	wasmtimeBin string
	testBase    string
)

func TestMain(m *testing.M) {
	if os.Getenv("HIKE_WABT_TESTS") != "1" {
		fmt.Fprintln(os.Stderr, "hike_wabt tests skipped; set HIKE_WABT_TESTS=1 to run them")
		os.Exit(0)
	}

	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	projectRoot = root

	base, err := os.MkdirTemp(root, ".hike-wabt-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testBase = base
	defer os.RemoveAll(base)

	binName := "hike-hike"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	hikeHikeBin = filepath.Join(base, binName)

	build := exec.Command("go", "build", "-o", hikeHikeBin, filepath.Join(root, "cmd", "hike-hike"))
	build.Dir = root
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".gocache"))
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "hike-hike build failed: %v\n%s\n", err, output)
		os.Exit(1)
	}

	wasmtimeBin = os.Getenv("WASM_CHECKER_PATH")
	if wasmtimeBin == "" {
		// Keep accepting the previous name while local environments migrate.
		wasmtimeBin = os.Getenv("WASMTIME_HIKE_PATH")
	}
	if wasmtimeBin == "" {
		wasmtimeBin = filepath.Join(root, "bin", "wasm-checker.exe")
		if runtime.GOOS != "windows" {
			wasmtimeBin = filepath.Join(root, "bin", "wasm-checker")
		}
		if _, err := os.Stat(wasmtimeBin); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(wasmtimeBin), 0755); err != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker directory creation failed: %v\n", err)
				os.Exit(1)
			}
			build := exec.Command("go", "build", "-o", wasmtimeBin, filepath.Join(root, "cmd", "wasm-checker"))
			build.Dir = root
			build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".gocache"))
			if output, err := build.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "wasm-checker build failed: %v\n%s\n", err, output)
				os.Exit(1)
			}
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "wasm-checker stat failed: %v\n", err)
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

func findProjectRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "", fmt.Errorf("could not find project root")
}

func requireHikeWabtTools(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(wasmtimeBin); err != nil {
		t.Skipf("Wasmtime Hike runner not found: %s", wasmtimeBin)
	}
	for _, name := range []string{"wat2wasm"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is required for the Hike-WABT E2E test", name)
		}
	}
}

// buildAndRunHikeWabt follows the complete external pipeline:
// Hike source -> hike-hike -> WAT -> wat2wasm -> Wasmtime.
func buildAndRunHikeWabt(t *testing.T, source string) string {
	t.Helper()
	return buildAndRunHikeWabtProject(t, source, nil)
}

func buildAndRunHikeWabtProject(t *testing.T, source string, files map[string]string) string {
	t.Helper()
	requireHikeWabtTools(t)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "main.hike")
	wat := filepath.Join(tmp, "main.wat")
	wasm := filepath.Join(tmp, "main.wasm")
	if err := os.WriteFile(src, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write companion source %s: %v", name, err)
		}
	}

	sources := []string{src}
	for name := range files {
		sources = append(sources, filepath.Join(tmp, name))
	}
	sort.Strings(sources[1:])
	args := []string{"-o", wat}
	args = append(args, sources...)
	compile := exec.Command(hikeHikeBin, args...)
	compile.Dir = projectRoot
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("hike-hike WAT generation failed: %v\n%s", err, output)
	}

	assemble := exec.Command("wat2wasm", "--enable-threads", wat, "-o", wasm)
	if output, err := assemble.CombinedOutput(); err != nil {
		t.Fatalf("wat2wasm failed: %v\n%s", err, output)
	}

	var stdout, stderr bytes.Buffer
	run := exec.Command(wasmtimeBin, wasm, "main")
	run.Stdout = &stdout
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("Wasmtime int execution failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func buildAndRunHikeWabtString(t *testing.T, source, function string) string {
	return buildAndRunHikeWabtStringMode(t, source, function, false)
}

func buildAndRunHikeWabtStringMode(t *testing.T, source, function string, structured bool) string {
	return buildAndRunHikeWabtStringWithCheckerMode(t, source, function, structured, "normal")
}

func buildAndRunHikeWabtStringWithCheckerMode(t *testing.T, source, function string, structured bool, checkerMode string) string {
	return buildAndRunHikeWabtStringWithCheckerModeAndWorkers(t, source, function, structured, checkerMode, 1)
}

func buildAndRunHikeWabtStringWithCheckerModeAndWorkers(t *testing.T, source, function string, structured bool, checkerMode string, workers int) string {
	t.Helper()
	requireHikeWabtTools(t)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "main.hike")
	wat := filepath.Join(tmp, "main.wat")
	wasm := filepath.Join(tmp, "main.wasm")
	if err := os.WriteFile(src, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	compileArgs := []string{"-o", wat}
	if checkerMode != "normal" {
		compileArgs = append(compileArgs, "--wasm-mode="+checkerMode)
	}
	compileArgs = append(compileArgs, src)
	compile := exec.Command(hikeHikeBin, compileArgs...)
	compile.Dir = projectRoot
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("hike-hike string WAT generation failed: %v\n%s", err, output)
	}
	assemble := exec.Command("wat2wasm", "--enable-threads", wat, "-o", wasm)
	if output, err := assemble.CombinedOutput(); err != nil {
		t.Fatalf("wat2wasm string case failed: %v\n%s", err, output)
	}
	var stdout, stderr bytes.Buffer
	mode := "--string"
	if structured {
		mode = "--string-info"
	}
	args := []string{wasm, function}
	// Normal mode is the historical default.  Omitting the explicit flag
	// keeps the test loop compatible with an already-built checker binary;
	// concurrent mode must be selected explicitly because it has a different
	// bootstrap sequence.
	if checkerMode != "normal" {
		args = append(args, "--mode="+checkerMode)
	}
	if workers > 1 {
		args = append(args, fmt.Sprintf("--workers=%d", workers))
	}
	args = append(args, mode)
	run := exec.Command(wasmtimeBin, args...)
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("Wasmtime string execution failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}
