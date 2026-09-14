package e2e_test

import "testing"

// std/crypto のハッシュ識別子と std/crypto/subtle のconstant-time APIを検証する。
func TestE2EStd_Crypto(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/crypto"
import "std/crypto/subtle"
import "std/crypto/sha256"
import "std/crypto/md5"

func printf(format string, ...) int

func main() int {
    left := []byte{65, 66, 67, 68}
    same := []byte{65, 66, 67, 68}
    other := []byte{65, 66, 67, 69}
    selected := subtle.ConstantTimeSelect(1, 42, 7)
    fallback := subtle.ConstantTimeSelect(0, 42, 7)
    copied := []byte{1, 2, 3, 4}
    subtle.ConstantTimeCopy(1, copied, []byte{9, 8, 7, 6})
    unchanged := []byte{1, 2, 3, 4}
    subtle.ConstantTimeCopy(0, unchanged, []byte{9, 8, 7, 6})
    digest := sha256.Sum256([]byte{97, 98, 99})
    md5Digest := md5.Sum([]byte{97, 98, 99})

    printf("HASH=%d,%d,%d,%d,%d,%d,%d,%d ",
        crypto.Size(crypto.MD5), crypto.Size(crypto.SHA1), crypto.Size(crypto.SHA224),
        crypto.Size(crypto.SHA256), crypto.Size(crypto.SHA384), crypto.Size(crypto.SHA512),
        crypto.Size(crypto.MD5SHA1), crypto.Size(crypto.BLAKE2b_512))
    printf("COMPARE=%d,%d,%d BYTE=%d,%d EQ=%d,%d LE=%d,%d ",
        subtle.ConstantTimeCompare(left, same),
        subtle.ConstantTimeCompare(left, other),
        subtle.ConstantTimeCompare(left, []byte{65}),
        subtle.ConstantTimeByteEq(byte(7), byte(7)),
        subtle.ConstantTimeByteEq(byte(7), byte(8)),
        subtle.ConstantTimeEq(int32(123), int32(123)),
        subtle.ConstantTimeEq(int32(123), int32(124)),
        subtle.ConstantTimeLessOrEq(3, 3),
        subtle.ConstantTimeLessOrEq(4, 3))
    printf("SELECT=%d,%d COPY=%d,%d,%d,%d UNCHANGED=%d,%d,%d,%d\n",
        selected, fallback,
        copied[0], copied[1], copied[2], copied[3],
        unchanged[0], unchanged[1], unchanged[2], unchanged[3])
    printf("SHA256=%d,%d,%d,%d,%d,%d,%d,%d\n",
        digest[0], digest[1], digest[2], digest[3],
        digest[4], digest[5], digest[6], digest[7])
    printf("MD5=%d,%d,%d,%d,%d,%d,%d,%d\n",
        md5Digest[0], md5Digest[1], md5Digest[2], md5Digest[3],
        md5Digest[4], md5Digest[5], md5Digest[6], md5Digest[7])
    return 0
}
`,
		ExpectedOut: "HASH=16,20,28,32,48,64,36,64 COMPARE=1,0,0 BYTE=1,0 EQ=1,0 LE=1,0 SELECT=42,7 COPY=9,8,7,6 UNCHANGED=1,2,3,4\nSHA256=186,120,22,191,143,1,207,234\nMD5=144,1,80,152,60,210,79,176",
		ExpectedExit: 0,
	})
}
