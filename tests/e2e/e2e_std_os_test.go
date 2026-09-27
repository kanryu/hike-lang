package e2e_test

import (
	"runtime"
	"testing"
)

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

// os.File の位置操作、ReadAt、Stat、および Rename をネイティブ実装で検証する。
func TestE2EStdOS_FilePositionAndMetadata(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/os"

func printf(format string, ...) int

func main() int {
    path := "std-os-position.txt"
    renamed := "std-os-position-renamed.txt"
    os.WriteFile(path, []byte{97, 98, 99, 100, 101, 102}, 0644)

    file, openErr := os.Open(path)
    file.Seek(2, 0)
    current := make([]byte, 2)
    read, readErr := file.Read(current)
    at := make([]byte, 2)
    readAt, readAtErr := file.ReadAt(at, 1)
    position, seekErr := file.Seek(0, 1)
    info, statErr := file.Stat()
    file.Close()
    renameErr := os.Rename(path, renamed)
    removeErr := os.Remove(renamed)

    printf("OPEN=%d,READ=%d,TEXT=%s,READAT=%d,AT=%s,POS=%d,SIZE=%d,ERRORS=%d\n",
        openErr == nil, read, string(current[0:read]), readAt,
        string(at[0:readAt]), position, info.Size(),
        readErr == nil && readAtErr == nil && seekErr == nil && statErr == nil && renameErr == nil && removeErr == nil)
    return 0
}
`,
		ExpectedOut:  "OPEN=1,READ=2,TEXT=cd,READAT=2,AT=bc,POS=4,SIZE=6,ERRORS=1",
		ExpectedExit: 0,
	})
}

// ネイティブCランタイムに接続された環境情報とプロセス情報を検証する。
func TestE2EStdOS_NativeEnvironment(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/os"

func printf(format string, ...) int

func main() int {
    path, found := os.LookupEnv("PATH")
    cwd, cwdErr := os.Getwd()
    temp := os.TempDir()
    pid := os.Getpid()
    printf("PATH=%d,CWD=%d,TEMP=%d,PID=%d,PATHLEN=%d\n",
        found && len(path) > 0, cwdErr == nil && len(cwd) > 0,
        len(temp) > 0, pid > 0, len(path))
	    return 0
	}
	`,
		ExpectedOutRegex: `^PATH=1,CWD=1,TEMP=1,PID=1,PATHLEN=[0-9]+$`,
		ExpectedExit:     0,
	})
}

// Windows では FindFirstFileA/FindNextFileA による一階層列挙を実機検証する。
func TestE2EStdOS_WindowsFindFiles(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Win32 file enumeration is only available on Windows")
	}
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/os"

func printf(format string, ...) int

func main() int {
    entries, err := os.ReadDirWindows(".")
    hasMain := false
    for i := 0; i < len(entries); i++ {
        if entries[i].Name() == "main.hike" { hasMain = true }
    }
    printf("READDIR=%d,HAS_MAIN=%d,ERROR=%d\n", len(entries) > 0, hasMain, err == nil)
	    return 0
	}
	`,
		ExpectedOut:  "READDIR=1,HAS_MAIN=1,ERROR=1",
		ExpectedExit: 0,
	})
}

// Win32のディレクトリ、パス、環境変数、改名・削除APIをまとめて検証する。
func TestE2EStdOS_WindowsNativeFileAndEnvironment(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Win32 APIs are only available on Windows")
	}
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/os"

func printf(format string, ...) int

func main() int {
    root := "std-os-win-native"
    nested := root + "\\nested"
    file := nested + "\\file.txt"
    renamed := nested + "\\renamed.txt"
    os.RemoveAllWindows(root)
    mkdirErr := os.MkdirAllWindows(nested, 0755)
    writeErr := os.WriteFile(file, []byte{87, 105, 110, 51, 50}, 0644)
    renameErr := os.RenameWindows(file, renamed)
    setErr := os.SetenvWindows("HIKE_STDOS_TEST", "ok")
    env, found := os.GetenvWindows("HIKE_STDOS_TEST")
    expanded := os.ExpandEnvWindows("%HIKE_STDOS_TEST%")
    cwd, cwdErr := os.GetwdWindows()
    temp := os.TempDirWindows()
    exe, exeErr := os.ExecutableWindows()
    removeErr := os.RemoveAllWindows(root)
    printf("MKDIR=%d,WRITE=%d,RENAME=%d,ENV=%d:%s,EXPAND=%s,CWD=%d,TEMP=%d,EXE=%d,REMOVE=%d\n",
        mkdirErr == nil, writeErr == nil, renameErr == nil,
        found, env, expanded, cwdErr == nil && len(cwd) > 0,
        len(temp) > 0, exeErr == nil && len(exe) > 0, removeErr == nil)
    return 0
}
`,
		ExpectedOut:  "MKDIR=1,WRITE=1,RENAME=1,ENV=1:ok,EXPAND=ok,CWD=1,TEMP=1,EXE=1,REMOVE=1",
		ExpectedExit: 0,
	})
}
