package debug

import (
	"strings"
	"testing"
)

func TestDebugManagerUsesFunctionSourceFile(t *testing.T) {
	dm := NewDebugManager("cmd/hikec/main.go", true)
	dm.StartFunction("main", 10, "cmd/hikec/main.go")
	dm.StartFunction("sprintf_Sprintf", 217, "std/fmt/sprintf/sprintf.hike")

	metadata := dm.EmitMetadata()
	if !strings.Contains(metadata, `!DIFile(filename: "sprintf.hike"`) {
		t.Fatalf("metadata does not contain the function source file: %s", metadata)
	}
	if !strings.Contains(metadata, `!DISubprogram(name: "sprintf_Sprintf"`) {
		t.Fatalf("metadata does not contain the function subprogram: %s", metadata)
	}
}
