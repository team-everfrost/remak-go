package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/team-everfrost/remak-go/internal/enrichment"
	"github.com/team-everfrost/remak-go/internal/platform/awsx"
	"github.com/team-everfrost/remak-go/internal/platform/config"
	"github.com/team-everfrost/remak-go/internal/platform/database"
	"github.com/team-everfrost/remak-go/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	startupCtx := context.Background()
	pool, err := database.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	clients, err := awsx.New(startupCtx, cfg.AWSRegion, cfg.AWSEndpointURL)
	if err != nil {
		pool.Close()
		logger.Error("create AWS clients", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer pool.Close()

	var provider enrichment.Provider = enrichment.NewHashProvider(cfg.EmbeddingDimensions)
	if cfg.AIBaseURL != "" {
		provider = enrichment.NewHTTPProvider(cfg.AIBaseURL, cfg.AIAPIKey, cfg.EmbeddingModel, cfg.ChatModel, cfg.EmbeddingDimensions)
	}
	logger.Info("worker starting", "embedding_provider", provider.Name())
	dispatcher := worker.NewOutboxDispatcher(pool, clients.SQS, cfg.ScrapeRequestQueueURL, logger)
	consumer := worker.NewScrapeResultConsumer(pool, clients.SQS, clients.S3, cfg.ScrapeResultQueueURL, cfg.ArtifactBucket, cfg.QueuePollWait, logger)
	extractor := enrichment.NewS3ArtifactExtractor(clients.S3, cfg.DocumentBucket, provider)
	processor := enrichment.NewProcessor(pool, provider, extractor, cfg.EnrichmentBatchSize, logger)
	cleanup := worker.NewArtifactCleanupProcessor(pool, clients.S3, cfg.DocumentBucket, cfg.ArtifactBucket, logger)
	maintenance := worker.NewMaintenanceProcessor(pool, logger)

	go dispatcher.Run(ctx, cfg.OutboxPollInterval)
	go consumer.Run(ctx)
	go runEnrichment(ctx, processor, logger)
	go runArtifactCleanup(ctx, cleanup, logger)
	go runMaintenance(ctx, maintenance, logger)
	<-ctx.Done()
	logger.Info("worker shutting down")
}

func runMaintenance(ctx context.Context, processor *worker.MaintenanceProcessor, logger *slog.Logger) {
	if err := processor.RunOnce(ctx); err != nil && ctx.Err() == nil {
		logger.Error("maintenance failed", "error", err)
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := processor.RunOnce(ctx); err != nil && ctx.Err() == nil {
				logger.Error("maintenance failed", "error", err)
			}
		}
	}
}

func runArtifactCleanup(ctx context.Context, processor *worker.ArtifactCleanupProcessor, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		count, err := processor.ProcessBatch(ctx, 10)
		if err != nil && ctx.Err() == nil {
			logger.Error("artifact cleanup batch failed", "error", err)
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

func runEnrichment(ctx context.Context, processor *enrichment.Processor, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		count, err := processor.ProcessBatch(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("enrichment batch failed", "error", err)
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
