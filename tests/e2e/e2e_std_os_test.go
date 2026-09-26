package e2e_test

import "testing"

// std/os の正式APIを通じたファイル作成・書き込み・読み込み・削除を検証する。
func TestE2EStdOS_ReadWriteFile(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/os"

func printf(format string, ...) int

func main() int {
    path := "std-os-read-write.txt"
    data := []byte{72, 105, 107, 101, 32, 79, 83}
    writeErr := os.WriteFile(path, data, 0644)
    loaded, readErr := os.ReadFile(path)
    removeErr := os.Remove(path)

    printf("WRITE_OK=%d,READ_OK=%d,REMOVE_OK=%d,LEN=%d,HEAD=%d,TAIL=%d\n",
        writeErr == nil, readErr == nil, removeErr == nil,
        len(loaded), loaded[0], loaded[len(loaded)-1])
    return 0
}
`,
		ExpectedOut:  "WRITE_OK=1,READ_OK=1,REMOVE_OK=1,LEN=7,HEAD=72,TAIL=83",
		ExpectedExit: 0,
	})
}

// os.File のオープン、Write、Read、Close を検証する。
func TestE2EStdOS_FileLifecycle(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/os"

func printf(format string, ...) int

func main() int {
    path := "std-os-file-lifecycle.txt"
    created, createErr := os.Create(path)
    written, writeErr := created.WriteString("native os")
    closeErr := created.Close()

    opened, openErr := os.Open(path)
    buffer := make([]byte, 32)
    read, readErr := opened.Read(buffer)
    secondCloseErr := opened.Close()
    removeErr := os.Remove(path)

    printf("CREATE_OK=%d,WRITE=%d,CLOSE_OK=%d,OPEN_OK=%d,READ=%d,TEXT=%s,READ_OK=%d,CLOSE2_OK=%d,REMOVE_OK=%d\n",
        createErr == nil, written, closeErr == nil, openErr == nil,
        read, string(buffer[0:read]), readErr == nil,
        secondCloseErr == nil, removeErr == nil)
    return 0
}
`,
		ExpectedOut:  "CREATE_OK=1,WRITE=9,CLOSE_OK=1,OPEN_OK=1,READ=9,TEXT=native os,READ_OK=1,CLOSE2_OK=1,REMOVE_OK=1",
		ExpectedExit: 0,
	})
}
