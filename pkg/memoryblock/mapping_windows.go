//go:build windows && !go_hike

package memoryblock

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	createFileMap = kernel32.NewProc("CreateFileMappingW")
	mapView       = kernel32.NewProc("MapViewOfFile")
	unmapView     = kernel32.NewProc("UnmapViewOfFile")
)

// DeserializeMapped opens path and maps it with FILE_MAP_COPY. The source
// file is never modified, even when callers mutate Mapping.Data.
func DeserializeMapped(path string) (*Mapping, error) {
	// PAGE_WRITECOPY only needs a readable source handle. FILE_MAP_COPY then
	// gives the process a private writable view without write-through access.
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
	h, _, callErr := createFileMap.Call(uintptr(f.Fd()), 0, 0x08 /* PAGE_WRITECOPY */, 0, 0, 0)
	if h == 0 {
		f.Close()
		return nil, callErr
	}
	addr, _, callErr := mapView.Call(h, 0x0001 /* FILE_MAP_COPY */, 0, 0, 0)
	if addr == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		f.Close()
		return nil, callErr
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(addr)), int(info.Size()))
	payload, relocs, count, err := decodeHeader(b)
	if err != nil {
		unmapView.Call(addr)
		syscall.CloseHandle(syscall.Handle(h))
		f.Close()
		return nil, err
	}
	offsets := make([]uint64, count)
	for i := range offsets {
		offsets[i] = binary.LittleEndian.Uint64(relocs[i*8:])
	}
	m := &Mapping{Data: payload, Pointers: offsets}
	m.close = func() error {
		_, _, e1 := unmapView.Call(addr)
		e2 := syscall.CloseHandle(syscall.Handle(h))
		e3 := f.Close()
		if e1 != nil && e1.Error() != "The operation completed successfully." {
			return e1
		}
		if e2 != nil {
			return e2
		}
		return e3
	}
	return m, nil
}
