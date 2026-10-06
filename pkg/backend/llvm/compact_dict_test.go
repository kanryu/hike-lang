package llvm

import "testing"

func TestCompactDictPreservesInsertionOrderAndHandlesDeletion(t *testing.T) {
	d := newCompactDict(4)
	d.set("first", 1)
	d.set("second", 2)
	d.set("third", 3)
	if !d.delete("second") {
		t.Fatal("delete did not find an existing key")
	}
	d.set("fourth", 4)

	entries := d.orderedEntries()
	if len(entries) != 3 || entries[0].key != "first" || entries[1].key != "third" || entries[2].key != "fourth" {
		t.Fatalf("unexpected insertion order: %+v", entries)
	}
}

func TestCompactDictUsesPrecomputedHashForLookup(t *testing.T) {
	d := newCompactDict(8)
	key := "literal-key"
	hash := compactDictHash(key)
	d.setHashed(key, hash, 42)

	value, ok := d.getHashed(key, hash)
	if !ok || value != 42 {
		t.Fatalf("precomputed-hash lookup failed: value=%d ok=%v", value, ok)
	}
}

func TestCompactDictInternIDFastPathFallsBackForDynamicKeys(t *testing.T) {
	d := newCompactDict(8)
	key := "literal-key"
	hash := compactDictHash(key)
	d.setInterned(key, 3, hash, 42)

	if value, ok := d.getInterned(key, 3, hash); !ok || value != 42 {
		t.Fatalf("intern ID lookup failed: value=%d ok=%v", value, ok)
	}
	if value, ok := d.getInterned(string([]byte(key)), -1, hash); !ok || value != 42 {
		t.Fatalf("dynamic-key fallback failed: value=%d ok=%v", value, ok)
	}
}

func TestCompactDictUpdatesCollidingKeys(t *testing.T) {
	d := newCompactDict(8)
	const collision = uint64(7)
	d.setHashed("a", collision, 1)
	d.setHashed("b", collision, 2)
	d.setHashed("a", collision, 3)

	if value, ok := d.getHashed("a", collision); !ok || value != 3 {
		t.Fatalf("collision update failed for a: value=%d ok=%v", value, ok)
	}
	if value, ok := d.getHashed("b", collision); !ok || value != 2 {
		t.Fatalf("collision lookup failed for b: value=%d ok=%v", value, ok)
	}
}
