package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var hikecBin string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "hikec-test-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "一時ディレクトリ作成失敗: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	binName := "hikec"
	if runtime.GOOS == "windows" {
		binName = "hikec.exe"
	}
	hikecBin = filepath.Join(tmpDir, binName)

	buildCmd := exec.Command("go", "build", "-o", hikecBin, "../cmd/hikec")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "hikec のビルドに失敗しました: %v\n%s\n", err, string(out))
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}

type TestCase struct {
	Name         string
	HikeSource   string
	ExpectedOut  string
	ExpectedExit int
}

func runTryRunTest(t *testing.T, tc TestCase) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "hike-test-*")
	if err != nil {
		t.Fatalf("一時ディレクトリ作成失敗: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "main.hike")
	if err := os.WriteFile(srcPath, []byte(tc.HikeSource), 0644); err != nil {
		t.Fatalf("ソース書き込み失敗: %v", err)
	}

	runCmd := exec.Command(hikecBin, "run", srcPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr

	err = runCmd.Run()

	actualExit := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			actualExit = exitErr.ExitCode()
		} else {
			t.Fatalf("バイナリ実行失敗: %v\n[Stderr]: %s", err, stderr.String())
		}
	}

	if actualExit != tc.ExpectedExit {
		t.Errorf("[%s] 終了コード不一致: 期待値 %d, 実際値 %d\n[Stderr]: %s", tc.Name, tc.ExpectedExit, actualExit, stderr.String())
	}

	actualOut := strings.TrimSpace(stdout.String())
	expectedOut := strings.TrimSpace(tc.ExpectedOut)
	if actualOut != expectedOut {
		t.Errorf("[%s] 出力不一致:\n期待値:\n%s\n実際値:\n%s\n[Stderr]: %s", tc.Name, expectedOut, actualOut, stderr.String())
	}
}

