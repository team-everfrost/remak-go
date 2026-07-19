package enrichment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/platform/idgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

type Processor struct {
	pool      *pgxpool.Pool
	queries   *dbgen.Queries
	provider  Provider
	extractor ArtifactExtractor
	batchSize int32
	logger    *slog.Logger
}

func NewProcessor(
	pool *pgxpool.Pool,
	provider Provider,
	extractor ArtifactExtractor,
	batchSize int32,
	logger *slog.Logger,
) *Processor {
	return &Processor{
		pool:      pool,
		queries:   dbgen.New(pool),
		provider:  provider,
		extractor: extractor,
		batchSize: batchSize,
		logger:    logger,
	}
}

func (p *Processor) ProcessBatch(ctx context.Context) (int, error) {
	jobs, err := p.queries.ClaimEnrichmentJobs(ctx, p.batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim enrichment jobs: %w", err)
	}
	for _, job := range jobs {
		if err := p.process(ctx, job); err != nil {
			p.logger.Error(
				"enrichment job failed",
				"job_id",
				job.ID,
				"document_id",
				job.DocumentID,
				"attempt",
				job.AttemptCount,
				"error",
				err,
			)
			backoff := int64(1 << minAttempt(job.AttemptCount, 8))
			_, failErr := p.queries.FailJob(
				ctx,
				dbgen.FailJobParams{
					ID:               job.ID,
					LastErrorCode:    pgutil.Text("enrichment_failed"),
					LastErrorMessage: pgutil.Text(truncate(err.Error(), 1000)),
					BackoffSeconds:   backoff,
				},
			)
			_, _ = p.queries.RejectEnrichment(
				ctx,
				dbgen.RejectEnrichmentParams{ID: job.DocumentID, CurrentVersion: job.DocumentVersion},
			)
			if failErr != nil {
				return len(jobs), fmt.Errorf("fail enrichment job: %w", failErr)
			}
		}
	}
	return len(jobs), nil
}

func (p *Processor) process(ctx context.Context, job dbgen.IngestionJob) error {
	version, err := p.queries.GetDocumentVersion(
		ctx,
		dbgen.GetDocumentVersionParams{DocumentID: job.DocumentID, Version: job.DocumentVersion},
	)
	if err != nil {
		return fmt.Errorf("get document version: %w", err)
	}
	document, err := p.queries.GetDocumentByID(ctx, job.DocumentID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, completeErr := p.queries.CompleteJob(ctx, job.ID)
		return completeErr
	}
	if err != nil {
		return fmt.Errorf("get document: %w", err)
	}
	if document.CurrentVersion != job.DocumentVersion {
		_, err := p.queries.CompleteJob(ctx, job.ID)
		return err
	}
	content := pgutil.String(version.Content)
	if content == "" {
		content = pgutil.String(document.Content)
	}
	extractionMethod := pgutil.String(version.ExtractionMethod)
	if strings.TrimSpace(content) == "" && p.extractor != nil {
		content, extractionMethod, err = p.extractor.Extract(ctx, document, version)
		if err != nil {
			return fmt.Errorf("extract artifact: %w", err)
		}
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("document content is empty")
	}
	chunks := ChunkText(content)
	embeddings, err := p.provider.Embed(ctx, chunks)
	if err != nil {
		return fmt.Errorf("embed chunks: %w", err)
	}
	if len(embeddings) != len(chunks) {
		return fmt.Errorf("embedding count mismatch")
	}
	analysis, err := p.provider.Analyze(ctx, pgutil.String(document.Title), content)
	if err != nil {
		return fmt.Errorf("analyze document: %w", err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := p.queries.WithTx(tx)
	_, _ = queries.MarkDocumentEnrichProcessing(
		ctx,
		dbgen.MarkDocumentEnrichProcessingParams{ID: document.ID, CurrentVersion: job.DocumentVersion},
	)
	if !version.Content.Valid || version.Content.String != content {
		digest := sha256.Sum256([]byte(content))
		if affected, saveErr := queries.SaveExtractedContent(ctx, dbgen.SaveExtractedContentParams{
			DocumentID: document.ID, Version: job.DocumentVersion, Content: pgutil.Text(content),
			ContentHash: pgutil.Text(hex.EncodeToString(digest[:])), ExtractionMethod: pgutil.Text(extractionMethod),
		}); saveErr != nil {
			return fmt.Errorf("save extracted content: %w", saveErr)
		} else if affected != 1 {
			return fmt.Errorf("save extracted content: affected=%d", affected)
		}
	}
	replaceParams := dbgen.ReplaceChunksParams{
		DocumentID:      document.ID,
		DocumentVersion: job.DocumentVersion,
	}
	if err := queries.ReplaceChunks(ctx, replaceParams); err != nil {
		return fmt.Errorf("replace chunks: %w", err)
	}
	for index, chunk := range chunks {
		params := dbgen.CreateChunkParams{
			ID:              idgen.New(),
			DocumentID:      document.ID,
			DocumentVersion: job.DocumentVersion,
			ChunkIndex:      int32(index),
			Content:         chunk,
			EmbeddingModel:  p.provider.Name(),
			Embedding:       pgvector.NewVector(embeddings[index]),
		}
		if err := queries.CreateChunk(ctx, params); err != nil {
			return fmt.Errorf("create chunk %d: %w", index, err)
		}
	}
	if err := queries.DeleteDocumentTags(ctx, document.ID); err != nil {
		return fmt.Errorf("replace document tags: %w", err)
	}
	for _, name := range analysis.Tags {
		tag, createErr := queries.CreateTag(
			ctx,
			dbgen.CreateTagParams{ID: idgen.New(), OwnerID: document.OwnerID, Name: name},
		)
		if createErr != nil {
			return fmt.Errorf("create suggested tag: %w", createErr)
		}
		params := dbgen.AddTagToDocumentParams{
			DocumentID: document.ID,
			TagID:      tag.ID,
			OwnerID:    document.OwnerID,
		}
		if addErr := queries.AddTagToDocument(ctx, params); addErr != nil {
			return fmt.Errorf("attach suggested tag: %w", addErr)
		}
	}
	if err := queries.DeleteUnusedTags(ctx, document.OwnerID); err != nil {
		return fmt.Errorf("delete unused tags: %w", err)
	}
	versionParams := dbgen.CompleteEnrichedVersionParams{
		DocumentID: document.ID,
		Version:    job.DocumentVersion,
		Summary:    pgutil.Text(analysis.Summary),
	}
	if affected, err := queries.CompleteEnrichedVersion(ctx, versionParams); err != nil {
		return fmt.Errorf("complete enriched version: %w", err)
	} else if affected != 1 {
		return fmt.Errorf("complete enriched version: affected=%d", affected)
	}
	documentParams := dbgen.CompleteEnrichmentParams{
		ID:             document.ID,
		CurrentVersion: job.DocumentVersion,
		Summary:        pgutil.Text(analysis.Summary),
	}
	if affected, err := queries.CompleteEnrichment(ctx, documentParams); err != nil {
		return fmt.Errorf("complete enrichment: %w", err)
	} else if affected != 1 {
		return fmt.Errorf("complete enrichment: affected=%d", affected)
	}
	if affected, err := queries.CompleteJob(ctx, job.ID); err != nil {
		return fmt.Errorf("complete job: %w", err)
	} else if affected != 1 {
		return fmt.Errorf("complete job: affected=%d", affected)
	}
	return tx.Commit(ctx)
}

func truncate(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func minAttempt(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}
