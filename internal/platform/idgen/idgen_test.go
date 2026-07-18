package idgen

import "testing"

func TestNewReturnsUUIDv7(t *testing.T) {
	t.Parallel()

	id := New()
	if got := id.Version(); got != 7 {
		t.Fatalf("version = %d, want 7", got)
	}
}
