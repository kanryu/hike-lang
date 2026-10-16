package memoryblock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSerializeAndPrivateMapping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.hmb")
	data := []byte{1, 2, 3, 4, 5}
	if err := Serialize(path, data, []uint64{0, 4}); err != nil {
		t.Fatal(err)
	}
	m, err := DeserializeMapped(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if string(m.Data) != string(data) || len(m.Pointers) != 2 {
		t.Fatalf("unexpected mapping: %#v %v", m.Data, m.Pointers)
	}
	m.Data[0] = 99
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if b[headerBytes] != 1 {
		t.Fatalf("private mapping wrote through to source: %d", b[headerBytes])
	}
	addresses := RestorePointers(0x1000, m.Pointers)
	if addresses[0] != 0x1000 || addresses[1] != 0x1004 {
		t.Fatalf("bad relocation: %#v", addresses)
	}
}

func TestRejectsOutOfRangePointer(t *testing.T) {
	if err := Serialize(filepath.Join(t.TempDir(), "bad"), []byte{1}, []uint64{1}); err == nil {
		t.Fatal("expected invalid pointer error")
	}
}
