package llvm

import (
	"strings"
	"testing"
)

func TestRuntimeTemplateRendering(t *testing.T) {
	for _, tc := range []struct {
		name   string
		triple string
		bits   int
		want   string
	}{
		{name: "linux-amd64", triple: "x86_64-unknown-linux-gnu", bits: 64, want: "i64"},
		{name: "linux-arm64", triple: "aarch64-unknown-linux-gnu", bits: 64, want: "i64"},
		{name: "linux-arm32", triple: "armv7-unknown-linux-gnueabihf", bits: 32, want: "i32"},
		{name: "windows-x86", triple: "i686-w64-windows-gnu", bits: 32, want: "i32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ir := GetRuntimeIR(tc.triple, tc.bits)
			if strings.Contains(ir, "{{") || strings.Contains(ir, "}}") {
				t.Fatal("runtime IR still contains an unrendered template action")
			}
			if !strings.Contains(ir, tc.want) {
				t.Fatalf("runtime IR does not contain %s-sized ABI types", tc.want)
			}
			if strings.HasPrefix(tc.name, "linux-") {
				for _, required := range []string{
					"declare i32 @pthread_cond_timedwait(i8*, i8*, i8*)",
					"%ts_ptr = bitcast [16 x i8]* %ts to i8*",
					"call i32 @pthread_cond_timedwait(i8* %c, i8* %m, i8* %ts_ptr)",
					"call i32 @clock_gettime(i32 0, i8* %ts_ptr)",
				} {
					if !strings.Contains(ir, required) {
						t.Errorf("Linux runtime is missing timeout support %q", required)
					}
				}
			}
		})
	}
}

func TestCompactMapReadWrappersHandleNilMaps(t *testing.T) {
	ir := GetRuntimeIR("x86_64-w64-windows-gnu", 64)
	lenStart := strings.Index(ir, "define internal i64 @__hike_cdict_len")
	if lenStart < 0 {
		t.Fatal("runtime is missing __hike_cdict_len")
	}
	lenEnd := strings.Index(ir[lenStart:], "\n}\n")
	if lenEnd < 0 {
		t.Fatal("runtime __hike_cdict_len has no closing brace")
	}
	if !strings.Contains(ir[lenStart:lenStart+lenEnd], "icmp eq %struct.__hike_map* %m, null") {
		t.Fatal("__hike_cdict_len does not preserve nil-map len semantics")
	}
	for _, name := range []string{
		"__hike_cdict_get",
		"__hike_cdict_get_str",
	} {
		start := strings.Index(ir, "define internal i1 @"+name)
		if start < 0 {
			t.Fatalf("runtime is missing %s", name)
		}
		end := strings.Index(ir[start:], "\n}\n")
		if end < 0 {
			t.Fatalf("runtime function %s has no closing brace", name)
		}
		body := ir[start : start+end]
		if !strings.Contains(body, "icmp eq %struct.__hike_map* %m, null") {
			t.Errorf("%s does not preserve nil-map lookup semantics", name)
		}
	}
}
