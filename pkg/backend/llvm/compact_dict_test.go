package llvm

import (
	"fmt"
	"testing"
)

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

func TestCompactDictGrowsAndRetainsLiveEntryOrder(t *testing.T) {
	d := newCompactDict(1)
	for i := 0; i < 40; i++ {
		key := fmt.Sprintf("key-%02d", i)
		d.set(key, uint64(i))
	}

	entries := d.orderedEntries()
	if len(entries) != 40 {
		t.Fatalf("entry count after growth = %d, want 40", len(entries))
	}
	for i, entry := range entries {
		wantKey := fmt.Sprintf("key-%02d", i)
		if entry.key != wantKey || entry.value != uint64(i) {
			t.Fatalf("entry[%d] = {%q, %d}, want {%q, %d}", i, entry.key, entry.value, wantKey, i)
		}
	}
	for i := 0; i < 40; i++ {
		key := fmt.Sprintf("key-%02d", i)
		if value, ok := d.get(key); !ok || value != uint64(i) {
			t.Fatalf("lookup after growth for %q = (%d, %v), want (%d, true)", key, value, ok, i)
		}
	}
}

func TestCompactDictReusesTombstonesAndDropsThemOnGrowth(t *testing.T) {
	d := newCompactDict(8)
	const tableSize = uint64(8)
	firstSlot := compactDictHash("collision-0") % tableSize
	keys := make([]string, 0, 4)
	for i := 0; len(keys) < cap(keys); i++ {
		key := fmt.Sprintf("collision-%d", i)
		if compactDictHash(key)%tableSize == firstSlot {
			keys = append(keys, key)
		}
	}
	for i, key := range keys[:3] {
		d.set(key, uint64(i+1))
	}

	if !d.delete(keys[0]) {
		t.Fatal("delete did not find the first colliding entry")
	}
	d.set(keys[3], 4)

	entries := d.orderedEntries()
	if len(entries) != 3 || entries[0].key != keys[1] || entries[1].key != keys[2] || entries[2].key != keys[3] {
		t.Fatalf("unexpected order after tombstone reuse: %+v", entries)
	}

	for i := 0; i < 20; i++ {
		d.set(fmt.Sprintf("extra-%02d", i), uint64(i))
	}
	if _, ok := d.get(keys[0]); ok {
		t.Fatal("deleted key became visible after growth")
	}
	if value, ok := d.get(keys[3]); !ok || value != 4 {
		t.Fatalf("replacement lookup after growth = (%d, %v), want (4, true)", value, ok)
	}
}
