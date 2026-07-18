package library

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	raw := EncodeNextCursor([]Document{{DocID: id.String(), CreatedAt: time.Now()}})
	decoded, err := DecodeCursor(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != id.String() {
		t.Fatalf("got %s, want %s", decoded, id)
	}
}

func TestCursorRejectsGarbage(t *testing.T) {
	if _, err := DecodeCursor("not-a-cursor"); err == nil {
		t.Fatal("garbage cursor must be rejected")
	}
}

func TestValidatePublicURL(t *testing.T) {
	valid, err := validatePublicURL(" https://example.com/a#fragment ")
	if err != nil || valid != "https://example.com/a" {
		t.Fatalf("unexpected result %q, %v", valid, err)
	}
	for _, raw := range []string{"file:///etc/passwd", "https://user:pass@example.com", "javascript:alert(1)"} {
		if _, err := validatePublicURL(raw); err == nil {
			t.Fatalf("URL %q must be rejected", raw)
		}
	}
}
