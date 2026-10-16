package memoryblock

import (
	"encoding/binary"
	"path/filepath"
	"testing"
)

func TestSerializeGraphPreservesRootHeapRelocations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.hmb")
	graph := Graph{
		Root:        []byte{1, 0, 0, 0, 0, 0, 0, 0},
		Heap:        []GraphObject{{Data: []byte("hello")}},
		Relocations: []GraphRelocation{{Source: 0, Target: 8}}, SchemaHash: 42,
	}
	if err := SerializeGraph(path, graph); err != nil {
		t.Fatal(err)
	}
	root, heap, payload, relocations, schemaHash, err := ReadGraph(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(root) != 8 || string(heap) != "hello" || len(relocations) != 1 {
		t.Fatalf("unexpected graph: root=%v heap=%q relocations=%v", root, heap, relocations)
	}
	if schemaHash != 42 {
		t.Fatalf("schema hash = %d, want 42", schemaHash)
	}
	if got := binary.LittleEndian.Uint64(payload[:8]); got != 8 {
		t.Fatalf("serialized target = %d, want 8", got)
	}
	if err := RestoreGraphPointers(0x1000, payload, relocations); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(payload[:8]); got != 0x1008 {
		t.Fatalf("restored pointer = %#x, want %#x", got, 0x1008)
	}
}

func TestSerializeGraphRejectsInvalidRelocation(t *testing.T) {
	err := SerializeGraph(filepath.Join(t.TempDir(), "bad.hmb"), Graph{
		Root:        make([]byte, 8),
		Relocations: []GraphRelocation{{Source: 8, Target: 0}},
	})
	if err == nil {
		t.Fatal("expected invalid graph relocation error")
	}
}

func TestValidateSchemaHash(t *testing.T) {
	if err := ValidateSchemaHash(11, 12); err == nil || err.Error() != "memorymap: mapped schema hash mismatch (expected 000000000000000b, got 000000000000000c)" {
		t.Fatalf("unexpected schema mismatch error: %v", err)
	}
	if err := ValidateSchemaHash(11, 11); err != nil {
		t.Fatal(err)
	}
}
