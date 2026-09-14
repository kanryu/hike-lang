package e2e_test

import "testing"

// std/bytes の検索、変換、分割、結合、トリム操作を一通り検証する。
func TestE2EStd_Bytes(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/bytes"

func printf(format string, ...) int

func main() int {
    source := []byte{32, 32, 97, 108, 112, 104, 97, 44, 98, 101, 116, 97, 44, 98, 101, 116, 97, 32, 32}
    beta := []byte{98, 101, 116, 97}
    comma := []byte{44}

    printf("EQUAL=%d,%d COMPARE=%d,%d,%d ",
        bytes.Equal(beta, []byte{98, 101, 116, 97}),
        bytes.Equal(beta, []byte{66, 69, 84, 65}),
        bytes.Compare([]byte{97}, []byte{98}),
        bytes.Compare([]byte{98}, []byte{98}),
        bytes.Compare([]byte{99}, []byte{98}))
    printf("INDEX=%d,LAST=%d,COUNT=%d,ANY=%d,LASTANY=%d ",
        bytes.Index(source, beta),
        bytes.LastIndex(source, beta),
        bytes.Count(source, beta),
        bytes.IndexAny(source, "be"),
        bytes.LastIndexAny(source, "ae"))

    before, after, found := bytes.Cut(source, comma)
    printf("CUT=%s|%s|%d ", string(before), string(after), found)

    replaced := bytes.ReplaceAll(source, beta, []byte{105, 116, 101, 109})
    limited := bytes.Replace(source, beta, []byte{88}, 1)
    repeated := bytes.Repeat([]byte{97, 98}, 3)
    upper := bytes.ToUpper([]byte{65, 98, 99, 45, 120, 121})
    lower := bytes.ToLower([]byte{65, 98, 99, 45, 88, 89})
    clone := bytes.Clone(beta)
    clone[0] = byte(66)
    printf("REPLACE=%s/%s REPEAT=%s CASE=%s/%s CLONE=%s ",
        string(replaced), string(limited), string(repeated),
        string(upper), string(lower), string(clone))

    csv := []byte{97, 44, 98, 44, 99}
    parts := bytes.Split(csv, comma)
    afterParts := bytes.SplitAfter(csv, comma)
    limitedParts := bytes.SplitN(csv, comma, 2)
    joined := bytes.Join(parts, []byte{43})
    printf("SPLIT=%d:%s:%s:%s AFTER=%d:%s:%s:%s SPLITN=%d:%s:%s JOIN=%s ",
        len(parts), string(parts[0]), string(parts[1]), string(parts[2]),
        len(afterParts), string(afterParts[0]), string(afterParts[1]), string(afterParts[2]),
        len(limitedParts), string(limitedParts[0]), string(limitedParts[1]), string(joined))

    trimmed := bytes.TrimSpace(source)
    custom := bytes.Trim([]byte{45, 45, 45, 104, 101, 108, 108, 111, 45, 45, 45}, "-")
    fields := bytes.Fields([]byte{32, 111, 110, 101, 9, 32, 116, 119, 111, 10, 116, 104, 114, 101, 101, 32})
    printf("TRIM=%s/%s FIELDS=%d:%s:%s:%s\n",
        string(trimmed), string(custom), len(fields),
        string(fields[0]), string(fields[1]), string(fields[2]))
    return 0
}
`,
		ExpectedOut: "EQUAL=1,0 COMPARE=-1,0,1 INDEX=8,LAST=13,COUNT=2,ANY=8,LASTANY=16 CUT=  alpha|beta,beta  |1 REPLACE=  alpha,item,item  /  alpha,X,beta   REPEAT=ababab CASE=ABC-XY/abc-xy CLONE=Beta SPLIT=3:a:b:c AFTER=3:a,:b,:c SPLITN=2:a:b,c JOIN=a+b+c TRIM=alpha,beta,beta/hello FIELDS=3:one:two:three",
		ExpectedExit: 0,
	})
}
