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
		ExpectedOut:  "EXEC=hikec:1,RUNTIME=unknown/unknown,ZIP=0:1,EMBED=0",
		ExpectedExit: 0,
	})
}

func TestE2EStdEncodingBase32(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/encoding/base32"

func printf(format string, ...) int

func main() int {
    empty := base32.StdEncoding.EncodeToString([]byte{})
    encoded32 := base32.StdEncoding.EncodeToString([]byte{'f', 'o', 'o'})
    prefix32 := base32.StdEncoding.EncodeToStringPrefix([]byte{'f', 'o', 'o'}, 5)
    printf("B32=%s:%s:%s\n", empty, encoded32, prefix32)
    return 0
}
`,
		ExpectedOut:  "B32=:MZXW6===:MZXW6",
		ExpectedExit: 0,
	})
}

func TestE2EStdEncodingBase64(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/encoding/base64"

func printf(format string, ...) int

func main() int {
    empty := base64.StdEncoding.EncodeToString([]byte{})
    one := base64.StdEncoding.EncodeToString([]byte{'f'})
    two := base64.StdEncoding.EncodeToString([]byte{'f', 'o'})
    encoded := base64.StdEncoding.EncodeToString([]byte{'M', 'a', 'n'})
    decoded, err := base64.StdEncoding.DecodeString("TWE=")
    _, badErr := base64.StdEncoding.DecodeString("bad")
    printf("B64=%s:%s:%s:%s:%s:%d:%d\n", empty, one, two, encoded, string(decoded), err == nil, badErr != nil)
    return 0
}
`,
		ExpectedOut:  "B64=:Zg==:Zm8=:TWFu:Ma:1:1",
		ExpectedExit: 0,
	})
}

func TestE2EStdJSONSet(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/encoding/json"

func printf(format string, ...) int

func main() int {
    obj := json.NewObject()
    json.SetValue(obj, "answer", json.NewNumber(1))
    json.SetValue(obj, "answer", json.NewNumber(2))
    json.SetValue(obj, "name", json.NewString("hike"))
    printf("%s\n", json.Stringify(obj))
    return 0
}
`,
		ExpectedOut:  "{\"answer\": 2, \"name\": \"hike\"}",
		ExpectedExit: 0,
	})
}

func TestE2EStdMathRand(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/math/rand"

func printf(format string, ...) int

func main() int {
    rand.Seed(7)
    a := rand.Int31n(1000)
    b := rand.Int31n(1000)
    rand.Seed(7)
    c := rand.Int31n(1000)
    d := rand.Int31n(1000)
    printf("%d:%d:%d:%d:%d\n", a == c, b == d, a >= 0, a < 1000, b < 1000)
    return 0
}
`,
		ExpectedOut:  "1:1:1:1:1",
		ExpectedExit: 0,
	})
}

func TestE2EStdURLPathUnescape(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/net/url"

func printf(format string, ...) int

func main() int {
    decoded, err := url.PathUnescape("a%2Fb+value%21")
    _, badErr := url.PathUnescape("%ZZ")
    printf("%s:%d:%d\n", decoded, err == nil, badErr != nil)
    return 0
}
`,
		ExpectedOut:  "a/b value!:1:1",
		ExpectedExit: 0,
	})
}

func TestE2EStdNetParsing(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/net"

func printf(format string, ...) int

func main() int {
    host, port, err := net.SplitHostPort("[::1]:8080")
    v4 := net.ParseIP("127.0.0.1")
    v6 := net.ParseIP("::1")
    mapped := net.ParseIP("::ffff:192.0.2.1")
    printf("%s:%s:%d:%d:%d:%d:%d:%s\n",
        host, port, err == nil, v4.IsLoopback(), v6.IsUnspecified(),
        mapped.To4().IsLoopback(), net.ParseIP("169.254.1.1").IsLinkLocalUnicast(),
        net.JoinHostPort("::1", "80"))
    return 0
}
`,
		ExpectedOut:  "::1:8080:1:1:0:0:1:[::1]:80",
		ExpectedExit: 0,
	})
}

func TestE2EStdFmtPrintFloat(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		GoHike: true,
		Source: `
package main

import "std/fmt"

func main() int {
    fmt.PrintFloat(3.125)
    return 0
}
`,
		ExpectedOut:  "3.125",
		ExpectedExit: 0,
	})
}
