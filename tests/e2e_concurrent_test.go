package e2e_test

import "testing"

// 1. <-Async による即時Joinと多値のアンパック検証
func TestConcurrent_ImmediateJoinAndUnpack(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func compute(a int, b int) (int, int) {
    return a + b, a * b
}

func main() int {
    sum, mul := <-Async(func() (int, int) {
        return compute(15, 3)
    })
    printf("SUM=%d,MUL=%d\n", sum, mul)
    return 0
}
`,
		ExpectedOut:  "SUM=18,MUL=45",
		ExpectedExit: 0,
	})
}

// 2. 矢印なし Async による並行タスク投入と <- による合流（Fork-Join）検証
func TestConcurrent_ForkJoin(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    task1 := Async(func() int {
        return 10 * 2
    })
    task2 := Async(func() int {
        return 30 + 4
    })

    r1 := <-task1
    r2 := <-task2

    printf("R1=%d,R2=%d,TOTAL=%d\n", r1, r2, r1 + r2)
    return 0
}
`,
		ExpectedOut:  "R1=20,R2=34,TOTAL=54",
		ExpectedExit: 0,
	})
}

// 3. ワーカースレッド内での <-Async 待機と、同一スコープ内での継続処理（Continuation）検証
func TestConcurrent_NestedContinuation(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func runOrchestrator() int {
    // ワーカースレッド側でサブタスクを同期受信
    sub1 := <-Async(func() int {
        return 50
    })
    sub2 := <-Async(func() int {
        return 70
    })

    // 同じ関数スコープ内で結果を評価・集計して返却
    return sub1 + sub2
}

func main() int {
    // オーケストレータ自体をバックグラウンドに逃がす
    orchTask := Async(func() int {
        return runOrchestrator()
    })

    res := <-orchTask
    printf("ORCH_RESULT=%d\n", res)
    return 0
}
`,
		ExpectedOut:  "ORCH_RESULT=120",
		ExpectedExit: 0,
	})
}

// 4. 外側スコープの変数キャプチャ（クロージャ環境）を伴う並行計算検証
func TestConcurrent_ClosureCapture(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    base := 100
    multiplier := 3

    val := <-Async(func() int {
        return base * multiplier + 24
    })

    printf("CAPTURED=%d\n", val)
    return 0
}
`,
		ExpectedOut:  "CAPTURED=324",
		ExpectedExit: 0,
	})
}
