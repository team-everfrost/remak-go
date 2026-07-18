package httpx

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDRejectsUnboundedOrUnsafeValues(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(RequestIDFromContext(r.Context())))
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", strings.Repeat("x", 129)+"\nforged")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Body.String() == request.Header.Get("X-Request-ID") || !validRequestID(response.Body.String()) {
		t.Fatalf("unsafe request id was accepted: %q", response.Body.String())
	}
}

func TestAccessLogRecordsStatusAndBytes(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestID(AccessLog(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/documents", nil))
	logLine := output.String()
	if !strings.Contains(logLine, `"status":201`) || !strings.Contains(logLine, `"bytes":2`) {
		t.Fatalf("missing response metrics: %s", logLine)
	}
}
