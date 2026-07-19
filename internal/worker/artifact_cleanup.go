package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

type cleanupS3 interface {
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type ArtifactCleanupProcessor struct {
	queries        *dbgen.Queries
	s3             cleanupS3
	documentBucket string
	artifactBucket string
	logger         *slog.Logger
}

func NewArtifactCleanupProcessor(
	pool *pgxpool.Pool,
	client cleanupS3,
	documentBucket, artifactBucket string,
	logger *slog.Logger,
) *ArtifactCleanupProcessor {
	return &ArtifactCleanupProcessor{
		queries:        dbgen.New(pool),
		s3:             client,
		documentBucket: documentBucket,
		artifactBucket: artifactBucket,
		logger:         logger,
	}
}

func (p *ArtifactCleanupProcessor) ProcessBatch(ctx context.Context, batchSize int32) (int, error) {
	jobs, err := p.queries.ClaimArtifactCleanupJobs(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim artifact cleanup jobs: %w", err)
	}
	for _, job := range jobs {
		if processErr := p.process(ctx, job); processErr != nil {
			backoff := int64(1 << min(job.AttemptCount, 8))
			if _, failErr := p.queries.FailArtifactCleanupJob(ctx, dbgen.FailArtifactCleanupJobParams{
				ID: job.ID, LastError: pgutil.Text(truncateError(processErr, 1000)), BackoffSeconds: backoff,
			}); failErr != nil {
				return len(jobs), fmt.Errorf("fail artifact cleanup job: %w", failErr)
			}
			p.logger.Error(
				"artifact cleanup failed",
				"job_id",
				job.ID,
				"document_id",
				job.DocumentID,
				"error",
				processErr,
			)
		}
	}
	return len(jobs), nil
}

func (p *ArtifactCleanupProcessor) process(ctx context.Context, job dbgen.ArtifactCleanupJob) error {
	artifacts, err := p.queries.ListDocumentArtifactKeys(ctx, job.DocumentID)
	if err != nil {
		return fmt.Errorf("list document artifacts: %w", err)
	}
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		key := pgutil.String(artifact.Key)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		bucket := p.documentBucket
		if artifact.Type == dbgen.DocumentTypeWEBPAGE {
			bucket = p.artifactBucket
		}
		input := &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}
		if _, err := p.s3.DeleteObject(ctx, input); err != nil {
			return fmt.Errorf("delete s3://%s/%s: %w", bucket, key, err)
		}
	}
	if affected, err := p.queries.CompleteArtifactCleanupJob(ctx, job.ID); err != nil {
		return fmt.Errorf("complete artifact cleanup: %w", err)
	} else if affected != 1 {
		return fmt.Errorf("complete artifact cleanup: affected=%d", affected)
	}
	return nil
}
