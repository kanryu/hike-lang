// Package memoryblock implements the on-disk representation used by mapped
// memory blocks.  The payload is deliberately opaque: the compiler/runtime
// owns the layout, while this package only validates the container and fixes
// up pointer fields represented as offsets from the mapping base.
package memoryblock

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

const (
	magic       = "HMB1"
	version     = uint32(1)
	headerBytes = 32
)

// Mapping is a private, copy-on-write view of a serialized block. Data is
// writable but changes are never written back to the source file.
type Mapping struct {
	Data     []byte
	Pointers []uint64
	close    func() error
}

// Close releases the underlying mapping. It is safe to call more than once.
func (m *Mapping) Close() error {
	if m == nil || m.close == nil {
		return nil
	}
	err := m.close()
	m.close = nil
	m.Data = nil
	return err
}

// Serialize writes a memory block. pointers contains byte offsets into data;
// this is the relocation form stored on disk, never process addresses.
func Serialize(path string, data []byte, pointers []uint64) error {
	for _, p := range pointers {
		if p >= uint64(len(data)) {
			return fmt.Errorf("memoryblock: pointer offset %d outside payload of %d bytes", p, len(data))
		}
	}
	if uint64(len(data)) > ^uint64(0)-headerBytes {
		return errors.New("memoryblock: payload too large")
	}
	b := make([]byte, headerBytes+len(data)+len(pointers)*8)
	copy(b[:4], magic)
	binary.LittleEndian.PutUint32(b[4:8], version)
	binary.LittleEndian.PutUint64(b[8:16], uint64(len(data)))
	binary.LittleEndian.PutUint64(b[16:24], uint64(len(pointers)))
	copy(b[headerBytes:headerBytes+len(data)], data)
	pos := headerBytes + len(data)
	for _, p := range pointers {
		binary.LittleEndian.PutUint64(b[pos:pos+8], p)
		pos += 8
	}
	return os.WriteFile(path, b, 0o600)
}

// decodeHeader validates a mapped file and returns payload and relocation
// table boundaries. It intentionally accepts no trailing bytes.
func decodeHeader(b []byte) (payload, relocs []byte, count int, err error) {
	if len(b) < headerBytes || string(b[:4]) != magic {
		return nil, nil, 0, errors.New("memoryblock: invalid file magic")
	}
	if binary.LittleEndian.Uint32(b[4:8]) != version {
		return nil, nil, 0, errors.New("memoryblock: unsupported file version")
	}
	dataLen, n := binary.LittleEndian.Uint64(b[8:16]), binary.LittleEndian.Uint64(b[16:24])
	if dataLen > uint64(len(b)-headerBytes) || n > uint64((len(b)-headerBytes-int(dataLen))/8) {
		return nil, nil, 0, errors.New("memoryblock: truncated relocation table")
	}
	if headerBytes+int(dataLen)+int(n)*8 != len(b) {
		return nil, nil, 0, errors.New("memoryblock: unexpected trailing data")
	}
	return b[headerBytes : headerBytes+int(dataLen)], b[headerBytes+int(dataLen):], int(n), nil
}

// RestorePointers converts serialized offsets into process addresses.
func RestorePointers(base uintptr, offsets []uint64) []uintptr {
	result := make([]uintptr, len(offsets))
	for i, offset := range offsets {
		result[i] = base + uintptr(offset)
	}
	return result
}
