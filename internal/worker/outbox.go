package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/events"
	"github.com/team-everfrost/remak-go/internal/platform/idgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

type sqsSender interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

type OutboxDispatcher struct {
	pool     *pgxpool.Pool
	queries  *dbgen.Queries
	sqs      sqsSender
	queueURL string
	lockID   uuid.UUID
	logger   *slog.Logger
}

func NewOutboxDispatcher(pool *pgxpool.Pool, sqsClient sqsSender, queueURL string, logger *slog.Logger) *OutboxDispatcher {
	return &OutboxDispatcher{pool: pool, queries: dbgen.New(pool), sqs: sqsClient, queueURL: queueURL, lockID: idgen.New(), logger: logger}
}

func (d *OutboxDispatcher) DispatchBatch(ctx context.Context, batchSize int32) (int, error) {
	lockToken := pgtype.UUID{Bytes: [16]byte(d.lockID), Valid: true}
	claimed, err := d.queries.ClaimOutboxEvents(ctx, dbgen.ClaimOutboxEventsParams{LockToken: lockToken, BatchSize: batchSize})
	if err != nil {
		return 0, fmt.Errorf("claim outbox events: %w", err)
	}
	for _, event := range claimed {
		if event.EventType != events.ScrapeRequested {
			d.deadLetter(ctx, event, lockToken, fmt.Errorf("unsupported outbox event type %q", event.EventType))
			continue
		}
		var request events.Envelope[events.ScrapeRequestData]
		if err := json.Unmarshal(event.Payload, &request); err != nil {
			d.deadLetter(ctx, event, lockToken, fmt.Errorf("decode scrape request outbox payload: %w", err))
			continue
		}
		if request.SchemaVersion != events.SchemaVersionV1 ||
			request.EventID != event.ID || request.EventType != event.EventType ||
			request.Data.JobID == uuid.Nil || request.Data.DocumentID != event.AggregateID ||
			request.Data.DocumentVersion <= 0 || request.Data.URL == "" {
			d.deadLetter(ctx, event, lockToken, errors.New("invalid scrape request outbox payload envelope"))
			continue
		}
		_, sendErr := d.sqs.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: aws.String(d.queueURL), MessageBody: aws.String(string(event.Payload))})
		if sendErr == nil {
			if markErr := d.markPublishedAndProcessing(ctx, event, request.Data, lockToken); markErr != nil {
				return len(claimed), markErr
			}
			continue
		}
		if event.AttemptCount >= 9 {
			d.deadLetter(ctx, event, lockToken, sendErr)
			continue
		}
		backoff := int64(1 << min32(event.AttemptCount+1, 8))
		if err := d.queries.RescheduleOutboxEvent(ctx, dbgen.RescheduleOutboxEventParams{ID: event.ID, BackoffSeconds: backoff, LastError: pgutil.Text(truncateError(sendErr, 1000)), LockToken: lockToken}); err != nil {
			return len(claimed), fmt.Errorf("reschedule outbox event: %w", err)
		}
	}
	return len(claimed), nil
}

func (d *OutboxDispatcher) markPublishedAndProcessing(ctx context.Context, event dbgen.OutboxEvent, request events.ScrapeRequestData, lockToken pgtype.UUID) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin outbox publish transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := d.queries.WithTx(tx)
	if affected, markErr := queries.MarkOutboxPublished(ctx, dbgen.MarkOutboxPublishedParams{ID: event.ID, LockToken: lockToken}); markErr != nil {
		return fmt.Errorf("mark outbox published: %w", markErr)
	} else if affected != 1 {
		return fmt.Errorf("mark outbox published: affected=%d", affected)
	}
	if _, markErr := queries.MarkJobProcessing(ctx, request.JobID); markErr != nil {
		return fmt.Errorf("mark scrape job processing: %w", markErr)
	}
	if _, markErr := queries.MarkDocumentProcessing(ctx, dbgen.MarkDocumentProcessingParams{ID: request.DocumentID, CurrentVersion: request.DocumentVersion}); markErr != nil {
		return fmt.Errorf("mark scrape document processing: %w", markErr)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit outbox publish transaction: %w", err)
	}
	return nil
}

func (d *OutboxDispatcher) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		count, err := d.DispatchBatch(ctx, 10)
		if err != nil && ctx.Err() == nil {
			d.logger.Error("outbox dispatch failed", "error", err)
		}
		if count > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *OutboxDispatcher) deadLetter(ctx context.Context, event dbgen.OutboxEvent, lockToken pgtype.UUID, cause error) {
	_, err := d.queries.DeadLetterOutboxEvent(ctx, dbgen.DeadLetterOutboxEventParams{ID: event.ID, LastError: pgutil.Text(truncateError(cause, 1000)), LockToken: lockToken})
	if err != nil {
		d.logger.Error("dead-letter outbox event failed", "event_id", event.ID, "error", err)
		return
	}
	d.logger.Error("outbox event dead-lettered", "event_id", event.ID, "event_type", event.EventType, "error", cause)
}

func truncateError(err error, maximum int) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}
