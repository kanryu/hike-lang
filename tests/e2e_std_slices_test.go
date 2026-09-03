package e2e_test

import "testing"

// 1. Filter: 条件に合致する要素を抽出する
func TestStd_Slices_Filter(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/slices"

func printf(format string, ...) int

func main() int {
    nums := []int{5, 2, 8, 1, 9, 4}
    evens := slices.Filter[int](nums, func(x int) bool {
        return x % 2 == 0
    })

    printf("LEN=%d: ", len(evens))
    for i := 0; i < len(evens); i = i + 1 {
        printf("%d ", evens[i])
    }
    printf("\n")
    return 0
}
`,
		ExpectedOut:  "LEN=3: 2 8 4",
		ExpectedExit: 0,
	})
}

// 2. Map: 全要素を関数によって射影・変換する
func TestStd_Slices_Map(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/slices"

func printf(format string, ...) int

func main() int {
    nums := []int{2, 8, 4}
    mapped := slices.Map[int, int](nums, func(x int) int {
        return x * 10
    })

    for i := 0; i < len(mapped); i = i + 1 {
        printf("%d ", mapped[i])
    }
    printf("\n")
    return 0
}
`,
		ExpectedOut:  "20 80 40",
		ExpectedExit: 0,
	})
}

// 3. SortFunc: 比較関数を用いてインプレース昇順ソートを行う
func TestStd_Slices_SortFunc(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/slices"

func printf(format string, ...) int

func main() int {
    nums := []int{5, 2, 8, 1, 9, 4}
    slices.SortFunc[int](nums, func(a int, b int) int {
        return a - b
    })

    for i := 0; i < len(nums); i = i + 1 {
        printf("%d ", nums[i])
    }
    printf("\n")
    return 0
}
`,
		ExpectedOut:  "1 2 4 5 8 9",
		ExpectedExit: 0,
	})
}

// 4. Find: 条件に一致する最初の要素と検出フラグを取得する
func TestStd_Slices_Find(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/slices"

func printf(format string, ...) int

func main() int {
    nums := []int{1, 2, 4, 5, 8, 9}
    foundVal, ok := slices.Find[int](nums, func(x int) bool {
        return x > 5
    })

    printf("VAL=%d,OK=%d\n", foundVal, ok)
    return 0
}
`,
		ExpectedOut:  "VAL=8,OK=1",
		ExpectedExit: 0,
	})
}

// 5. IndexFunc: 条件に一致する最初の要素のインデックスを特定する
func TestStd_Slices_IndexFunc(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/slices"

func printf(format string, ...) int

func main() int {
    nums := []int{1, 2, 4, 5, 8, 9}
    foundIdx := slices.IndexFunc[int](nums, func(x int) bool {
        return x > 5
    })

    printf("IDX=%d\n", foundIdx)
    return 0
}
`,
		ExpectedOut:  "IDX=4",
		ExpectedExit: 0,
	})
}
