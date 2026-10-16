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

func TestMemoryMapOffsetsUseEightByteAlignment(t *testing.T) {
	for _, test := range []struct {
		in  int
		out int
	}{
		{in: 0, out: 0},
		{in: 1, out: 8},
		{in: 4, out: 8},
		{in: 8, out: 8},
		{in: 15, out: 16},
	} {
		if got := alignMemoryMapOffset(test.in); got != test.out {
			t.Fatalf("alignMemoryMapOffset(%d) = %d, want %d", test.in, got, test.out)
		}
	}
}
