package e2e_test

import "testing"

// amd64向けLLVM RDRAND intrinsicのビルド制約付き選択を検証する。
func TestE2EStd_CryptoRandIntrinsic(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/crypto/rand"

func printf(format string, ...) int

func main() int {
    value, success := rand.Uint32()
    printf("READY=%d,NONZERO=%d\n", success != 0, value != 0)
    return 0
}
`,
		ExpectedOut:  "READY=1,NONZERO=1",
		ExpectedExit: 0,
	})
}
