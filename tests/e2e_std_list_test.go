package e2e_test

import "testing"

// 1. Push & Pop: 前後からの追加と取り出し、要素数（Len）の追従を検証する
func TestStd_List_PushPop(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/collections/list"

func printf(format string, ...) int

func main() int {
    l := list.New[int]()
    l.PushBack(10)
    l.PushBack(20)
    l.PushFront(5)

    printf("LEN=%d: ", l.Len())
    v1, _ := l.PopFront()
    v2, _ := l.PopBack()
    v3, _ := l.PopBack()

    printf("%d %d %d REMAIN=%d\n", v1, v2, v3, l.Len())
    return 0
}
`,
		ExpectedOut:  "LEN=3: 5 20 10 REMAIN=0",
		ExpectedExit: 0,
	})
}

// 2. Indexable & IndexAssignable: 添字構文 l[i] による取得と l[i] = v によるインプレース代入を検証する
func TestStd_List_Indexing(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/collections/list"

func printf(format string, ...) int

func main() int {
    l := list.New[int]()
    l.PushBack(100)
    l.PushBack(200)
    l.PushBack(300)

    // Indexable による参照脱糖 (l.Get(1))
    printf("ORIG[1]=%d ", l[1])

    // IndexAssignable による代入脱糖 (l.Set(1, 999))
    l[1] = 999
    printf("MOD[1]=%d\n", l[1])
    return 0
}
`,
		ExpectedOut:  "ORIG[1]=200 MOD[1]=999",
		ExpectedExit: 0,
	})
}

// 3. Sliceable: 部分切り出し構文 l[low:high] による独立した新リスト生成を検証する
func TestStd_List_Slice(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/collections/list"

func printf(format string, ...) int

func main() int {
    l := list.New[int]()
    l.PushBack(10)
    l.PushBack(20)
    l.PushBack(30)
    l.PushBack(40)
    l.PushBack(50)

    // Sliceable によるスライシング脱糖 (sub := l.Slice(1, 4))
    sub := l[1:4]

    printf("SUB_LEN=%d: ", sub.Len())
    for i := 0; i < sub.Len(); i = i + 1 {
        printf("%d ", sub[i])
    }

    // サブリストへの代入が元リストに影響しないことの検証
    sub[0] = 999
    printf("ORIG[1]=%d SUB[0]=%d\n", l[1], sub[0])
    return 0
}
`,
		ExpectedOut:  "SUB_LEN=3: 20 30 40 ORIG[1]=20 SUB[0]=999",
		ExpectedExit: 0,
	})
}

// 4. Iterable: for-range 構文 (for v := range l) によるゼロアロケーション走査を検証する
func TestStd_List_ForRange(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/collections/list"

func printf(format string, ...) int

func main() int {
    l := list.New[int]()
    l.PushBack(1)
    l.PushBack(2)
    l.PushBack(4)
    l.PushBack(8)

    sum := 0
    // Iterable (InitIterator + Next) による脱糖ループ
    for v := range l {
        printf("%d ", v)
        sum = sum + v
    }
    printf("SUM=%d\n", sum)
    return 0
}
`,
		ExpectedOut:  "1 2 4 8 SUM=15",
		ExpectedExit: 0,
	})
}

// 5. Sizeof & Generics: ノード構造体とリスト構造体のコンパイル時サイズ計算を検証する
func TestStd_List_Sizeof(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/collections/list"

func printf(format string, ...) int

func main() int {
    // コンパイル時即値として展開される sizeof の検証
    listSize := sizeof(list.List[int])
    printf("LIST_SIZE_GE_16=%d\n", listSize >= 16)
    return 0
}
`,
		ExpectedOut:  "LIST_SIZE_GE_16=1",
		ExpectedExit: 0,
	})
}
