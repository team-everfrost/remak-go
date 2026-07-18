package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/team-everfrost/remak-go/internal/platform/idgen"
)

const (
	SchemaVersionV1 = "1.0"
	ScrapeRequested = "document.scrape.requested.v1"
	ScrapeCompleted = "document.scrape.completed.v1"
	ScrapeFailed    = "document.scrape.failed.v1"
)

type Envelope[T any] struct {
	SchemaVersion string    `json:"schemaVersion"`
	EventID       uuid.UUID `json:"eventId"`
	EventType     string    `json:"eventType"`
	TraceID       string    `json:"traceId"`
	OccurredAt    time.Time `json:"occurredAt"`
	Data          T         `json:"data"`
}

type ScrapeRequestData struct {
	JobID           uuid.UUID `json:"jobId"`
	DocumentID      uuid.UUID `json:"documentId"`
	DocumentVersion int32     `json:"documentVersion"`
	URL             string    `json:"url"`
}

type ScrapeResultData struct {
	JobID              uuid.UUID `json:"jobId"`
	DocumentID         uuid.UUID `json:"documentId"`
	DocumentVersion    int32     `json:"documentVersion"`
	Title              string    `json:"title"`
	Content            string    `json:"content,omitempty"`
	RawArtifactKey     string    `json:"rawArtifactKey"`
	ContentArtifactKey string    `json:"contentArtifactKey"`
	ContentHash        string    `json:"contentHash"`
	ThumbnailURL       string    `json:"thumbnailUrl,omitempty"`
	ExtractionMethod   string    `json:"extractionMethod"`
	FileSize           int64     `json:"fileSize"`
	ErrorCode          string    `json:"errorCode,omitempty"`
	ErrorMessage       string    `json:"errorMessage,omitempty"`
}

func New[T any](eventType, traceID string, data T) Envelope[T] {
	return Envelope[T]{
		SchemaVersion: SchemaVersionV1,
		EventID:       idgen.New(),
		EventType:     eventType,
		TraceID:       traceID,
		OccurredAt:    time.Now().UTC(),
		Data:          data,
	}
}

func Marshal[T any](event Envelope[T]) ([]byte, error) {
	return json.Marshal(event)
}
