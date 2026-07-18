package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/team-everfrost/remak-go/internal/dbgen"
)

type MaintenanceProcessor struct {
	queries *dbgen.Queries
	logger  *slog.Logger
}

func NewMaintenanceProcessor(pool *pgxpool.Pool, logger *slog.Logger) *MaintenanceProcessor {
	return &MaintenanceProcessor{queries: dbgen.New(pool), logger: logger}
}

func (p *MaintenanceProcessor) RunOnce(ctx context.Context) error {
	operations := []struct {
		name string
		run  func(context.Context) (int64, error)
	}{
		{"verification_challenges", p.queries.DeleteOldVerificationChallenges},
		{"refresh_tokens", p.queries.DeleteExpiredRefreshTokens},
		{"inbox_events", p.queries.DeleteOldInboxEvents},
		{"outbox_events", p.queries.DeleteOldOutboxEvents},
		{"ingestion_jobs", p.queries.DeleteOldIngestionJobs},
		{"documents", p.queries.HardDeleteCleanedDocuments},
		{"accounts", p.queries.HardDeleteWithdrawnAccounts},
	}
	for _, operation := range operations {
		deleted, err := operation.run(ctx)
		if err != nil {
			return fmt.Errorf("maintain %s: %w", operation.name, err)
		}
		if deleted > 0 {
			p.logger.Info("maintenance deleted rows", "table", operation.name, "count", deleted)
		}
	}
	return nil
}
