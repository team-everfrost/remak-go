package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/team-everfrost/remak-go/internal/enrichment"
	"github.com/team-everfrost/remak-go/internal/library"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
	"github.com/team-everfrost/remak-go/internal/retrieval"
)

type Service struct {
	retrieval *retrieval.Service
	library   *library.Service
	provider  enrichment.Provider
}

func NewService(retrievalService *retrieval.Service, libraryService *library.Service, provider enrichment.Provider) *Service {
	return &Service{retrieval: retrievalService, library: libraryService, provider: provider}
}

func (s *Service) Answer(ctx context.Context, ownerID uuid.UUID, rawQuery string) ([]library.Document, string, error) {
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		return nil, "", httpx.BadRequest("query_required", "질문을 입력해 주세요")
	}
	if len([]rune(query)) > 4000 {
		return nil, "", httpx.BadRequest("query_too_long", "질문은 4000자 이하여야 합니다")
	}
	result, err := s.retrieval.Search(ctx, ownerID, query, 5)
	if err != nil {
		return nil, "", err
	}
	documents, err := s.library.GetMany(ctx, ownerID, result.DocumentIDs)
	if err != nil {
		return nil, "", err
	}
	contextText := buildContext(documents, result.Chunks)
	answer, err := s.provider.Answer(ctx, query, contextText)
	if err != nil {
		return nil, "", httpx.Internal(fmt.Errorf("generate grounded answer: %w", err))
	}
	return documents, answer, nil
}

func buildContext(documents []library.Document, chunks []retrieval.ChunkHit) string {
	allowed := make(map[uuid.UUID]int, len(documents))
	for index, document := range documents {
		if documentID, err := uuid.Parse(document.DocID); err == nil {
			allowed[documentID] = index + 1
		}
	}
	var builder strings.Builder
	used := make(map[uuid.UUID]int, len(documents))
	for _, chunk := range chunks {
		citation, ok := allowed[chunk.DocumentID]
		if !ok || used[chunk.DocumentID] >= 2 || builder.Len() >= 20000 {
			continue
		}
		used[chunk.DocumentID]++
		fmt.Fprintf(&builder, "[%d] %s\n%s\n\n", citation, chunk.Title, trimRunes(chunk.Content, 2400))
	}
	for index, document := range documents {
		documentID, _ := uuid.Parse(document.DocID)
		if used[documentID] > 0 || builder.Len() >= 20000 {
			continue
		}
		content := document.Summary
		if strings.TrimSpace(content) == "" {
			content = document.Content
		}
		fmt.Fprintf(&builder, "[%d] %s\n%s\n\n", index+1, document.Title, trimRunes(content, 2400))
	}
	return builder.String()
}

func trimRunes(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}
