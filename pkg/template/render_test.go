package template

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

func TestRenderLiteralRejectsDynamicActions(t *testing.T) {
	for _, source := range []string{"{{.}}", "{{if .OK}}x{{end}}", "{{.Missing}}", "{{.A.B}}", "{{.A"} {
		if _, err := RenderLiteral(source, func(field string) (any, bool) {
			return 1, field == "A"
		}); err == nil {
			t.Errorf("RenderLiteral(%q) accepted unsupported action", source)
		}
	}
}
