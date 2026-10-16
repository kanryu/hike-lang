package e2e_test

import "testing"

// parser_expr.go の軽量な構文ケースを1回のコンパイルで検証する。
func TestParserExpr_Batch(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

type Layer2 struct { scores []int }
type Layer1 struct { child *Layer2 }

func getLayer() *Layer1 {
    l2 := &Layer2{scores: []int{100, 200, 300}}
    return &Layer1{child: l2}
}

func taskWork(base int, mult int) int { return base * mult }

func main() int {
    r1 := 1 | 2 & 2
    r2 := (1 | 2) & 2
    r3 := 20 >> 1 + 1
    r4 := 20 >> (1 + 1)
    r5 := 2 + 3 * 4
    r6 := (2 + 3) * 4
    printf("CASE=precedence:R1=%d,R2=%d,R3=%d,R4=%d,R5=%d,R6=%d\n", r1, r2, r3, r4, r5, r6)

    b1 := 0
    if true || false && false { b1 = 1 }
    b2 := 0
    if (true || false) && false { b2 = 1 }
    b3 := 0
    if 5 > 3 && 10 <= 20 { b3 = 1 }
    printf("CASE=logical:B1=%d,B2=%d,B3=%d\n", b1, b2, b3)

    cTab := '\t'
    cNL := '\n'
    cCR := '\r'
    cZero := '\0'
    cQuote := '\''
    cSlash := '\\'
    cA := 'A'
    printf("CASE=char:TAB=%d,NL=%d,CR=%d,ZERO=%d,Q=%d,S=%d,A=%d\n", int(cTab), int(cNL), int(cCR), int(cZero), int(cQuote), int(cSlash), int(cA))

    str := "HikeLang"
    printf("CASE=string:P1=%s,P2=%s,P3=%s\n", str[:4], str[4:], str[:])

    arr := []int{10, 20, 30, 40, 50}
    s1 := arr[:3]
    s2 := arr[2:]
    s3 := arr[:]
    printf("CASE=slice:S1_LEN=%d,S1_END=%d;S2_LEN=%d,S2_START=%d;S3_LEN=%d\n", len(s1), s1[2], len(s2), s2[0], len(s3))

    iife := func(a int, b int) int { return a*a + b*b }(3, 4)
    printf("CASE=iife:IIFE_RES=%d\n", iife)

    chain := getLayer().child.scores[1]
    printf("CASE=chain:CHAIN_VAL=%d\n", chain)

    ch := make(chan int, 2)
    ch <- 15
    ch <- 25
    sum := (<-ch) + (<-ch)
    printf("CASE=receive:CHAN_EXPR_SUM=%d\n", sum)

    var a any = 777
    v1, ok1 := a.(int)
    v2, ok2 := a.(string)
    lenV2 := 0
    if ok2 { lenV2 = len(v2) }
    printf("CASE=assert:V1=%d,OK1=%d,V2_LEN=%d,OK2=%d\n", v1, ok1, lenV2, ok2)

    fut := async(func() int { return taskWork(7, 6) })
    res := <-fut
    printf("CASE=async:ASYNC_EVAL=%d\n", res)
    return 0
}
`,
		ExpectedOut:  "CASE=precedence:R1=3,R2=2,R3=11,R4=5,R5=14,R6=20\nCASE=logical:B1=1,B2=0,B3=1\nCASE=char:TAB=9,NL=10,CR=13,ZERO=0,Q=39,S=92,A=65\nCASE=string:P1=Hike,P2=Lang,P3=HikeLang\nCASE=slice:S1_LEN=3,S1_END=30;S2_LEN=3,S2_START=30;S3_LEN=5\nCASE=iife:IIFE_RES=25\nCASE=chain:CHAIN_VAL=200\nCASE=receive:CHAN_EXPR_SUM=40\nCASE=assert:V1=777,OK1=1,V2_LEN=0,OK2=0\nCASE=async:ASYNC_EVAL=42",
		ExpectedExit: 0,
	})
}
