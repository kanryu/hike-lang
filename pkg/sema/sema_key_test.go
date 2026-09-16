package sema

import "testing"

func TestBuildInternalKeyUsesReceiverDelimiter(t *testing.T) {
	tests := []struct {
		name     string
		receiver string
		want     string
	}{
		{"function", "", "crypto/Sum"},
		{"value receiver", "Digest", "crypto/Sum@Digest"},
		{"pointer receiver", "*Digest", "crypto/Sum@@Digest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildInternalKey("crypto", "Sum", tt.receiver); got != tt.want {
				t.Fatalf("BuildInternalKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMangleInternalKeyWithReceiver(t *testing.T) {
	tests := map[string]string{
		"crypto/Sum@Digest":  "crypto_Digest_Sum",
		"crypto/Sum@@Digest": "crypto_Digest_ptr_Sum",
	}
	for key, want := range tests {
		if got := MangleInternalKeyToIR(key); got != want {
			t.Errorf("MangleInternalKeyToIR(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestLookupFunctionFromModuleFQDN(t *testing.T) {
	ctx := NewContext()
	shaSum := &FuncType{Name: "sha256/Sum256", InternalKey: "sha256/Sum256"}
	md5Sum := &FuncType{Name: "md5/Sum", InternalKey: "md5/Sum"}
	ctx.Functions[shaSum.Name] = shaSum
	ctx.Functions[md5Sum.Name] = md5Sum

	for name, want := range map[string]*FuncType{
		"sha256/Sum256": shaSum,
		"md5/Sum":       md5Sum,
	} {
		got, canonical := ctx.LookupFunction(name)
		if got != want || canonical != name {
			t.Errorf("LookupFunction(%q) = (%p, %q), want (%p, %q)", name, got, canonical, want, name)
		}
	}
}

func TestLookupMethodFromReceiverFQDN(t *testing.T) {
	ctx := NewContext()
	valueMethod := &FuncType{
		Name:        "pkg/Value@Digest",
		InternalKey: "pkg/Value@Digest",
		IsMethod:    true,
	}
	pointerMethod := &FuncType{
		Name:        "pkg/Value@@Digest",
		InternalKey: "pkg/Value@@Digest",
		IsMethod:    true,
	}
	otherMethod := &FuncType{
		Name:        "other/Value@Digest",
		InternalKey: "other/Value@Digest",
		IsMethod:    true,
	}
	ctx.RegisterMethod("Digest", "Sum", valueMethod)
	ctx.RegisterMethod("*Digest", "Sum", pointerMethod)
	ctx.RegisterMethod("other.Digest", "Sum", otherMethod)

	for _, tt := range []struct {
		receiver string
		want     *FuncType
		key      string
	}{
		{"Digest", valueMethod, "pkg/Value@Digest"},
		{"*Digest", pointerMethod, "pkg/Value@@Digest"},
		{"other.Digest", otherMethod, "other/Value@Digest"},
	} {
		got, key := ctx.LookupMethod(tt.receiver, "Sum")
		if got != tt.want || key != tt.key {
			t.Errorf("LookupMethod(%q, Sum) = (%p, %q), want (%p, %q)", tt.receiver, got, key, tt.want, tt.key)
		}
	}

	if got, _ := ctx.LookupMethod("Digest", "Missing"); got != nil {
		t.Fatalf("LookupMethod returned a method for an unknown method name: %v", got)
	}
}
