package e2e_test

import "testing"

// 1. ポーリング型イベントディスパッチャの検証（非同期タスクの完了監視と集約）
func TestEventLoop_PollingDispatcher(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int
func Sleep(ms int)

func main() int {
    t1 := Async(func() int {
        Sleep(20)
        return 100
    })
    t2 := Async(func() int {
        Sleep(30)
        return 200
    })

    // イベントループ: 10msごとにポーリングして結果を回収
    Sleep(10)
    r1 := <-t1
    Sleep(10)
    r2 := <-t2

    total := r1 + r2
    printf("EVENT_LOOP_DONE=%d\n", total)
    return 0
}
`,
		ExpectedOut:  "EVENT_LOOP_DONE=300",
		ExpectedExit: 0,
	})
}

// 2. Tickベースのタイマー駆動ステートマシン検証
func TestEventLoop_TickStateMachine(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int
func Sleep(ms int)

type StateMachine struct {
    State int
    Ticks int
}

func (sm *StateMachine) Step() {
    sm.Ticks = sm.Ticks + 1
    if sm.State == 0 {
        sm.State = 1
    } else if sm.State == 1 {
        sm.State = 2
    } else if sm.State == 2 {
        sm.State = 3
    }
}

func main() int {
    sm := StateMachine{State: 0, Ticks: 0}

    // イベントループ: 1回のTickあたり15msスリープ、State=3で正常終了
    for sm.State < 3 {
        Sleep(15)
        sm.Step()
    }

    printf("STATE_FINAL=%d,TICKS=%d\n", sm.State, sm.Ticks)
    return 0
}
`,
		ExpectedOut:  "STATE_FINAL=3,TICKS=3",
		ExpectedExit: 0,
	})
}

// 3. 複数非同期ワーカーとイベント合流（Fork-Join & Sleep）の検証
func TestEventLoop_ConcurrentWorkers(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int
func Sleep(ms int)

func worker(id int, sleepMs int) int {
    Sleep(sleepMs)
    return id * 10
}

func main() int {
    // 3つのワーカーを同時にスレッドプールへ投入
    w1 := Async(func() int {
        return worker(1, 20)
    })
    w2 := Async(func() int {
        return worker(2, 30)
    })
    w3 := Async(func() int {
        return worker(3, 10)
    })

    // イベントループ側で各ワーカーの完了を順次待機・合流
    r1 := <-w1
    r2 := <-w2
    r3 := <-w3

    sum := r1 + r2 + r3
    printf("WORKER1=%d,WORKER2=%d,WORKER3=%d,SUM=%d\n", r1, r2, r3, sum)
    return 0
}
`,
		ExpectedOut:  "WORKER1=10,WORKER2=20,WORKER3=30,SUM=60",
		ExpectedExit: 0,
	})
}

// 4. 指定時刻到達による遅延タスク発火（Delayed Dispatcher）の検証
func TestEventLoop_DelayedTaskExecution(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int
func Sleep(ms int)

func main() int {
    tickCount := 0
    triggeredAt := -1

    // 1回20msのスリープを挟みながらTickを監視
    for tickCount < 4 {
        Sleep(20)
        tickCount = tickCount + 1
        if tickCount == 2 {
            triggeredAt = tickCount
        }
    }

    printf("TRIGGERED_AT=%d,FINAL_TICKS=%d\n", triggeredAt, tickCount)
    return 0
}
`,
		ExpectedOut:  "TRIGGERED_AT=2,FINAL_TICKS=4",
		ExpectedExit: 0,
	})
}

// 5. 標準ライブラリ eventloop によるメインスレッド同期実行（<-InvokeCh による待機初期化構文）検証
func TestConcurrent_EventLoop_InvokeCh(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/eventloop"

func printf(format string, ...) int

var mainDeviceID int = 42

func main() int {
    eventloop.Init(16)

    worker := Async(func() int {
        // <-InvokeCh による待機初期化構文（メインスレッドで実行させて値を受信）
        val := <-eventloop.InvokeCh[int](func() int {
            return mainDeviceID * 10
        })

        // ループ停止タスクを投入
        eventloop.Stop()

        return val
    })

    // メインスレッドでイベントループを実行（ブロッキング）
    eventloop.Run()

    res := <-worker
    printf("EV_RESULT=%d\n", res)
    return 0
}
`,
		ExpectedOut:  "EV_RESULT=420",
		ExpectedExit: 0,
	})
}

// 6. eventloop への複数タスク先行投入とパイプライン回収検証
func TestConcurrent_EventLoop_Pipeline(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/eventloop"

func printf(format string, ...) int

var counter int = 100

func main() int {
    eventloop.Init(16)

    worker := Async(func() int {
        // メインスレッドへ2つの処理を先行投入 (Pipeline)
        chA := eventloop.InvokeCh[int](func() int {
            counter = counter + 5
            return counter
        })
        chB := eventloop.InvokeCh[int](func() int {
            counter = counter * 2
            return counter
        })

        // 順次待機して回収
        a := <-chA
        b := <-chB

        eventloop.Stop()
        return a + b
    })

    eventloop.Run()

    total := <-worker
    printf("PIPELINE_TOTAL=%d\n", total)
    return 0
}
`,
		ExpectedOut:  "PIPELINE_TOTAL=315",
		ExpectedExit: 0,
	})
}
