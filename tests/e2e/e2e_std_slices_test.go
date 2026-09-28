package e2e_test

import "testing"

// slices の独立した操作を一つのプログラムでまとめて確認する。
func TestStd_Slices_Batch(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/slices"

func printf(format string, ...)

func main() int {
    nums := []int{5, 2, 8, 1, 9, 4}
    evens := slices.Filter[int](nums, func(x int) bool { return x % 2 == 0 })
    printf("CASE=filter:%d,%d,%d,%d\n", len(evens), evens[0], evens[1], evens[2])

    mapped := slices.Map[int, int]([]int{2, 8, 4}, func(x int) int { return x * 10 })
    printf("CASE=map:%d,%d,%d\n", mapped[0], mapped[1], mapped[2])

    sortable := []int{5, 2, 8, 1, 9, 4}
    slices.SortFunc[int](sortable, func(a int, b int) int { return a - b })
    printf("CASE=sort:%d,%d,%d,%d,%d,%d\n", sortable[0], sortable[1], sortable[2], sortable[3], sortable[4], sortable[5])

    searchable := []int{1, 2, 4, 5, 8, 9}
    found, ok := slices.Find[int](searchable, func(x int) bool { return x > 5 })
    printf("CASE=find:%d,%d\n", found, ok)
    printf("CASE=index:%d\n", slices.IndexFunc[int](searchable, func(x int) bool { return x > 5 }))

    buf := []byte{10, 20, 30, 40}
    first := &buf[0]
    second := &buf[1]
    *first = 101
    *second = 202
    printf("CASE=address:%d,%d,%d,%d\n", buf[0], buf[1], buf[2], buf[3])
    return 0
}
`,
		ExpectedOut:  "CASE=filter:3,2,8,4\nCASE=map:20,80,40\nCASE=sort:1,2,4,5,8,9\nCASE=find:8,1\nCASE=index:4\nCASE=address:101,202,30,40",
		ExpectedExit: 0,
	})
}
