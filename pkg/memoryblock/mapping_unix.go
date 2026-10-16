//go:build !windows

package memoryblock

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
)

// DeserializeMapped uses MAP_PRIVATE on Unix. The fallback is intentionally
// not os.ReadFile: callers depend on copy-on-write mapping semantics.
func DeserializeMapped(path string) (*Mapping, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size() <= 0 {
		f.Close()
		return nil, fmt.Errorf("memoryblock: empty file")
	}
	b, err := syscall.Mmap(int(f.Fd()), 0, int(info.Size()), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
	f.Close()
	if err != nil {
		return nil, err
	}
	payload, relocs, count, err := decodeHeader(b)
	if err != nil {
		syscall.Munmap(b)
		return nil, err
	}
	offsets := make([]uint64, count)
	for i := range offsets {
		offsets[i] = binary.LittleEndian.Uint64(relocs[i*8:])
	}
	return &Mapping{Data: payload, Pointers: offsets, close: func() error { return syscall.Munmap(b) }}, nil
}
