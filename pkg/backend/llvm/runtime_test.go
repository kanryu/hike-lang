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
		})
	}
}
