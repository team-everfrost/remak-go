package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/events"
	"github.com/team-everfrost/remak-go/internal/platform/idgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

const maxScrapedContentBytes = 16 << 20

type sqsReceiver interface {
	ReceiveMessage(
		ctx context.Context,
		params *sqs.ReceiveMessageInput,
		optFns ...func(*sqs.Options),
	) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(
		ctx context.Context,
		params *sqs.DeleteMessageInput,
		optFns ...func(*sqs.Options),
	) (*sqs.DeleteMessageOutput, error)
}

type s3Reader interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type ScrapeResultConsumer struct {
	pool     *pgxpool.Pool
	queries  *dbgen.Queries
	sqs      sqsReceiver
	s3       s3Reader
	queueURL string
	bucket   string
	wait     time.Duration
	logger   *slog.Logger
}

func NewScrapeResultConsumer(
	pool *pgxpool.Pool,
	sqsClient sqsReceiver,
	s3Client s3Reader,
	queueURL, bucket string,
	wait time.Duration,
	logger *slog.Logger,
) *ScrapeResultConsumer {
	return &ScrapeResultConsumer{
		pool:     pool,
		queries:  dbgen.New(pool),
		sqs:      sqsClient,
		s3:       s3Client,
		queueURL: queueURL,
		bucket:   bucket,
		wait:     wait,
		logger:   logger,
	}
}

func (c *ScrapeResultConsumer) Poll(ctx context.Context) error {
	waitSeconds := int32(c.wait / time.Second)
	if waitSeconds < 1 {
		waitSeconds = 1
	}
	if waitSeconds > 20 {
		waitSeconds = 20
	}
	output, err := c.sqs.ReceiveMessage(
		ctx,
		&sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(c.queueURL),
			MaxNumberOfMessages: 5,
			WaitTimeSeconds:     waitSeconds,
		},
	)
	if err != nil {
		return fmt.Errorf("receive scrape results: %w", err)
	}
	for _, message := range output.Messages {
		if err := c.processMessage(ctx, message); err != nil {
			c.logger.Error("scrape result failed", "message_id", aws.ToString(message.MessageId), "error", err)
			continue
		}
		deleteInput := &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(c.queueURL),
			ReceiptHandle: message.ReceiptHandle,
		}
		if _, err := c.sqs.DeleteMessage(ctx, deleteInput); err != nil {
			c.logger.Error("delete scrape result failed", "message_id", aws.ToString(message.MessageId), "error", err)
		}
	}
	return nil
}

