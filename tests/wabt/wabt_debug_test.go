package wabt_test

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWabtDebugNamesAreEmbeddedWithG(t *testing.T) {
	requireTools(t)
	tmp := t.TempDir()
	source := filepath.Join(tmp, "main.hike")
	wasm := filepath.Join(tmp, "main.wasm")
	if err := os.WriteFile(source, []byte(`package main

func main() int {
    value := 41
    return value + 1
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(hikecBin, "build", "-g", "-target", "wabt", "-o", wasm, source)
	cmd.Dir = projectRoot(t)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("WABT debug build failed: %v\n%s", err, output)
	}

	data, err := os.ReadFile(wasm)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWasmCustomSection(data, "name") {
		t.Fatal("-g WABT build did not contain the Wasm name custom section")
	}
	for _, section := range []string{".debug_abbrev", ".debug_str", ".debug_line", ".debug_info"} {
		if !hasWasmCustomSection(data, section) {
			t.Fatalf("-g WABT build did not contain the DWARF custom section %q", section)
		}
	}
}

func hasWasmCustomSection(data []byte, wanted string) bool {
	if len(data) < 8 || string(data[:4]) != "\x00asm" {
		return false
	}
	for offset := 8; offset < len(data); {
		sectionID := data[offset]
		offset++
		sectionSize, n := readULEB(data[offset:])
		if n == 0 {
			return false
		}
		offset += n
		end := offset + int(sectionSize)
		if end > len(data) {
			return false
		}
		if sectionID == 0 {
			nameSize, nameN := readULEB(data[offset:])
			if nameN > 0 {
				nameStart := offset + nameN
				nameEnd := nameStart + int(nameSize)
				if nameEnd <= end && string(data[nameStart:nameEnd]) == wanted {
					return true
				}
			}
		}
		offset = end
	}
	return false
}

func readULEB(data []byte) (uint32, int) {
	value, n := binary.Uvarint(data)
	if n <= 0 || value > uint64(^uint32(0)) {
		return 0, 0
	}
	return uint32(value), n
}
