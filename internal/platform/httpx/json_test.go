package httpx

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSONKeepsLegacyEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteJSON(recorder, 200, map[string]string{"value": "ok"})
	var response struct {
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Message != "success" || response.Data["value"] != "ok" {
		t.Fatalf("legacy response envelope changed: %#v", response)
	}
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		context.Background(),
		"POST",
		"/",
		strings.NewReader(`{"known":true,"admin":true}`),
	)
	var input struct {
		Known bool `json:"known"`
	}
	if err := DecodeJSON(recorder, request, &input); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
}
