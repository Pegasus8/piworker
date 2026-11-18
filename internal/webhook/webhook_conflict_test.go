package webhook

import (
	"errors"
	"testing"

	"github.com/Pegasus8/piworker/internal/node"
)

// A taken path is an exclusive-resource conflict (ADR-0006), so the API can
// answer 409 instead of 500.
func TestRegisterDuplicatePathIsResourceConflict(t *testing.T) {
	h := NewHub()
	noop := func(Request) Result { return 0 }
	if err := h.Register("/a", noop); err != nil {
		t.Fatal(err)
	}
	err := h.Register("/a", noop)
	if !errors.Is(err, node.ErrResourceConflict) {
		t.Fatalf("Register duplicate = %v, want node.ErrResourceConflict", err)
	}
}