func (c *ScrapeResultConsumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := c.Poll(ctx); err != nil && ctx.Err() == nil {
			c.logger.Error("scrape result poll failed", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (c *ScrapeResultConsumer) processMessage(ctx context.Context, message types.Message) error {
	if message.Body == nil || message.ReceiptHandle == nil {
		return fmt.Errorf("SQS message is missing body or receipt handle")
	}
	var event events.Envelope[events.ScrapeResultData]
	if err := json.Unmarshal([]byte(*message.Body), &event); err != nil {
		return fmt.Errorf("decode scrape result: %w", err)
	}
	if event.SchemaVersion != events.SchemaVersionV1 || event.EventID == uuid.Nil || event.Data.JobID == uuid.Nil ||
		event.Data.DocumentID == uuid.Nil ||
		event.Data.DocumentVersion <= 0 {
		return fmt.Errorf("invalid scrape result envelope")
	}
	if event.EventType != events.ScrapeCompleted && event.EventType != events.ScrapeFailed {
		return fmt.Errorf("unsupported scrape result type %q", event.EventType)
	}
	if event.EventType == events.ScrapeCompleted && event.Data.Content == "" && event.Data.ContentArtifactKey != "" {
		content, err := c.readArtifact(ctx, event.Data.ContentArtifactKey)
		if err != nil {
			return err
		}
		event.Data.Content = content
	}
	job, err := c.queries.GetIngestionJob(ctx, event.Data.JobID)
	if err != nil {
		return fmt.Errorf("get scrape job: %w", err)
	}
	if job.Type != dbgen.IngestionJobTypeSCRAPE || job.DocumentID != event.Data.DocumentID ||
		job.DocumentVersion != event.Data.DocumentVersion {
		return fmt.Errorf("scrape result does not match job")
	}
	document, err := c.queries.GetDocumentByID(ctx, event.Data.DocumentID)
	documentFound := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get scrape document: %w", err)
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := c.queries.WithTx(tx)
	inboxParams := dbgen.RegisterInboxEventParams{
		EventID:   event.EventID.String(),
		EventType: event.EventType,
	}
	if _, err := queries.RegisterInboxEvent(ctx, inboxParams); errors.Is(
		err,
		pgx.ErrNoRows,
	) {
		return tx.Commit(ctx)
	} else if err != nil {
		return fmt.Errorf("register inbox event: %w", err)
	}
	current := documentFound && document.ID != uuid.Nil && document.CurrentVersion == event.Data.DocumentVersion
	terminalDuplicate := (job.State == dbgen.IngestionJobStateSUCCEEDED && event.EventType == events.ScrapeCompleted) ||
		(job.State == dbgen.IngestionJobStateFAILED && event.EventType == events.ScrapeFailed)
	if terminalDuplicate {
		if err := queries.CompleteInboxEvent(ctx, event.EventID.String()); err != nil {
			return fmt.Errorf("complete duplicate inbox event: %w", err)
		}
		return tx.Commit(ctx)
	}
	if event.EventType == events.ScrapeFailed {
		if current {
			_, _ = queries.RejectScrape(
				ctx,
				dbgen.RejectScrapeParams{ID: event.Data.DocumentID, CurrentVersion: event.Data.DocumentVersion},
			)
		}
		_, err = queries.FailJob(
			ctx,
			dbgen.FailJobParams{
				ID:               job.ID,
				LastErrorCode:    pgutil.Text(event.Data.ErrorCode),
				LastErrorMessage: pgutil.Text(truncateError(errors.New(event.Data.ErrorMessage), 1000)),
				BackoffSeconds:   0,
			},
		)
	} else {
		if event.Data.Content == "" {
			return fmt.Errorf("successful scrape result has no content")
		}
		versionParams := dbgen.CompleteDocumentVersionParams{
			DocumentID:         event.Data.DocumentID,
			Version:            event.Data.DocumentVersion,
			RawArtifactKey:     pgutil.Text(event.Data.RawArtifactKey),
			ContentArtifactKey: pgutil.Text(event.Data.ContentArtifactKey),
			ContentHash:        pgutil.Text(event.Data.ContentHash),
			Title:              pgutil.Text(event.Data.Title),
			Content:            pgutil.Text(event.Data.Content),
			ExtractionMethod:   pgutil.Text(event.Data.ExtractionMethod),
		}
		if affected, completeErr := queries.CompleteDocumentVersion(ctx, versionParams); completeErr != nil {
			err = completeErr
		} else if affected != 1 {
			err = fmt.Errorf("complete document version: affected=%d", affected)
		} else if current {
			scrapeParams := dbgen.CompleteScrapeParams{
				ID:             event.Data.DocumentID,
				CurrentVersion: event.Data.DocumentVersion,
				Title:          pgutil.Text(event.Data.Title),
				Content:        pgutil.Text(event.Data.Content),
				ThumbnailUrl:   pgutil.Text(event.Data.ThumbnailURL),
				FileSize:       event.Data.FileSize,
			}
			if affected, completeErr := queries.CompleteScrape(ctx, scrapeParams); completeErr != nil {
				err = completeErr
			} else if affected != 1 {
				err = fmt.Errorf("complete scrape: affected=%d", affected)
			}
			if err == nil {
				jobParams := dbgen.CreateIngestionJobParams{
					ID:              idgen.New(),
					DocumentID:      event.Data.DocumentID,
					DocumentVersion: event.Data.DocumentVersion,
					Type:            dbgen.IngestionJobTypeENRICH,
				}
				_, err = queries.CreateIngestionJob(ctx, jobParams)
			}
		}
		if err == nil && !documentFound {
			affected, requeueErr := queries.RequeueArtifactCleanupJob(ctx, event.Data.DocumentID)
			if requeueErr != nil {
				err = requeueErr
			} else if affected != 1 {
				err = fmt.Errorf("requeue artifact cleanup for deleted document: affected=%d", affected)
			}
		}
		if err == nil {
			_, err = queries.CompleteJob(ctx, job.ID)
		}
	}
	if err != nil {
		return fmt.Errorf("apply scrape result: %w", err)
	}
	if err := queries.CompleteInboxEvent(ctx, event.EventID.String()); err != nil {
		return fmt.Errorf("complete inbox event: %w", err)
	}
	return tx.Commit(ctx)
}

func (c *ScrapeResultConsumer) readArtifact(ctx context.Context, key string) (string, error) {
	output, err := c.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return "", fmt.Errorf("read content artifact: %w", err)
	}
	defer func() { _ = output.Body.Close() }()
	content, err := io.ReadAll(io.LimitReader(output.Body, maxScrapedContentBytes+1))
	if err != nil {
		return "", fmt.Errorf("read content artifact body: %w", err)
	}
	if len(content) > maxScrapedContentBytes {
		return "", fmt.Errorf("content artifact exceeds %d bytes", maxScrapedContentBytes)
	}
	return string(content), nil
}
