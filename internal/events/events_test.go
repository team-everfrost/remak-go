package events

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestEventEnvelopeContainsRequiredContractFields(t *testing.T) {
	event := New(
		ScrapeRequested,
		"trace-1",
		ScrapeRequestData{
			JobID:           uuid.Must(uuid.NewV7()),
			DocumentID:      uuid.Must(uuid.NewV7()),
			DocumentVersion: 1,
			URL:             "https://example.com",
		},
	)
	payload, err := Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schemaVersion", "eventId", "eventType", "traceId", "occurredAt", "data"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("event contract is missing %q", key)
		}
	}
}
