package e2e_test

import "testing"

func TestE2EStdPromotedValueLibraries(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/path"
import "std/path/filepath"
import "std/sync/atomic"
import "std/syscall"
import "std/unicode"
import "std/unicode/utf8"

func printf(format string, ...) int

func main() int {
    value := int32(4)
    atomic.AddInt32(&value, 3)
    decoded, decodedSize := utf8.DecodeRune([]byte{72})
    printf("PATH=%s,FILEPATH=%s,EXT=%s,ATOMIC=%d,ERRNO=%d,UNICODE=%d:%d:%d:%d:%d\n",
        path.Base("src/main.hike"), filepath.Join("src", "main.hike"), filepath.Ext("main.hike"),
        value, int(syscall.ENOENT), unicode.IsLetter(65), unicode.IsDigit(49),
        unicode.IsSpace(32), decoded, decodedSize)
    return 0
}
`,
		ExpectedOut:  "PATH=main.hike,FILEPATH=src/main.hike,EXT=.hike,ATOMIC=7,ERRNO=2,UNICODE=1:1:1:72:1",
		ExpectedExit: 0,
	})
}

func TestE2EStdPromotedRegexp(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/regexp"

func printf(format string, ...) int

func main() int {
    re, err := regexp.Compile("^Hi")
    printf("ERROR=%d,MATCH=%d,STRING=%s\n", err == nil, re.MatchString("Hi"), re.String())
    return 0
}
`,
		ExpectedOut:  "ERROR=1,MATCH=1,STRING=^Hi",
		ExpectedExit: 0,
	})
}

func TestE2EStdPromotedReaderLibraries(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/bufio"
import "std/io"
import "std/io/ioutil"

func printf(format string, ...) int

type testReader struct { used bool }
func (r *testReader) Read(data []byte) (int, error) {
    if r.used { return 0, io.EOF }
    r.used = true
    data[0] = 72
    data[1] = 105
    return 2, nil
}

func main() int {
    reader := &testReader{}
    all, allErr := ioutil.ReadAll(reader)
    scanner := bufio.NewScanner(cstring("first\nsecond"))
    scan := scanner.Scan()
    printf("READ=%s:%d,SCAN=%d:%s\n", string(all), allErr == nil, scan, scanner.Text())
    return 0
}
`,
		ExpectedOut:  "READ=Hi:1,SCAN=1:first",
		ExpectedExit: 0,
	})
}

// 追加された標準配置の互換APIが解決・呼び出し可能であることを検証する。
func TestE2EStdPromotedCompatibilitySurface(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/archive/zip"
import "std/embed"
import "std/os/exec"
import "std/runtime"

func printf(format string, ...) int

func main() int {
    cmd := exec.Command("hikec", "run")
    looked, lookErr := exec.LookPath(cmd.Path)
    archive, archiveErr := zip.OpenReader("virtual.zip")
    closeErr := archive.Close()
    fs := embed.FS{}
    printf("EXEC=%s:%d,RUNTIME=%s/%s,ZIP=%d:%d,EMBED=%d\n",
        looked, lookErr == nil, runtime.GOOS, runtime.GOARCH,
        archiveErr == nil, closeErr == nil, fs != embed.FS{})
    return 0
}
`,
		ExpectedOut:  "EXEC=hikec:1,RUNTIME=unknown/unknown,ZIP=1:1,EMBED=0",
		ExpectedExit: 0,
	})
}
