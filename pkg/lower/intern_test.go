package lower

import (
	"testing"

	"hikec-go/pkg/ast"
	"hikec-go/pkg/sema"
)

func TestStringConstantsReceiveStableInternIDsAndHashes(t *testing.T) {
	l := New(&ast.Program{}, sema.NewContext())
	first := l.getStringConst("name")
	reused := l.getStringConst("name")
	second := l.getStringConst("value")

	if first != reused {
		t.Fatal("identical string constants must be pooled")
	}
	if first.InternID != 0 || second.InternID != 1 {
		t.Fatalf("unexpected intern IDs: first=%d second=%d", first.InternID, second.InternID)
	}
	if len(l.hirProg.InternHashes) != 2 {
		t.Fatalf("expected two intern hashes, got %d", len(l.hirProg.InternHashes))
	}
	if l.hirProg.InternHashes[0] == l.hirProg.InternHashes[1] {
		t.Fatal("different literals unexpectedly received the same hash")
	}
}
