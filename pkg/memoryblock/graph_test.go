package memoryblock

import (
	"encoding/binary"
	"os"
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

func TestSerializeGraphPlacesDynamicRelocationsAfterPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ordered.hmb")
	graph := Graph{
		Root: make([]byte, 16),
		Heap: []GraphObject{{Data: []byte{5, 6}}, {Data: []byte{7, 8, 9}}},
		Relocations: []GraphRelocation{
			{Source: 0, Target: 16},
			{Source: 8, Target: 18},
		},
	}
	if err := SerializeGraph(path, graph); err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	payloadEnd := graphHeaderBytes + len(graph.Root) + 2 + 3
	if len(image) != payloadEnd+len(graph.Relocations)*16 {
		t.Fatalf("image length = %d, want %d", len(image), payloadEnd+len(graph.Relocations)*16)
	}
	if got := binary.LittleEndian.Uint64(image[24:32]); got != uint64(len(graph.Relocations)) {
		t.Fatalf("relocation count = %d, want %d", got, len(graph.Relocations))
	}
	if got := binary.LittleEndian.Uint64(image[48:56]); got != uint64(payloadEnd) {
		t.Fatalf("relocation offset = %d, want %d", got, payloadEnd)
	}
	if got := binary.LittleEndian.Uint64(image[56:64]); got != uint64(len(graph.Relocations)*16) {
		t.Fatalf("relocation bytes = %d, want %d", got, len(graph.Relocations)*16)
	}
	if got := binary.LittleEndian.Uint64(image[payloadEnd : payloadEnd+8]); got != 0 {
		t.Fatalf("first relocation source = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint64(image[payloadEnd+8 : payloadEnd+16]); got != 16 {
		t.Fatalf("first relocation target = %d, want 16", got)
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
