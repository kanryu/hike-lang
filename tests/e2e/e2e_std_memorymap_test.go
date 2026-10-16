package e2e_test

import (
	"runtime"
	"testing"
)

// TestE2EStdMemoryMapWindowsPrivateShared covers only the ordinary mapping
// API. Mapped-variable serialization has a separate compiler-intrinsic test.
func TestE2EStdMemoryMapWindowsPrivateShared(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("std/memorymap A2A test requires Windows Win32 APIs")
	}
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/memorymap"
import "std/os"

func printf(format string, ...) int

func main() int {
    path := "std-memorymap-a2a.hmb"
    source := []byte{10, 20, 30, 40}
    serializeErr := memorymap.Serialize(path, source, []int{0, 2})
    info, statErr := os.Stat(path)
    fileSize := 32 + len(source) + 16
    view, mapErr := memorymap.Map(path, fileSize)
    first := byte(0)
    restored := byte(0)
    isolated := false
	var data cbuffer
    if view != nil {
		data = view.Data()
		first = data[0]
        restored = *view.Pointer(2)
		data[0] = 99
        isolated = true
    }
    closeErr := view.Close()
    loaded, readErr := os.ReadFile(path)
    serializeSharedErr := memorymap.Serialize(path, source, []int{0, 2})
    _, sharedStatErr := os.Stat(path)
    sharedView, sharedMapErr := memorymap.MapShared(path, fileSize)
    sharedWrite := false
    var sharedCloseErr error
    if sharedView != nil {
		sharedData := sharedView.Data()
		sharedData[0] = 77
        sharedWrite = true
        sharedCloseErr = sharedView.Close()
    }
    sharedLoaded, sharedReadErr := os.ReadFile(path)
    removeErr := os.Remove(path)
	printf("SERIALIZE=%d,STAT=%d,MAP=%d,FIRST=%d,OFFSET=%d,PRIVATE=%d,CLOSE=%d,FILE=%d,SHARED_SERIALIZE=%d,SHARED_STAT=%d,SHARED_MAP=%d,SHARED_WRITE=%d,SHARED_CLOSE=%d,SHARED_FILE=%d,REMOVE=%d\n",
        serializeErr == nil, statErr == nil, mapErr == nil,
        first, restored, isolated && readErr == nil && loaded[32] == 10,
		closeErr == nil, readErr == nil && loaded[34] == 30,
		serializeSharedErr == nil, sharedStatErr == nil, sharedMapErr == nil,
        sharedWrite, sharedCloseErr == nil,
		sharedReadErr == nil && sharedLoaded[32] == 77, removeErr == nil)
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,STAT=1,MAP=1,FIRST=10,OFFSET=30,PRIVATE=1,CLOSE=1,FILE=1,SHARED_SERIALIZE=1,SHARED_STAT=1,SHARED_MAP=1,SHARED_WRITE=1,SHARED_CLOSE=1,SHARED_FILE=1,REMOVE=1",
		ExpectedExit: 0,
	})
}
