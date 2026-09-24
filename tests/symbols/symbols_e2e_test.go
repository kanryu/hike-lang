package symbols_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	codegensymbols "hikec-go/pkg/codegen/symbols"
	"hikec-go/tests/testutil"
)

var (
	projectRoot string
	hikecBin    string
)

func TestMain(m *testing.M) {
	root, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	root, err = filepath.Abs(filepath.Join(root, "..", ".."))
	if err != nil {
		os.Exit(1)
	}
	projectRoot = root

	base, err := os.MkdirTemp(root, ".symbols-test-")
	if err != nil {
		os.Exit(1)
	}
	hikecBin = filepath.Join(base, "hikec")
	if runtime.GOOS == "windows" {
		hikecBin += ".exe"
	}
	if output, err := testutil.BuildHikec(root, hikecBin); err != nil {
		fmt.Fprintf(os.Stderr, "hikec build failed: %v\n%s", err, output)
		_ = os.RemoveAll(base)
		os.Exit(1)
	}

	status := m.Run()
	_ = os.RemoveAll(base)
	os.Exit(status)
}

func TestExportSymbolsE2E(t *testing.T) {
	t.Parallel()
	testDir := t.TempDir()

	mainSource := `package main

import "dep"

type Counter struct {
    Value int
}

type Table struct {}

type Adder interface {
    Add(int) int
}

func (c *Counter) Add(value int) int {
    total := value
    return total + c.Value
}

func (c *Counter) Other() int {
    return c.Value
}

func (t *Table) Get(index int) int {
    return index
}

func main() int {
    localValue := dep.Helper()
    counter := Counter{Value: localValue}
    return counter.Add(1)
}
`
	depSource := `package dep

type External struct {
    Field int
}

func Helper() int {
    importedLocal := 7
    return importedLocal
}
`
	writeFile(t, filepath.Join(testDir, "main.hike"), mainSource)
	writeFile(t, filepath.Join(testDir, "dep", "dep.hike"), depSource)
	writeFile(t, filepath.Join(testDir, "hike.mod"), "module symbols-test\nhike 0.1.0\nreplace dep => ./dep\n")

	outputPath := filepath.Join(testDir, "symbols.json")
	cmd := exec.Command(hikecBin, "emit-ir", "--export-symbols", outputPath, filepath.Join(testDir, "main.hike"))
	cmd.Dir = testDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("symbol export failed: %v\n%s", err, output)
	}

	file, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("open symbols JSON: %v", err)
	}
	defer file.Close()
	var exported codegensymbols.Export
	if err := json.NewDecoder(file).Decode(&exported); err != nil {
		t.Fatalf("decode symbols JSON: %v", err)
	}

	if !contains(exported.Modules, "dep") {
		t.Fatalf("modules = %#v", exported.Modules)
	}
	if !contains(exported.Functions, "main.main") || !contains(exported.Functions, "dep.Helper") {
		t.Fatalf("functions = %#v", exported.Functions)
	}
	if !contains(exported.Locals, "localValue") || !contains(exported.Locals, "importedLocal") {
		t.Fatalf("locals = %#v", exported.Locals)
	}
	if !hasStruct(exported.TypeStructs, "main.Counter", "main.Counter.Value") || !hasStruct(exported.TypeStructs, "dep.External", "dep.External.Field") {
		t.Fatalf("type structs = %#v", exported.TypeStructs)
	}
	if !hasInterface(exported.TypeInterfaces, "main.Adder", "main.Adder.Add") {
		t.Fatalf("type interfaces = %#v", exported.TypeInterfaces)
	}
	if !hasReceiver(exported.FixedReceivers, "main.Add", "*main.Counter") || !hasReceiver(exported.FixedReceivers, "main.Get", "*main.Table") {
		t.Fatalf("fixed receivers = %#v", exported.FixedReceivers)
	}
	if hasReceiver(exported.FixedReceivers, "main.Other", "*main.Counter") {
		t.Fatalf("ordinary method unexpectedly exported as fixed receiver: %#v", exported.FixedReceivers)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasStruct(values []codegensymbols.Struct, name, member string) bool {
	for _, value := range values {
		if value.Name == name && contains(value.Members, member) {
			return true
		}
	}
	return false
}

func hasInterface(values []codegensymbols.Interface, name, method string) bool {
	for _, value := range values {
		if value.Name == name && contains(value.Methods, method) {
			return true
		}
	}
	return false
}

func hasReceiver(values []codegensymbols.Method, name, receiver string) bool {
	for _, value := range values {
		if value.Name == name && value.Receiver == receiver {
			return true
		}
	}
	return false
}
