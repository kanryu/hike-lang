package e2e_test

import "testing"

func TestFmt_Sprintf_Batch(t *testing.T) {
	RunHikeCase(t, HikeTestCase{
		Source: `
package main

import "std/fmt"
import "std/fmt/sprintf"

func printf(format string, ...)

func main() int {
    basic := fmt.Sprintf("INT=%d,NEG=%d,STR=%s,CHAR=%c,BOOL_T=%t,BOOL_F=%t,PERCENT=%%", 42, -17, "hello", 65, true, false)
    printf("CASE=basic:%s\n", basic)

    radix := fmt.Sprintf("BIN=%#b,OCT=%#o,HEX=%#x,HEX_U=%#X", 5, 64, 255, 255)
    flags := fmt.Sprintf("POS=%+d,ZERO=%05d", 42, 7)
    printf("CASE=radix:%s | %s\n", radix, flags)

    align := fmt.Sprintf("L=[%-6s],R=[%6s]", "go", "hike")
    quoted := fmt.Sprintf("Q=%q,PI=%.2f", "A\nB", 3.141592)
    printf("CASE=precision:%s | %s\n", align, quoted)

    typed := fmt.Sprintf("T_INT=%T,T_STR=%T,T_BOOL=%T", 100, "text", true)
    values := fmt.Sprintf("V_INT=%v,V_STR=%v,V_BOOL=%v", 999, "hike", false)
    printf("CASE=values:%s | %s\n", typed, values)

    printf("CASE=direct:%s\n", sprintf.Sprintf("VAL=%v,HEX=%#x,PAD=%04d", 123, 16, 8))
    return 0
}
`,
		ExpectedOut:  "CASE=basic:INT=42,NEG=-17,STR=hello,CHAR=A,BOOL_T=true,BOOL_F=false,PERCENT=%\nCASE=radix:BIN=0b101,OCT=0o100,HEX=0xff,HEX_U=0XFF | POS=+42,ZERO=00007\nCASE=precision:L=[go    ],R=[  hike] | Q=\"A\\nB\",PI=3.14\nCASE=values:T_INT=int,T_STR=string,T_BOOL=bool | V_INT=999,V_STR=hike,V_BOOL=false\nCASE=direct:VAL=123,HEX=0x10,PAD=0008",
		ExpectedExit: 0,
	})
}
