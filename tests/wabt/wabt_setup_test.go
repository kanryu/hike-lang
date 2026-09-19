package wabt_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"hikec-go/tests/testutil"
)

var parallelTests sync.Map
var hikecBin string

func projectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestMain(m *testing.M) {
	root, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	root, err = filepath.Abs(filepath.Join(root, "..", ".."))
	if err != nil {
		os.Exit(1)
	}
	base, err := os.MkdirTemp(root, ".wabt-test-")
	if err != nil {
		os.Exit(1)
	}
	hikecBin = filepath.Join(base, "hikec.exe")
	if output, err := testutil.BuildHikec(root, hikecBin); err != nil {
		os.Stderr.WriteString("hikec build failed: " + err.Error() + "\n")
		os.Stderr.Write(output)
		_ = os.RemoveAll(base)
		os.Exit(1)
	}
	status := m.Run()
	_ = os.RemoveAll(base)
	os.Exit(status)
}

func requireTools(t *testing.T) {
	t.Helper()
	// Every WABT test is independent: it uses its own temporary build
	// directory and process. Some tests call this helper both while building
	// and while running the module, so mark each testing.T only once.
	if _, loaded := parallelTests.LoadOrStore(t, struct{}{}); !loaded {
		t.Parallel()
	}
	for _, name := range []string{"wat2wasm", "node"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " is required for the Wabt E2E test")
		}
	}
}

func buildWabt(t *testing.T, source string) string {
	t.Helper()
	return buildWabtProject(t, source, nil)
}

func buildWabtProject(t *testing.T, source string, files map[string]string) string {
	t.Helper()
	requireTools(t)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "main.hike")
	if err := os.WriteFile(src, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write Wabt source %s: %v", name, err)
		}
	}
	wasm := filepath.Join(tmp, "main.wasm")
	sources := []string{src}
	for name := range files {
		sources = append(sources, filepath.Join(tmp, name))
	}
	sort.Strings(sources[1:])
	args := []string{"build", "-target", "wabt", "-o", wasm}
	args = append(args, sources...)
	cmd := exec.Command(hikecBin, args...)
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Wabt build failed: %v\n%s", err, out)
	}
	return wasm
}

func runWabt(t *testing.T, wasm string) string {
	t.Helper()
	requireTools(t)
	runner := filepath.Join(projectRoot(t), "tests", "wabt", "langwabt.js")
	script := filepath.Join(filepath.Dir(runner), "wabt_arithmetic.js")
	cmd := exec.Command("node", runner, wasm, script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Node.js Wabt execution failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}
