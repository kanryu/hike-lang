package e2e_test

import "testing"

func TestBufferLifetime_FunctionArgumentsAndDerivedViews(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int
func consumeString(label string, value string) int { middle := value[1:len(value)-1]; printf("%s=%s\n", label, middle); if len(value)>2 { branch:=value[0:2]; printf("B=%s\n",branch) }; for i:=0;i<1;i=i+1 { loop:=value[0:1]; printf("L=%s\n",loop) }; return len(middle) }
func consumeSlice(label string, value []int) int { middle:=value[1:len(value)]; printf("%s=%d,%d\n",label,middle[0],len(middle)); return middle[0]+len(middle) }
func main() int { literal:="literal"; dynamic:=string([]byte{'d','y','n','a','m','i','c'}); a:=consumeString("SL",literal); b:=consumeString("SD",dynamic); ls:=[]int{10,20,30}; ds:=make([]int,4,6); ds[0]=40;ds[1]=50;ds[2]=60;ds[3]=70; c:=consumeSlice("VL",ls); d:=consumeSlice("VD",ds); printf("RESULT=%d,%d,%d,%d\n",a,b,c,d); return 0 }
`, ExpectedOut: "SL=itera\nB=li\nL=l\nSD=ynami\nB=dy\nL=d\nVL=20,2\nVD=50,3\nRESULT=5,5,22,53\n", ExpectedExit: 0})
}

func TestBufferLifetime_IfBranches(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int
func chooseString(value string, right int) int { var part string; if right!=0 { part=value[2:5] } else { part=value[0:3] }; printf("PART=%s,LEN=%d\n",part,len(part)); return len(value) }
func chooseSlice(value []int, right int) int { var part []int; if right!=0 { part=value[2:4] } else { part=value[0:2] }; printf("SLICE=%d,%d\n",part[0],len(part)); return value[0] }
func main() int { literal:="abcdef"; dynamic:=string([]byte{'u','v','w','x','y','z'}); v:=make([]int,4);v[0]=10;v[1]=20;v[2]=30;v[3]=40;chooseString(literal,0);chooseString(dynamic,1);chooseSlice(v,0);chooseSlice(v,1);printf("DONE=%d\n",len(dynamic));return 0 }
`, ExpectedOut: "PART=abc,LEN=3\nPART=wxy,LEN=3\nSLICE=10,2\nSLICE=30,2\nDONE=6", ExpectedExit: 0})
}

func TestBufferLifetime_ForLoops(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int
func scanString(value string) int { total:=0; for i:=0;i<len(value)-1;i=i+1 { part:=value[i:i+2]; total=total+len(part) }; return total }
func scanSlice(value []int) int { total:=0; for i:=0;i<len(value);i=i+1 { part:=value[i:i+1]; total=total+part[0] }; return total }
func main() int { text:=string([]byte{'l','o','o','p'}); n:=make([]int,3);n[0]=7;n[1]=11;n[2]=13;printf("LOOP=%d,%d\n",scanString(text),scanSlice(n));return 0 }
`, ExpectedOut: "LOOP=6,31", ExpectedExit: 0})
}

func TestBufferLifetime_CStringToString(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int
func consume(raw cstring) int { value:=string(raw); part:=value[1:len(value)-1]; printf("CSTR=%s,LEN=%d\n",part,len(value)); return len(part) }
func main() int { raw:=cstring("headered"); result:=consume(raw); printf("RESULT=%d\n",result); return 0 }
`, ExpectedOut: "CSTR=eadere,LEN=8\nRESULT=6", ExpectedExit: 0})
}

// C ABI entry points receive a raw pointer and explicit byte length. Verify
// both cstring -> string and direct pointer+length -> string paths before the
// result is converted back to a C string.
func TestBufferLifetime_CFuncCStringToString(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int

cfunc FromCString(input *byte, length int) cstring {
    raw := cstring(input, length)
    value := string(raw)
    if len(value) > 2 { value = value[1:len(value)-1] }
    return cstring(value + "!")
}

cfunc FromPointerLength(input *byte, length int) cstring {
    value := string(input, length)
    if len(value) > 2 { value = value[1:len(value)-1] }
    return cstring(value + "?")
}

func main() int {
    data := []byte{'a', 'b', 'c', 'd'}
    first := FromCString(&data[0], 4)
    second := FromPointerLength(&data[0], 4)
    printf("C=%s,%d P=%s,%d\n", first, len(first), second, len(second))
    return 0
}
`, ExpectedOut: "C=bc!,3 P=bc?,3", ExpectedExit: 0})
}

// A raw byte pointer is borrowed storage. Its slice view uses offset -1 and
// must remain valid through an if without retain/release on the raw pointer.
func TestBufferLifetime_RawBytePointerSliceInIf(t *testing.T) {
	RunHikeCase(t, HikeTestCase{Source: `
package main
func printf(format string, ...) int
func main() int {
    data := make([]byte, 4)
    data[0] = 10; data[1] = 20; data[2] = 30; data[3] = 40
    ptr := &data[0]
    length := 3
    var view []byte
    if ptr != nil { view = ptr[1:length] }
    printf("RAW=%d,%d,%d\n", view[0], view[1], len(view))
    return 0
}
`, ExpectedOut: "RAW=20,30,2", ExpectedExit: 0})
}
