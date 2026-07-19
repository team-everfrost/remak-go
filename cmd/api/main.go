package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/team-everfrost/remak-go/internal/account"
	"github.com/team-everfrost/remak-go/internal/chat"
	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/enrichment"
	"github.com/team-everfrost/remak-go/internal/filestore"
	"github.com/team-everfrost/remak-go/internal/identity"
	"github.com/team-everfrost/remak-go/internal/library"
	"github.com/team-everfrost/remak-go/internal/platform/awsx"
	"github.com/team-everfrost/remak-go/internal/platform/config"
	"github.com/team-everfrost/remak-go/internal/platform/database"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
	"github.com/team-everfrost/remak-go/internal/retrieval"
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

	tokens := identity.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	var codeSender identity.CodeSender = identity.NoopCodeSender{}
	if cfg.EmailFrom != "" {
		codeSender = identity.NewSESCodeSender(clients.SES, cfg.EmailFrom)
	}
	identityHandler := identity.NewHandler(
		identity.NewService(pool, tokens, codeSender, cfg.ChallengeSecret, cfg.ExposeDebugCodes),
	)
	accountHandler := account.NewHandler(account.NewService(pool))
	var provider enrichment.Provider = enrichment.NewHashProvider(cfg.EmbeddingDimensions)
	if cfg.AIBaseURL != "" {
		provider = enrichment.NewHTTPProvider(
			cfg.AIBaseURL,
			cfg.AIAPIKey,
			cfg.EmbeddingModel,
			cfg.ChatModel,
			cfg.EmbeddingDimensions,
		)
	}
	libraryService := library.NewService(pool)
	retrievalService := retrieval.NewService(dbgen.New(pool), provider, logger)
	fileService := filestore.NewService(pool, clients.S3, cfg.DocumentBucket, cfg.MaxDocumentUploadBytes)
	libraryHandler := library.NewHandler(libraryService, retrievalService, fileService)
	chatHandler := chat.NewHandler(chat.NewService(retrievalService, libraryService, provider))
	accountQueries := dbgen.New(pool)
	authenticate := httpx.Authenticate(tokens, func(ctx context.Context, accountID uuid.UUID) (bool, error) {
		_, err := accountQueries.GetAccountByID(ctx, accountID)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	})

	router := chi.NewRouter()
	router.Use(httpx.RequestID)
	router.Use(httpx.Recover(logger))
	router.Use(httpx.AccessLog(logger))
	router.Use(httpx.CORS(cfg.AllowedOrigins))
	router.Get("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.WriteError(w, httpx.Internal(err))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	router.Mount("/auth", identityHandler.Routes(authenticate))
	router.Group(func(protected chi.Router) {
		protected.Use(authenticate)
		protected.Mount("/user", accountHandler.Routes())
		protected.Mount("/document", libraryHandler.DocumentRoutes())
		protected.Mount("/tag", libraryHandler.TagRoutes())
		protected.Mount("/collection", libraryHandler.CollectionRoutes())
		protected.Mount("/chat", chatHandler.Routes())
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		logger.Info("api listening", "address", cfg.HTTPAddr, "environment", cfg.Environment)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api stopped unexpectedly", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.WorkerShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("api shutdown", "error", err)
	}
}
