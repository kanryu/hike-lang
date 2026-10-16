package memoryblock

import (
	"fmt"
	"hash/fnv"
)

// SchemaField is the stable description used to detect incompatible mapped
// variable declarations before applying serialized bytes.
type SchemaField struct {
	Name string
	Type string
	Kind uint32
	Size int
}

// SchemaHash returns a deterministic FNV-1a hash for a mapped variable group.
// Field order is significant because it also determines the root layout.
func SchemaHash(name string, fields []SchemaField) uint64 {
	h := fnv.New64a()
	writeSchemaPart(h, name)
	for _, field := range fields {
		writeSchemaPart(h, field.Name)
		writeSchemaPart(h, field.Type)
		writeSchemaPart(h, string(rune(field.Kind)))
		writeSchemaPart(h, string(rune(field.Size)))
	}
	return h.Sum64()
}

// ValidateSchemaHash rejects a file produced from a different mapped
// declaration before any pointer fixups or variable writes are performed.
func ValidateSchemaHash(expected, actual uint64) error {
	if expected != actual {
		return fmt.Errorf("memorymap: mapped schema hash mismatch (expected %016x, got %016x)", expected, actual)
	}
	return nil
}

func writeSchemaPart(h interface{ Write([]byte) (int, error) }, value string) {
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}
