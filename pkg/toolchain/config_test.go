package toolchain

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadAndExpandProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clang-target.toml")
	contents := `[profiles.windows-msvc]
command = "clang-cl"
args = [
  "/Zi", "/Od",
  "{input}", "/Fe:{output}",
  "/link", "msvcrt.lib", "legacy\_stdio\_definitions.lib"
]
`
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	command, args, ok := cfg.Command("windows-msvc", "input.ll", "out.exe")
	if !ok || command != "clang-cl" {
		t.Fatalf("profile lookup = %q, %v; want clang-cl, true", command, ok)
	}
	want := []string{"/Zi", "/Od", "input.ll", "/Fe:out.exe", "/link", "msvcrt.lib", "legacy_stdio_definitions.lib"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("expanded args = %#v, want %#v", args, want)
	}
}
