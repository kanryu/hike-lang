package compiler

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"hikec-go/pkg/target"
)

// TestBufferLifetimeIRChecks the ABI invariants that are not observable from
// a normal program result. In particular, literal strings must never reach a
// refcount header, while ordinary string/slice values must retain the offset
// needed to find their backing allocation.
func TestBufferLifetimeIRChecks(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod := "module buffer-lifetime-ir\nhike 0.1.0\nreplace std => " + filepath.ToSlash(filepath.Join(root, "std")) + "\n"
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main

func printf(format string, ...) int

func consumeString(label string, value string) int {
    middle := value[1:len(value)-1]
    printf("%s=%s\n", label, middle)
    if len(value) > 2 {
        branch := value[0:2]
        printf("B=%s\n", branch)
    }
    for i := 0; i < 1; i = i + 1 {
        loop := value[0:1]
        printf("L=%s\n", loop)
    }
    return len(middle)
}

func consumeSlice(label string, value []int) int {
    middle := value[1:len(value)]
    printf("%s=%d,%d\n", label, middle[0], len(middle))
    if len(value) > 2 {
        branch := value[0:2]
        printf("SB=%d\n", branch[0])
    }
    for i := 0; i < 1; i = i + 1 {
        loop := value[0:1]
        printf("SL=%d\n", loop[0])
    }
    return middle[0] + len(middle)
}

func consumeCString(raw cstring) int {
    value := string(raw)
    part := value[1:len(value)-1]
    printf("C=%s\n", part)
    return len(part)
}

func consumeRaw(ptr *byte, length int) int {
    var view []byte
    if ptr != nil {
        view = ptr[1:length]
    }
    return view[0] + len(view)
}

cfunc IRFromCString(ptr *byte, length int) cstring {
    raw := cstring(ptr, length)
    value := string(raw)
    if len(value) > 2 {
        value = value[1:len(value)-1]
    }
    return cstring(value + "!")
}

func main() int {
    literal := "literal"
    dynamic := string([]byte{'d', 'y', 'n', 'a', 'm', 'i', 'c'})
    literalSlice := []int{10, 20, 30}
    dynamicSlice := make([]int, 3)
    dynamicSlice[0] = 40
    dynamicSlice[1] = 50
    dynamicSlice[2] = 60
    _ = consumeString("SL", literal)
    _ = consumeString("SD", dynamic)
    _ = consumeSlice("VL", literalSlice)
    _ = consumeSlice("VD", dynamicSlice)
    _ = consumeCString(cstring("headered"))
    _ = consumeRaw(&dynamicSlice[0], 3)
    _ = IRFromCString(&dynamicSlice[0], 3)
    return 0
}
`
	entry := filepath.Join(tmp, "main.hike")
	if err := os.WriteFile(entry, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	tgt := target.TargetX86_64Windows
	c := New(&tgt)
	ir, _, _, err := c.CompileToLLVM(entry)
	if err != nil {
		t.Fatalf("buffer lifetime IR compilation failed: %v\n%s", err, c.Reporter().FormatAll())
	}

	checks := []struct {
		name string
		want string
	}{
		{"literal offset sentinel", `insertvalue \{ i8\*, i32, i32 \} .*i32 -1`},
		{"offset-aware string release", `call void @__hike_string_release\(i8\* [^,]+, i32 [^)]+\)`},
		{"offset-aware string retain", `call void @__hike_string_retain\(i8\* [^,]+, i32 [^)]+\)`},
		{"slice allocation header", `call i8\* @__hike_slice_alloc\(i64 [^,]+, i64 [^)]+\)`},
		{"cstring conversion uses headered runtime allocation", `call i8\* @__hike_slice_to_str\(i8\* [^,]+, i64 [^)]+\)`},
		{"slice capacity header", `getelementptr inbounds i8, i8\* %owner, i64 -16`},
		{"literal retain guard", `%literal = icmp slt i32 %offset, 0`},
		{"raw byte pointer view sentinel", `insertvalue \{ i8\*, i32, i32 \} .*i32 -1`},
	}
	for _, check := range checks {
		if !regexp.MustCompile(check.want).MatchString(ir) {
			t.Errorf("missing %s in generated IR", check.name)
		}
	}

	// The IR under test must contain the control-flow paths that exercise
	// branch-local and loop-local views, not merely the straight-line ABI
	// operations checked above.
	consumeStringIR := ""
	consumeStringStart := strings.Index(ir, "define ")
	for consumeStringStart >= 0 {
		candidateEnd := strings.Index(ir[consumeStringStart:], "@consumeString")
		if candidateEnd >= 0 {
			candidateEnd += consumeStringStart
			functionStart := strings.LastIndex(ir[:candidateEnd], "\ndefine ")
			if functionStart < 0 {
				functionStart = 0
			} else {
				functionStart++
			}
			bodyEnd := strings.Index(ir[candidateEnd:], "\ndefine ")
			if bodyEnd < 0 {
				bodyEnd = len(ir) - candidateEnd
			}
			consumeStringIR = ir[functionStart : candidateEnd+bodyEnd]
			for _, label := range []string{"structured.if.then", "structured.if.end", "structured.loop.header", "structured.loop.end"} {
				if !strings.Contains(consumeStringIR, label) {
					t.Errorf("consumeString IR is missing control-flow block %q", label)
				}
			}
			if strings.Count(consumeStringIR, "call void @__hike_string_retain") < 3 {
				t.Errorf("consumeString branch/loop views were not retained independently")
			}
			if strings.Count(consumeStringIR, "call void @__hike_string_release") < 3 {
				t.Errorf("consumeString branch/loop views were not released independently")
			}
			break
		}
		next := strings.Index(ir[consumeStringStart+len("define "):], "define ")
		if next < 0 {
			consumeStringStart = -1
		} else {
			consumeStringStart += len("define ") + next
		}
	}
	if consumeStringIR == "" {
		t.Fatal("consumeString function body was not emitted")
	}

	if regexp.MustCompile(`call void @__hike_string_(?:retain|release)\(i8\* [^,]+\)`).MatchString(ir) {
		t.Fatal("generated a legacy one-argument string retain/release")
	}
	stringRetains := len(regexp.MustCompile(`call void @__hike_string_retain\(i8\* [^,]+, i32 [^)]+\)`).FindAllString(ir, -1))
	stringReleases := len(regexp.MustCompile(`call void @__hike_string_release\(i8\* [^,]+, i32 [^)]+\)`).FindAllString(ir, -1))
	if stringRetains < 1 {
		t.Errorf("function string ownership is under-instrumented: retains=%d releases=%d", stringRetains, stringReleases)
	}
	sliceRetains := len(regexp.MustCompile(`call void @__hike_slice_retain\(i8\* [^)]+\)`).FindAllString(ir, -1))
	sliceReleases := len(regexp.MustCompile(`call void @__hike_slice_release\(i8\* [^)]+\)`).FindAllString(ir, -1))
	if sliceRetains < 2 || sliceReleases < 2 {
		t.Errorf("function slice ownership is under-instrumented: retains=%d releases=%d", sliceRetains, sliceReleases)
	}
	if !strings.Contains(ir, "call void @__hike_slice_retain") || !strings.Contains(ir, "call void @__hike_slice_release") {
		t.Fatal("derived slice view was not retained and released")
	}
	rawStart := strings.Index(ir, "@consumeRaw")
	if rawStart < 0 {
		t.Fatal("consumeRaw function body was not emitted")
	}
	rawEnd := strings.Index(ir[rawStart:], "\ndefine ")
	if rawEnd < 0 {
		rawEnd = len(ir) - rawStart
	}
	rawIR := ir[rawStart : rawStart+rawEnd]
	if !strings.Contains(rawIR, "i32 -1") {
		t.Fatal("raw byte pointer slice does not use offset -1")
	}
	if strings.Contains(rawIR, "@__hike_slice_retain") || strings.Contains(rawIR, "@__hike_slice_release") {
		t.Fatal("raw byte pointer view was treated as an owning slice")
	}
	if strings.Contains(rawIR, "@strlen") {
		t.Fatal("explicit byte-pointer length unexpectedly used strlen")
	}
	cfuncStart := strings.Index(ir, "IRFromCString")
	if cfuncStart < 0 {
		t.Fatal("IRFromCString cfunc was not emitted")
	}
	cfuncEnd := strings.Index(ir[cfuncStart:], "\ndefine ")
	if cfuncEnd < 0 {
		cfuncEnd = len(ir) - cfuncStart
	}
	cfuncIR := ir[cfuncStart : cfuncStart+cfuncEnd]
	if !strings.Contains(cfuncIR, "@__hike_slice_to_str") {
		t.Fatal("C string conversion did not allocate through __hike_slice_to_str")
	}
	if !strings.Contains(cfuncIR, "@__hike_string_release") {
		t.Fatal("C string-derived Hike string was not released")
	}
}

// TestBufferLifetimeRuntimeLayout checks the runtime implementation directly.
// These checks cover paths which a small Hike program cannot necessarily reach
// while still keeping the generated-IR test focused on compiler lowering.
func TestBufferLifetimeRuntimeLayout(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	common, err := os.ReadFile(filepath.Join(root, "pkg", "backend", "llvm", "runtime", "runtime_common.ll"))
	if err != nil {
		t.Fatal(err)
	}
	wasm, err := os.ReadFile(filepath.Join(root, "pkg", "backend", "llvm", "runtime", "runtime_wasm32.ll"))
	if err != nil {
		t.Fatal(err)
	}

	for name, ir := range map[string]string{"llvm64": string(common), "wasm32": string(wasm)} {
		if !regexp.MustCompile(`%total = add .*16`).MatchString(ir) {
			t.Errorf("%s slice allocation does not reserve a 16-byte header", name)
		}
		if !regexp.MustCompile(`getelementptr inbounds i8, i8\* %owner, .* -16`).MatchString(ir) {
			t.Errorf("%s slice capacity is not stored at owner-16", name)
		}
	}
	if !regexp.MustCompile(`(?s)@__hike_string_retain\(i8\* %data, i32 %offset\).*?icmp slt i32 %offset, 0`).MatchString(string(common)) {
		t.Error("LLVM string retain does not guard literal offsets")
	}
	if !regexp.MustCompile(`(?s)@__hike_string_release\(i8\* %data, i32 %offset\).*?icmp slt i32 %offset, 0`).MatchString(string(common)) {
		t.Error("LLVM string release does not guard literal offsets")
	}
	if !regexp.MustCompile(`(?s)__hike_string_retain32\(i8\* %data, i32 %offset\).*?icmp slt i32 %offset, 0`).MatchString(string(wasm)) {
		t.Error("WASM string retain does not guard literal offsets")
	}
	for name, ir := range map[string]string{"llvm64": string(common), "wasm32": string(wasm)} {
		if !regexp.MustCompile(`(?s)__hike_string_writable(?:32)?\([^)]*%offset, i32 %len\).*?icmp slt i32 %offset, 0`).MatchString(ir) {
			t.Errorf("%s writable-string path does not guard literal offsets", name)
		}
		if !regexp.MustCompile(`(?s)__hike_string_append(?:32)?\([^)]*%offset, i32 %len.*?icmp slt i32 %offset, 0`).MatchString(ir) {
			t.Errorf("%s string-append path does not guard literal offsets", name)
		}
	}
}
