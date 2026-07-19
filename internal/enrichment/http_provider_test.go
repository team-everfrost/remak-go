package enrichment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPProviderAnalyzeRequestsStructuredJSONAndNormalizesTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization header was not forwarded")
		}
		var body struct {
			Model          string `json:"model"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "chat-test" || body.ResponseFormat.Type != "json_object" {
			t.Errorf("unexpected structured request: model=%q format=%q", body.Model, body.ResponseFormat.Type)
		}
		if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
			t.Errorf("unexpected messages: %+v", body.Messages)
		}
		response.Header().Set("Content-Type", "application/json")
		payload := `{"choices":[{"message":{"content":"` +
			`{\"summary\":\" 요약 결과 \" ,\"tags\":[\"#Go\",\" go \",\"PostgreSQL\",\"RAG\"]}` +
			`"}}]}`
		_, _ = response.Write(
			[]byte(payload),
		)
	}))
	defer server.Close()

	provider := NewHTTPProvider(server.URL, "secret", "embedding-test", "chat-test", 1536)
	result, err := provider.Analyze(context.Background(), "제목", "본문")
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "요약 결과" {
		t.Fatalf("summary = %q", result.Summary)
	}
	want := []string{"Go", "PostgreSQL", "RAG"}
	if len(result.Tags) != len(want) {
		t.Fatalf("tags = %v, want %v", result.Tags, want)
	}
	for index := range want {
		if result.Tags[index] != want[index] {
			t.Fatalf("tags = %v, want %v", result.Tags, want)
		}
	}
}

func TestHTTPProviderAnalyzeRejectsInvalidStructuredResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"choices":[{"message":{"content":"not-json"}}]}`))
	}))
	defer server.Close()

	provider := NewHTTPProvider(server.URL, "", "embedding-test", "chat-test", 1536)
	if _, err := provider.Analyze(context.Background(), "제목", "본문"); err == nil {
		t.Fatal("expected malformed analysis response to fail")
	}
}
