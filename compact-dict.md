# Compact Dict and Built-in Maps

Hike provides two built-in map types:

- `map[K]V` uses Hike's compact-dict runtime.
- `hashmap[K]V` uses the conventional bucketed hash-map runtime.

This distinction is intentional. The default `map[K]V` is designed for
efficient iteration and compact storage, while `hashmap[K]V` preserves the
traditional Hike/Go-style hash-map behavior for code that benefits from that
representation.

## Compact-dict implementation

The implementation of `map[K]V` replaces the earlier bucketed map
representation with a compact-dict layout inspired by Python's compact
dictionary design. It is a native Hike/LLVM implementation; it does not
embed or depend on Python.

A compact dict is organized around two logical arrays:

1. An index table maps hash-table slots to entry numbers.
2. A dense entry array stores hashes, keys, values, and live/deleted state.

The index table uses open addressing and linear probing. Empty slots and
deleted slots are represented separately, so a deleted entry does not require
moving the other entries. The dense entry array keeps live entries in their
insertion sequence. When the table grows, the index table is rebuilt while
the live entries retain that sequence.

The implementation is available in the 64-bit native, 32-bit native, and
Wasm32 LLVM runtimes.

## Map and hashmap are separate built-in types

`map[K]V` and `hashmap[K]V` have similar source-level operations:

```hike
compact := make(map[string, int])
compact["Tokyo"] = 1400

legacy := make(hashmap[string, int])
legacy["Tokyo"] = 1400

value, ok := compact["Tokyo"]
delete(compact, "Tokyo")
```

The runtime representation is different:

| Type | Runtime representation | Iteration order |
| --- | --- | --- |
| `map[K]V` | Compact dict with an index table and dense entries | Insertion order for live entries |
| `hashmap[K]V` | Conventional chained-bucket hash map | Unspecified |

The `hashmap[K]V` type is the built-in legacy map type. It is useful when
compatibility with the traditional bucketed implementation is more important
than compact iteration storage. Existing code that explicitly needs that
representation can use `hashmap[K]V` without relying on the implementation
details of `map[K]V`.

For compatibility, using the `map[K]V` syntax still requires the `std/maps`
import in the current language surface.

## Stable maps

`stable map[K]V` is a fixed-key map for data whose complete key set is known
when the variable is declared. The initializer must define every key and its
initial value:

```hike
var Metrics = stable map[string, int64]{
    "requests_total": 0,
    "errors_total":   0,
    "bytes_sent":     0,
}
```

The key set of a stable map cannot change after initialization. Adding an
entry with `insert` or removing one with `delete` is a compile-time error.
Updating the value for an existing key through the normal set operation is
allowed:

```hike
Metrics["requests_total"] = Metrics["requests_total"] + 1
```

The compiler can use the declared key set to assign each key a dense integer
entry index. Read access to a stable map is therefore lowered to an indexed
access into the entry array, avoiding hash-table probing at runtime. This
optimization applies to accesses whose key is part of the declared stable
map key set; it does not change the source-level key and value semantics.

## Shared string hashing

Both built-in map types use the same string-hashing algorithm and the same
key-equality rules. The runtime uses an FNV-1a-style byte-at-a-time hash for
string keys, and compares the key contents after a hash match.

The algorithm is shared across the compact-dict and bucketed-map runtimes so
that the two implementations have consistent string-key behavior. The native
64-bit and 32-bit/Wasm32 runtimes use the corresponding 64-bit or 32-bit hash
width and constants; this is an ABI-width difference, not a different
string-hashing design.

Integer keys use their integer representation as the hash input. Other key
and value restrictions are determined by Hike's type checker and lowering
rules.

## Compile-time hashing for literal keys (CompactDict M0)

CompactDict M0 also optimizes string-literal keys and other string values that
are known to the compiler. During lowering, Hike interns each such string and
computes its string hash once. The compiler emits the resulting hashes in a
read-only program-level table indexed by the string's intern ID.

For `map[string]V`, the generated set, lookup, and delete operations pass the
cached hash to the compact-dict runtime helpers. The runtime therefore does
not need to scan the key bytes and calculate the hash again for each access;
it only constructs the temporary key view and performs table probing and key
comparison. Repeated accesses using the same literal key can consequently
avoid the hash-calculation cost and are faster than dynamically hashed string
keys.

The `hashmap[string]V` type uses the same string-hashing algorithm and keeps
the same key semantics. Its current legacy bucketed runtime still uses the
ordinary string-key helper path, so the prehashed helper ABI described above
is currently specific to the compact-dict implementation. This keeps the
behavior consistent while allowing the compact-dict path to take advantage
of compile-time hash caching.

## Iteration performance

Both map types provide expected constant-time lookup, insertion, and deletion
under normal hash-table load. Their iteration paths are different.

Compact-dict iteration walks the dense entry array sequentially and skips only
entries marked deleted. It does not scan every hash-table slot and does not
follow a linked list for each entry. This gives it several practical
advantages:

- `for range` is generally faster for the same number of live entries.
- Sequential memory access improves cache locality.
- Iteration cost is close to O(n), where `n` is the number of allocated
  entries, rather than the capacity of a sparse bucket table.
- Insertion order is preserved naturally by the dense entry array.

The bucketed `hashmap[K]V` must traverse bucket chains during iteration.
Those chains introduce pointer chasing and less predictable memory access, so
iteration is generally slower and its order is not guaranteed.

These are representation-level performance properties, not a language-level
promise of a fixed speedup. Workloads dominated by lookups may see a smaller
difference because both representations use hash-based lookup.

## Deletion, growth, and trade-offs

Deleting a compact-dict entry marks its index slot as deleted and its dense
entry as inactive. This avoids moving other entries and keeps iteration order
stable for the remaining entries. Deleted entries are skipped by iteration.

The compact dict grows when its load or entry capacity requires it. Growth
allocates larger arrays, rehashes the live entries, and discards deleted
entries. Consequently, repeated deletion without growth can leave inactive
entries in the dense array, while a subsequent growth compacts them.

The compact representation favors iteration speed, cache locality, and stable
insertion order. The conventional `hashmap[K]V` remains available when its
bucketed behavior or legacy representation is preferable.

