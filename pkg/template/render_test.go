package compiletemplate

import "testing"

func TestRenderLiteral(t *testing.T) {
	got, err := RenderLiteral("; {{.PointerBits}}-bit, size={{.SizeType}}", func(field string) (any, bool) {
		switch field {
		case "PointerBits":
			return 64, true
		case "SizeType":
			return "i64", true
		default:
			return nil, false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "; 64-bit, size=i64" {
		t.Fatalf("rendered %q", got)
	}
}

func TestRenderLiteralIfElse(t *testing.T) {
	lookup := func(field string) (any, bool) {
		if field == "SizeType" {
			return "i32", true
		}
		return nil, false
	}
	got, err := RenderLiteral(`{{if eq .SizeType "i64"}}wide{{else}}narrow{{end}}`, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got != "narrow" {
		t.Fatalf("rendered %q", got)
	}

	got, err = RenderLiteral(`{{if eq .SizeType "i32"}}outer {{if eq .SizeType "i32"}}inner{{end}}{{else}}wrong{{end}}`, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got != "outer inner" {
		t.Fatalf("nested rendered %q", got)
	}
}

func TestRenderLiteralRejectsDynamicActions(t *testing.T) {
	for _, source := range []string{"{{.}}", "{{if .OK}}x{{end}}", "{{if eq .OK \"yes\"}}x", "{{else}}x{{end}}", "{{.Missing}}", "{{.A.B}}", "{{.A"} {
		if _, err := RenderLiteral(source, func(field string) (any, bool) {
			return 1, field == "A"
		}); err == nil {
			t.Errorf("RenderLiteral(%q) accepted unsupported action", source)
		}
	}
}
