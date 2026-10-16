package memoryblock

import (
	"encoding/binary"
	"os"
)

// DeserializeMapped preserves the historical API name while using an owned
// byte buffer. Compiler-side memoryblock handling must not depend on an OS
// memory-mapping implementation.
func DeserializeMapped(path string) (*Mapping, error) {
	image, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	payload, relocs, count, err := decodeHeader(image)
	if err != nil {
		return nil, err
	}
	offsets := make([]uint64, count)
	for i := range offsets {
		offsets[i] = binary.LittleEndian.Uint64(relocs[i*8:])
	}
	data := append([]byte(nil), payload...)
	return &Mapping{Data: data, Pointers: offsets, close: func() error { return nil }}, nil
}
