package lower

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/hir"
	"hikec-go/pkg/sema"
)

func TestStringConstantsReceiveStableInternIDsAndHashes(t *testing.T) {
	l := New(&ast.Program{}, sema.NewContext())
	first := l.getStringConst("name")
	reused := l.getStringConst("name")
	second := l.getStringConst("value")

	if first != reused {
		t.Fatal("identical string constants must be pooled")
	}
	if first.InternID != 0 || second.InternID != 1 {
		t.Fatalf("unexpected intern IDs: first=%d second=%d", first.InternID, second.InternID)
	}
	if len(l.hirProg.InternHashes) != 2 {
		t.Fatalf("expected two intern hashes, got %d", len(l.hirProg.InternHashes))
	}
	if l.hirProg.InternHashes[0] == l.hirProg.InternHashes[1] {
		t.Fatal("different literals unexpectedly received the same hash")
	}
}

func TestMemoryMapOffsetsUseEightByteAlignment(t *testing.T) {
	for _, test := range []struct {
		in  int
		out int
	}{
		{in: 0, out: 0},
		{in: 1, out: 8},
		{in: 4, out: 8},
		{in: 8, out: 8},
		{in: 15, out: 16},
	} {
		if got := alignMemoryMapOffset(test.in); got != test.out {
			t.Fatalf("alignMemoryMapOffset(%d) = %d, want %d", test.in, got, test.out)
		}
	}
}

func TestMemoryMapMetadataTraversesCompactMapAndNestedPayloads(t *testing.T) {
	layout := &hir.MemoryMapDecl{Name: "state"}
	builder := newMemoryMapMetaBuilder(layout)
	builder.add(hir.MemoryMapMetaEntry{ID: 0, ParentID: 0, Kind: hir.MemoryMapMetaRoot})
	value := &sema.StructType{Fields: []sema.Field{
		{Name: "label", Type: sema.TypeString},
		{Name: "items", Type: &sema.SliceType{Elem: sema.TypeInt}},
	}}
	builder.walk(&sema.MapType{Key: sema.TypeInt, Value: value}, 0, 0, 0)

	if len(layout.Meta) < 9 {
		t.Fatalf("metadata table is too small for a compact map with nested payloads: %d", len(layout.Meta))
	}
	seen := map[hir.MemoryMapMetaKind]bool{}
	for _, entry := range layout.Meta {
		seen[entry.Kind] = true
		switch entry.Kind {
		case hir.MemoryMapMetaMapHeader:
			if entry.ID < hir.MemoryMapMetaMapHeaderBase || entry.ID >= hir.MemoryMapMetaMapEntriesBase {
				t.Fatalf("map header ID is outside its range: %#x", entry.ID)
			}
		case hir.MemoryMapMetaMapEntries:
			if entry.ID < hir.MemoryMapMetaMapEntriesBase || entry.ID >= hir.MemoryMapMetaPayloadBase {
				t.Fatalf("map entry ID is outside its range: %#x", entry.ID)
			}
		case hir.MemoryMapMetaPayload:
			if entry.ID < hir.MemoryMapMetaPayloadBase || entry.ID >= hir.MemoryMapMetaPointerBase {
				t.Fatalf("payload ID is outside its range: %#x", entry.ID)
			}
		case hir.MemoryMapMetaPointer:
			if entry.ID < hir.MemoryMapMetaPointerBase {
				t.Fatalf("pointer ID is outside its range: %#x", entry.ID)
			}
		}
	}
	for _, kind := range []hir.MemoryMapMetaKind{hir.MemoryMapMetaMapHeader, hir.MemoryMapMetaMapEntries, hir.MemoryMapMetaPayload, hir.MemoryMapMetaPointer} {
		if !seen[kind] {
			t.Fatalf("metadata table did not contain kind %d", kind)
		}
	}
}
