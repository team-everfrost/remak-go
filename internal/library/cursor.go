package library

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type pageToken struct {
	Version int       `json:"v"`
	AfterID uuid.UUID `json:"afterId"`
}

func EncodeNextCursor(documents []Document) string {
	if len(documents) == 0 {
		return ""
	}
	lastID, err := uuid.Parse(documents[len(documents)-1].DocID)
	if err != nil {
		return ""
	}
	payload, err := json.Marshal(pageToken{Version: 1, AfterID: lastID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeCursor(raw string) (string, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("decode page cursor: %w", err)
	}
	var token pageToken
	if err := json.Unmarshal(payload, &token); err != nil || token.Version != 1 || token.AfterID == uuid.Nil {
		return "", fmt.Errorf("invalid page cursor")
	}
	return token.AfterID.String(), nil
}
