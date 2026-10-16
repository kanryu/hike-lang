package e2e_test

import (
	"runtime"
	"testing"
)

// std/memorymap は Windows の Win32 file-mapping API を使うため、実機
// A2A テストは Windows でだけ実行する。FILE_MAP_COPY による変更の隔離と、
// シリアライズされた相対ポインタの復元を同時に検証する。
func TestE2EStdMemoryMapWindowsPrivateRelocation(t *testing.T) {
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
	apiPath := "std-memorymap-api.hmb"
	apiSerializeErr := memorymap.MappedSerialize(view, apiPath)
	if view != nil {
		data = view.Data()
		data[0] = 201
		data[2] = 202
	}
	apiDeserializeErr := memorymap.MappedDeserialize(view, apiPath)
	restoredAfterDeserialize := false
	if view != nil {
		data = view.Data()
		restoredAfterDeserialize = data[0] == 99 && data[2] == 30
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
	apiRemoveErr := os.Remove(apiPath)
	schemaPath := "std-memorymap-schema.hmb"
	schemaSerializeErr := memorymap.SerializeWithSchema(schemaPath, source, []int{0, 2}, 111)
	_, schemaMismatchErr := memorymap.MapWithSchema(schemaPath, fileSize, 222)
	schemaRemoveErr := os.Remove(schemaPath)
	printf("SERIALIZE=%d,STAT=%d,MAP=%d,FIRST=%d,OFFSET=%d,PRIVATE=%d,API_SERIALIZE=%d,API_DESTROY=1,API_DESERIALIZE=%d,RESTORED=%d,CLOSE=%d,FILE=%d,SCHEMA_SERIALIZE=%d,SCHEMA_MISMATCH=%d,SCHEMA_REMOVE=%d,SHARED_SERIALIZE=%d,SHARED_STAT=%d,SHARED_MAP=%d,SHARED_WRITE=%d,SHARED_CLOSE=%d,SHARED_FILE=%d,REMOVE=%d,API_REMOVE=%d\n",
        serializeErr == nil, statErr == nil, mapErr == nil,
        first, restored, isolated && readErr == nil && loaded[32] == 10,
		apiSerializeErr == nil, apiDeserializeErr == nil, restoredAfterDeserialize, closeErr == nil, readErr == nil && loaded[34] == 30,
		schemaSerializeErr == nil, schemaMismatchErr != nil, schemaRemoveErr == nil,
		serializeSharedErr == nil, sharedStatErr == nil, sharedMapErr == nil,
        sharedWrite, sharedCloseErr == nil,
		sharedReadErr == nil && sharedLoaded[32] == 77, removeErr == nil, apiRemoveErr == nil)
    return 0
}
`,
		ExpectedOut:  "SERIALIZE=1,STAT=1,MAP=1,FIRST=10,OFFSET=30,PRIVATE=1,API_SERIALIZE=1,API_DESTROY=1,API_DESERIALIZE=1,RESTORED=1,CLOSE=1,FILE=1,SCHEMA_SERIALIZE=1,SCHEMA_MISMATCH=1,SCHEMA_REMOVE=1,SHARED_SERIALIZE=1,SHARED_STAT=1,SHARED_MAP=1,SHARED_WRITE=1,SHARED_CLOSE=1,SHARED_FILE=1,REMOVE=1,API_REMOVE=1",
		ExpectedExit: 0,
	})
}