func TestHikeFeatures(t *testing.T) {
	tests := []TestCase{
		// -------------------------------------------------------------
		// 1. 算術・論理・ビット演算
		// -------------------------------------------------------------
		{
			Name: "Arithmetic and Logic Precedence",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    a := 10 + 20 * 3 - 5 / 2 % 3
    b := !(true && false) || (10 > 20)
    printf("A=%d,B=%d\n", a, b)
    return 0
}
`,
			ExpectedOut:  "A=68,B=1",
			ExpectedExit: 0,
		},
		{
			Name: "Bitwise Operations",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    a := 5  // 0101
    b := 3  // 0011
    andVal := a & b
    orVal := a | b
    xorVal := a ^ b
    shlVal := a << 2
    shrVal := a >> 1
    printf("AND=%d,OR=%d,XOR=%d,SHL=%d,SHR=%d\n", andVal, orVal, xorVal, shlVal, shrVal)
    return 0
}
`,
			ExpectedOut:  "AND=1,OR=7,XOR=6,SHL=20,SHR=2",
			ExpectedExit: 0,
		},
		{
			Name: "Compound Assignment and Increment",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    x := 10
    x += 5
    x -= 2
    x *= 3
    x++
    x--
    printf("X=%d\n", x)
    return 0
}
`,
			ExpectedOut:  "X=39",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 2. 制御構文 (if / for / switch)
		// -------------------------------------------------------------
		{
			Name: "If-Else with Initialization",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    if v := 15; v > 20 {
        printf("HIGH\n")
    } else if v > 10 {
        printf("MID=%d\n", v)
    } else {
        printf("LOW\n")
    }
    return 0
}
`,
			ExpectedOut:  "MID=15",
			ExpectedExit: 0,
		},
		{
			Name: "For Loop with Break and Continue",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    sum := 0
    for i := 0; i < 10; i = i + 1 {
        if i == 3 {
            continue
        }
        if i == 7 {
            break
        }
        sum = sum + i
    }
    printf("SUM=%d\n", sum)
    return 0
}
`,
			ExpectedOut:  "SUM=18",
			ExpectedExit: 0,
		},
		{
			Name: "Switch Multiple Cases and Default",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    val := 2
    switch val {
    case 1:
        printf("ONE\n")
    case 2, 3:
        printf("TWO_OR_THREE\n")
    default:
        printf("OTHER\n")
    }
    return 0
}
`,
			ExpectedOut:  "TWO_OR_THREE",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 3. 関数・再帰・多値返却・クロージャ
		// -------------------------------------------------------------
		{
			Name: "Multiple Return Values",
			HikeSource: `
package main

func printf(format string, ...) int

func swap(a int, b int) (int, int) {
    return b, a
}

func main() int {
    x, y := swap(100, 200)
    printf("X=%d,Y=%d\n", x, y)
    return 0
}
`,
			ExpectedOut:  "X=200,Y=100",
			ExpectedExit: 0,
		},
		{
			Name: "Recursive Fibonacci",
			HikeSource: `
package main

func printf(format string, ...) int

func fib(n int) int {
    if n <= 0 {
        return 0
    }
    if n == 1 {
        return 1
    }
    return fib(n - 1) + fib(n - 2)
}

func main() int {
    printf("FIB(10)=%d\n", fib(10))
    return 0
}
`,
			ExpectedOut:  "FIB(10)=55",
			ExpectedExit: 0,
		},
		{
			Name: "Closures and State Capture",
			HikeSource: `
package main

func printf(format string, ...) int

func makeCounter(start int) func() int {
    c := start
    return func() int {
        c = c + 1
        return c
    }
}

func main() int {
    cnt := makeCounter(10)
    v1 := cnt()
    v2 := cnt()
    printf("CNT=%d,%d\n", v1, v2)
    return 0
}
`,
			ExpectedOut:  "CNT=11,12",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 4. ポインタとメモリ操作
		// -------------------------------------------------------------
		{
			Name: "Pointer Referencing and Mutation",
			HikeSource: `
package main

func printf(format string, ...) int

func mutate(p *int) {
    *p = *p + 50
}

func main() int {
    x := 10
    mutate(&x)
    printf("X=%d\n", x)
    return 0
}
`,
			ExpectedOut:  "X=60",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 5. 構造体・メソッド・埋め込み
		// -------------------------------------------------------------
		{
			Name: "Struct Pointer Receiver Method",
			HikeSource: `
package main

func printf(format string, ...) int

type Counter struct {
    Val int
}

func (c *Counter) Inc(delta int) {
    c.Val = c.Val + delta
}

func (c *Counter) Get() int {
    return c.Val
}

func main() int {
    c := Counter{Val: 100}
    c.Inc(25)
    printf("VAL=%d\n", c.Get())
    return 0
}
`,
			ExpectedOut:  "VAL=125",
			ExpectedExit: 0,
		},
		{
			Name: "Struct Field Promotion via Embedding",
			HikeSource: `
package main

func printf(format string, ...) int

type Base struct {
    Id int
}

type Item struct {
    *Base
    Price int
}

func main() int {
    b := &Base{Id: 777}
    item := Item{Base: b, Price: 500}
    printf("ID=%d,PRICE=%d\n", item.Id, item.Price)
    return 0
}
`,
			ExpectedOut:  "ID=777,PRICE=500",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 6. 配列・スライス・for-range
		// -------------------------------------------------------------
		{
			Name: "Slice Dynamic Append and Cap",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    s := make([]int, 0, 4)
    s = append(s, 10, 20, 30)
    printf("LEN=%d,CAP=%d,AT1=%d\n", len(s), cap(s), s[1])
    return 0
}
`,
			ExpectedOut:  "LEN=3,CAP=4,AT1=20",
			ExpectedExit: 0,
		},
		{
			Name: "Array Slicing Operation",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    arr := [5]int{10, 20, 30, 40, 50}
    sl := arr[1:4]
    printf("LEN=%d,V0=%d,V2=%d\n", len(sl), sl[0], sl[2])
    return 0
}
`,
			ExpectedOut:  "LEN=3,V0=20,V2=40",
			ExpectedExit: 0,
		},
		{
			Name: "For Range Key and Value",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    arr := [3]string{"A", "B", "C"}
    for i, v := range arr {
        printf("%d:%s ", i, v)
    }
    printf("\n")
    return 0
}
`,
			ExpectedOut:  "0:A 1:B 2:C",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 7. 型キャスト (Type Casting)
		// -------------------------------------------------------------
		{
			Name: "Numeric Cast Between Float and Int",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    f := 12.85
    i := int(f)
    f2 := float64(i) + 0.5
    printf("INT=%d,FLOAT=%.1f\n", i, f2)
    return 0
}
`,
			ExpectedOut:  "INT=12,FLOAT=12.5",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 8. ジェネリクス (Generics)
		// -------------------------------------------------------------
		{
			Name: "Generic Function Monomorphization",
			HikeSource: `
package main

func printf(format string, ...) int

func Max[T](a T, b T) T {
    if a > b {
        return a
    }
    return b
}

func main() int {
    m1 := Max[int](10, 50)
    m2 := Max[float64](3.14, 1.41)
    printf("MAX_INT=%d,MAX_FLOAT=%.2f\n", m1, m2)
    return 0
}
`,
			ExpectedOut:  "MAX_INT=50,MAX_FLOAT=3.14",
			ExpectedExit: 0,
		},
		{
			Name: "Generic Struct Container",
			HikeSource: `
package main

func printf(format string, ...) int

type Box[T] struct {
    Value T
}

func main() int {
    b := Box[int]{Value: 999}
    printf("BOX=%d\n", b.Value)
    return 0
}
`,
			ExpectedOut:  "BOX=999",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 9. インターフェース・型アサーション
		// -------------------------------------------------------------
		{
			Name: "Interface Dynamic Dispatch",
			HikeSource: `
package main

func printf(format string, ...) int

type Greeter interface {
    Greet() string
}

type Robot struct {
    Model string
}

func (r *Robot) Greet() string {
    return r.Model
}

func main() int {
    var g Greeter = &Robot{Model: "RX-78"}
    printf("GREET=%s\n", g.Greet())
    return 0
}
`,
			ExpectedOut:  "GREET=RX-78",
			ExpectedExit: 0,
		},
		{
			Name: "Type Assertion from Any",
			HikeSource: `
package main

func printf(format string, ...) int

func main() int {
    var a any = 1234
    val, ok := a.(int)
    printf("VAL=%d,OK=%d\n", val, ok)
    return 0
}
`,
			ExpectedOut:  "VAL=1234,OK=1",
			ExpectedExit: 0,
		},

		// -------------------------------------------------------------
		// 10. プロセス終了コード (Exit Code)
		// -------------------------------------------------------------
		{
			Name: "Process Exit Code Propagation",
			HikeSource: `
package main

func main() int {
    return 42
}
`,
			ExpectedOut:  "",
			ExpectedExit: 42,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			runTryRunTest(t, tc)
		})
	}
}
