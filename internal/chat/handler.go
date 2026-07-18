package chat

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
)

type Handler struct {
	service *Service
}

type ragInput struct {
	Query   string `json:"query"`
	Context string `json:"context,omitempty"`
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Routes() chi.Router {
	router := chi.NewRouter()
	router.Post("/rag", h.rag)
	return router
}

func (h *Handler) rag(w http.ResponseWriter, r *http.Request) {
	var input ragInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, err)
		return
	}
	actor := httpx.ActorFromContext(r.Context())
	documents, answer, err := h.service.Answer(r.Context(), actor.AccountID, input.Query)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	documentPayload, err := json.Marshal(documents)
	if err != nil {
		httpx.WriteError(w, httpx.Internal(err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprintf(w, "event: documents\ndata: %s\n\n", documentPayload)
	flush(w)
	for _, part := range splitRunes(answer, 64) {
		payload, _ := json.Marshal(map[string]string{"text": part})
		_, _ = fmt.Fprintf(w, "event: chat\ndata: %s\n\n", payload)
		flush(w)
	}
}

func flush(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func splitRunes(value string, size int) []string {
	runes := []rune(value)
	if len(runes) == 0 {
		return []string{}
	}
	result := make([]string, 0, (len(runes)+size-1)/size)
	for start := 0; start < len(runes); start += size {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		result = append(result, string(runes[start:end]))
	}
	return result
}
