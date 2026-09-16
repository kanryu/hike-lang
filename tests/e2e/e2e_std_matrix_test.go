package e2e_test

import "testing"

// 標準ライブラリのMatrixを先頭要素ポインターから初期化し、二重添字を検証する。
func TestE2EStdMatrix(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `package main

import "std/collections/matrix"

func malloc(size int) *byte
func free(ptr *byte)
func printf(format string, ...) int

func main() int {
    var data *int = malloc(8 * 8 * 8)
    p := data + 0
    *p = 1
    p = data + 28
    *p = 29
    p = data + 63
    *p = 64

    m := matrix.New[int, 8, 8](data)
    first := m[0][0]
    middle := m[3][4]
    last := m[7][7]
    m[3][4] = 99
    printf("STD_MATRIX=%d,%d,%d;SET=%d\n", first, middle, last, m[3][4])

    free((*byte)(data))
    return 0
}
`,
		ExpectedOut:  "STD_MATRIX=1,29,64;SET=99",
		ExpectedExit: 0,
	})
}
