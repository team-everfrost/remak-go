package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment            string
	HTTPAddr               string
	DatabaseURL            string
	JWTSecret              string
	JWTIssuer              string
	ChallengeSecret        string
	ExposeDebugCodes       bool
	AllowedOrigins         []string
	AccessTokenTTL         time.Duration
	RefreshTokenTTL        time.Duration
	AWSRegion              string
	AWSEndpointURL         string
	ScrapeRequestQueueURL  string
	ScrapeResultQueueURL   string
	ArtifactBucket         string
	DocumentBucket         string
	EmailFrom              string
	QueuePollWait          time.Duration
	OutboxPollInterval     time.Duration
	WorkerShutdownTimeout  time.Duration
	MaxDocumentUploadBytes int64
	AIBaseURL              string
	AIAPIKey               string
	EmbeddingModel         string
	EmbeddingDimensions    int
	ChatModel              string
	EnrichmentBatchSize    int32
}

func Load() (Config, error) {
	cfg := Config{
		Environment:            value("APP_ENV", "development"),
		HTTPAddr:               value("HTTP_ADDR", ":8080"),
		DatabaseURL:            value("DATABASE_URL", "postgres://remak:remak@localhost:5432/remak?sslmode=disable"),
		JWTSecret:              value("JWT_SECRET", "local-development-secret-change-me"),
		JWTIssuer:              value("JWT_ISSUER", "remak-local"),
		ChallengeSecret:        value("CHALLENGE_SECRET", "local-challenge-secret-change-me"),
		AllowedOrigins:         splitCSV(value("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		AWSRegion:              value("AWS_REGION", "ap-northeast-2"),
		AWSEndpointURL:         os.Getenv("AWS_ENDPOINT_URL"),
		ScrapeRequestQueueURL:  value("SCRAPE_REQUEST_QUEUE_URL", "http://localhost:4566/000000000000/remak-scrape-request"),
		ScrapeResultQueueURL:   value("SCRAPE_RESULT_QUEUE_URL", "http://localhost:4566/000000000000/remak-scrape-result"),
		ArtifactBucket:         value("ARTIFACT_BUCKET", "remak-artifacts"),
		DocumentBucket:         value("DOCUMENT_BUCKET", "remak-documents"),
		EmailFrom:              strings.TrimSpace(os.Getenv("EMAIL_FROM")),
		WorkerShutdownTimeout:  15 * time.Second,
		MaxDocumentUploadBytes: 100 * 1024 * 1024,
		AIBaseURL:              value("AI_BASE_URL", ""),
		AIAPIKey:               os.Getenv("AI_API_KEY"),
		EmbeddingModel:         value("EMBEDDING_MODEL", "text-embedding-3-small"),
		EmbeddingDimensions:    1536,
		ChatModel:              value("CHAT_MODEL", "gpt-4.1-mini"),
		EnrichmentBatchSize:    4,
	}

	var err error
	if cfg.AccessTokenTTL, err = duration("ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTokenTTL, err = duration("REFRESH_TOKEN_TTL", 30*24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.QueuePollWait, err = duration("QUEUE_POLL_WAIT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.OutboxPollInterval, err = duration("OUTBOX_POLL_INTERVAL", time.Second); err != nil {
		return Config{}, err
	}
	if raw := os.Getenv("MAX_DOCUMENT_UPLOAD_BYTES"); raw != "" {
		cfg.MaxDocumentUploadBytes, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse MAX_DOCUMENT_UPLOAD_BYTES: %w", err)
		}
	}
	if raw := os.Getenv("EMBEDDING_DIMENSIONS"); raw != "" {
		cfg.EmbeddingDimensions, err = strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parse EMBEDDING_DIMENSIONS: %w", err)
		}
	}
	if raw := os.Getenv("ENRICHMENT_BATCH_SIZE"); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 32)
		if parseErr != nil {
			return Config{}, fmt.Errorf("parse ENRICHMENT_BATCH_SIZE: %w", parseErr)
		}
		cfg.EnrichmentBatchSize = int32(parsed)
	}
	if cfg.ExposeDebugCodes, err = strconv.ParseBool(value("EXPOSE_DEBUG_VERIFICATION_CODES", "false")); err != nil {
		return Config{}, fmt.Errorf("parse EXPOSE_DEBUG_VERIFICATION_CODES: %w", err)
	}

	if cfg.Environment == "production" && cfg.JWTSecret == "local-development-secret-change-me" {
		return Config{}, errors.New("JWT_SECRET must be changed in production")
	}
	if len(cfg.JWTSecret) < 24 {
		return Config{}, errors.New("JWT_SECRET must contain at least 24 characters")
	}
	if len(cfg.ChallengeSecret) < 24 {
		return Config{}, errors.New("CHALLENGE_SECRET must contain at least 24 characters")
	}
	if cfg.Environment == "production" && cfg.ExposeDebugCodes {
		return Config{}, errors.New("EXPOSE_DEBUG_VERIFICATION_CODES cannot be enabled in production")
	}
	if cfg.Environment == "production" && cfg.EmailFrom == "" {
		return Config{}, errors.New("EMAIL_FROM must be configured in production")
	}
	if cfg.Environment == "production" && cfg.AIBaseURL == "" {
		return Config{}, errors.New("AI_BASE_URL must be configured in production")
	}
	if cfg.EmbeddingDimensions != 1536 {
		return Config{}, errors.New("EMBEDDING_DIMENSIONS must match the vector(1536) schema")
	}
	return cfg, nil
}

func value(key, fallback string) string {
	if current := os.Getenv(key); current != "" {
		return current
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func splitCSV(raw string) []string {
	values := strings.Split(raw, ",")
	result := make([]string, 0, len(values))
	for _, current := range values {
		if current = strings.TrimSpace(current); current != "" {
			result = append(result, current)
		}
	}
	return result
}
