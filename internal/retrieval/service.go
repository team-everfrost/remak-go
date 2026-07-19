package retrieval

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"

	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/enrichment"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

const (
	defaultCandidateLimit int32   = 50
	rrfConstant           float64 = 60.0
)

type ChunkHit struct {
	DocumentID uuid.UUID
	Title      string
	Content    string
	Score      float64
}

type Result struct {
	DocumentIDs []uuid.UUID
	Chunks      []ChunkHit
}

type Service struct {
	queries  *dbgen.Queries
	provider enrichment.Provider
	logger   *slog.Logger
}

func NewService(queries *dbgen.Queries, provider enrichment.Provider, logger *slog.Logger) *Service {
	return &Service{queries: queries, provider: provider, logger: logger}
}

func (s *Service) Search(ctx context.Context, ownerID uuid.UUID, rawQuery string, limit int) (Result, error) {
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		return Result{DocumentIDs: []uuid.UUID{}, Chunks: []ChunkHit{}}, nil
	}
	if len([]rune(query)) > 1000 {
		return Result{}, httpx.BadRequest("query_too_long", "검색어는 1000자 이하여야 합니다")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}

	lexical, err := s.queries.SearchDocumentsLexical(ctx, dbgen.SearchDocumentsLexicalParams{
		OwnerID: ownerID, Query: query, CandidateLimit: defaultCandidateLimit,
	})
	if err != nil {
		return Result{}, httpx.Internal(fmt.Errorf("lexical retrieval: %w", err))
	}

	semantic := make([]dbgen.SearchChunksVectorRow, 0)
	vectors, embedErr := s.provider.Embed(ctx, []string{query})
	if embedErr == nil && len(vectors) == 1 {
		semantic, err = s.queries.SearchChunksVector(ctx, dbgen.SearchChunksVectorParams{
			OwnerID: ownerID, Embedding: pgvector.NewVector(vectors[0]), CandidateLimit: defaultCandidateLimit,
		})
		if err != nil {
			return Result{}, httpx.Internal(fmt.Errorf("semantic retrieval: %w", err))
		}
	} else if s.logger != nil {
		s.logger.Warn("semantic retrieval degraded to lexical", "error", embedErr)
	}

	lexicalIDs := make([]uuid.UUID, len(lexical))
	for index, candidate := range lexical {
		lexicalIDs[index] = candidate.DocumentID
	}
	semanticIDs := make([]uuid.UUID, 0, len(semantic))
	seenDocuments := make(map[uuid.UUID]struct{}, len(semantic))
	chunks := make([]ChunkHit, 0, minInt(len(semantic), 12))
	for _, candidate := range semantic {
		if len(chunks) < 12 {
			chunks = append(
				chunks,
				ChunkHit{
					DocumentID: candidate.DocumentID,
					Title:      pgutil.String(candidate.DocumentTitle),
					Content:    candidate.Content,
					Score:      candidate.Score,
				},
			)
		}
		if _, exists := seenDocuments[candidate.DocumentID]; exists {
			continue
		}
		seenDocuments[candidate.DocumentID] = struct{}{}
		semanticIDs = append(semanticIDs, candidate.DocumentID)
	}

	return Result{DocumentIDs: fuseRanks(lexicalIDs, semanticIDs, limit), Chunks: chunks}, nil
}

type fusedScore struct {
	id    uuid.UUID
	score float64
}

func fuseRanks(lexical, semantic []uuid.UUID, limit int) []uuid.UUID {
	scores := make(map[uuid.UUID]float64, len(lexical)+len(semantic))
	for rank, documentID := range lexical {
		scores[documentID] += 1 / (rrfConstant + float64(rank+1))
	}
	for rank, documentID := range semantic {
		scores[documentID] += 1 / (rrfConstant + float64(rank+1))
	}
	ordered := make([]fusedScore, 0, len(scores))
	for documentID, score := range scores {
		ordered = append(ordered, fusedScore{id: documentID, score: score})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score == ordered[j].score {
			return ordered[i].id.String() < ordered[j].id.String()
		}
		return ordered[i].score > ordered[j].score
	})
	if limit > len(ordered) {
		limit = len(ordered)
	}
	result := make([]uuid.UUID, limit)
	for index := 0; index < limit; index++ {
		result[index] = ordered[index].id
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
