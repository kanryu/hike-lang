package e2e_test

import "testing"

// AES-128 must retain the public API while selecting the AES-NI implementation
// on amd64 and the portable implementation on other targets.
func TestE2EStd_CryptoAESHardwareDispatch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/crypto/aes"

func printf(format string, ...) int

func main() int {
    key := []byte{0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15}
    plain := []byte{0,17,34,51,68,85,102,119,136,153,170,187,204,221,238,255}
    encrypted := make([]byte, 16)
    aes.NewCipher(key).Encrypt(encrypted, plain)
    printf("%d,%d,%d,%d\n", encrypted[0], encrypted[1], encrypted[14], encrypted[15])
    return 0
}
`,
		ExpectedOut:  "105,196,197,90",
		ExpectedExit: 0,
	})
}
