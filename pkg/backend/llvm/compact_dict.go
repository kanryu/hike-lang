package llvm

// compactDict is the reference model for the LLVM Compact Dict runtime.
// The runtime stores the same two logical arrays: indices contains entry
// numbers, while entries remains dense and preserves insertion order.
type compactDict struct {
	indices []int
	entries []compactDictEntry
	live    int
}

type compactDictEntry struct {
	hash     uint64
	internID int64
	key      string
	value    uint64
	live     bool
}

func newCompactDict(capacity int) *compactDict {
	if capacity < 8 {
		capacity = 8
	}
	d := &compactDict{indices: make([]int, capacity)}
	for i := range d.indices {
		d.indices[i] = -1
	}
	return d
}

func compactDictHash(key string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	return h
}

func (d *compactDict) lookup(hash uint64, internID int64, key string) (entry, slot int, found bool) {
	firstDeleted := -1
	for n := 0; n < len(d.indices); n++ {
		slot = int((hash + uint64(n)) % uint64(len(d.indices)))
		idx := d.indices[slot]
		switch {
		case idx == -1:
			if firstDeleted >= 0 {
				slot = firstDeleted
			}
			return -1, slot, false
		case idx == -2:
			if firstDeleted < 0 {
				firstDeleted = slot
			}
		case idx >= 0 && d.entries[idx].live:
			candidate := d.entries[idx]
			if candidate.hash == hash && ((internID >= 0 && candidate.internID >= 0 && internID == candidate.internID) || candidate.key == key) {
				return idx, slot, true
			}
		}
	}
	return -1, firstDeleted, false
}

func (d *compactDict) setInterned(key string, internID int64, hash, value uint64) {
	if d.live*2 >= len(d.indices) {
		d.grow()
	}
	idx, slot, found := d.lookup(hash, internID, key)
	if found {
		d.entries[idx].value = value
		return
	}
	idx = len(d.entries)
	d.entries = append(d.entries, compactDictEntry{hash: hash, internID: internID, key: key, value: value, live: true})
	d.indices[slot] = idx
	d.live++
}

func (d *compactDict) set(key string, value uint64) {
	d.setInterned(key, -1, compactDictHash(key), value)
}

func (d *compactDict) setHashed(key string, hash, value uint64) {
	d.setInterned(key, -1, hash, value)
}

func (d *compactDict) getInterned(key string, internID int64, hash uint64) (uint64, bool) {
	idx, _, found := d.lookup(hash, internID, key)
	if !found {
		return 0, false
	}
	return d.entries[idx].value, true
}

func (d *compactDict) getHashed(key string, hash uint64) (uint64, bool) {
	return d.getInterned(key, -1, hash)
}

func (d *compactDict) get(key string) (uint64, bool) {
	return d.getHashed(key, compactDictHash(key))
}

func (d *compactDict) delete(key string) bool {
	hash := compactDictHash(key)
	idx, slot, found := d.lookup(hash, -1, key)
	if !found {
		return false
	}
	d.indices[slot] = -2
	d.entries[idx].live = false
	d.live--
	return true
}

func (d *compactDict) grow() {
	old := d.entries
	d.indices = make([]int, len(d.indices)*2)
	for i := range d.indices {
		d.indices[i] = -1
	}
	d.live = 0
	for i := range old {
		if old[i].live {
			idx, slot, found := d.lookup(old[i].hash, old[i].internID, old[i].key)
			if found {
				d.entries[idx].value = old[i].value
				continue
			}
			d.indices[slot] = i
			d.live++
		}
	}
}

func (d *compactDict) orderedEntries() []compactDictEntry {
	result := make([]compactDictEntry, 0, d.live)
	for _, entry := range d.entries {
		if entry.live {
			result = append(result, entry)
		}
	}
	return result
}
