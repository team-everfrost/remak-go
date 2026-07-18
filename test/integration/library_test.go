//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pgvector/pgvector-go"
	"github.com/pressly/goose/v3"
	"github.com/team-everfrost/remak-go/db/migrations"
	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/enrichment"
	"github.com/team-everfrost/remak-go/internal/identity"
	"github.com/team-everfrost/remak-go/internal/library"
	"github.com/team-everfrost/remak-go/internal/platform/idgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
	"github.com/team-everfrost/remak-go/internal/retrieval"
	"golang.org/x/crypto/bcrypt"
)

func TestDocumentTransactionsAndStableCursor(t *testing.T) {
	ctx := context.Background()
	databaseURL := testDatabaseURL()
	migrate(t, databaseURL)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	queries := dbgen.New(pool)
	ownerID := createAccount(t, ctx, queries, "library-integration@example.com")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM accounts WHERE id=$1", ownerID) })
	service := library.NewService(pool)

	memo, err := service.CreateMemo(ctx, ownerID, library.MemoInput{Content: "cursor test memo"})
	if err != nil {
		t.Fatal(err)
	}
	webpage, err := service.CreateWebpage(ctx, ownerID, "trace-integration", library.WebpageInput{Title: "Example", URL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if memo.Status != "ENRICH_PENDING" || webpage.Status != "SCRAPE_PENDING" {
		t.Fatalf("unexpected initial states: memo=%s webpage=%s", memo.Status, webpage.Status)
	}
	var versionCount, jobCount, outboxCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM document_versions WHERE document_id IN ($1,$2)", uuid.MustParse(memo.DocID), uuid.MustParse(webpage.DocID)).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM ingestion_jobs WHERE document_id IN ($1,$2)", uuid.MustParse(memo.DocID), uuid.MustParse(webpage.DocID)).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE aggregate_id=$1", uuid.MustParse(webpage.DocID)).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if versionCount != 2 || jobCount != 2 || outboxCount != 1 {
		t.Fatalf("transactional records mismatch: versions=%d jobs=%d outbox=%d", versionCount, jobCount, outboxCount)
	}

	firstPage, err := service.List(ctx, ownerID, library.Cursor{Limit: 1})
	if err != nil || len(firstPage) != 1 {
		t.Fatalf("first page: %#v, %v", firstPage, err)
	}
	// Updating the first page item must not reorder it because the cursor is based
	// on immutable created_at + id, not updated_at.
	if _, err := pool.Exec(ctx, "UPDATE documents SET updated_at=now()+interval '1 day' WHERE id=$1", uuid.MustParse(firstPage[0].DocID)); err != nil {
		t.Fatal(err)
	}
	secondPage, err := service.List(ctx, ownerID, library.Cursor{Limit: 1, DocID: firstPage[0].DocID})
	if err != nil || len(secondPage) != 1 {
		t.Fatalf("second page: %#v, %v", secondPage, err)
	}
	if secondPage[0].DocID == firstPage[0].DocID {
		t.Fatal("stable cursor returned a duplicate after updated_at changed")
	}

	if err := service.Delete(ctx, ownerID, uuid.MustParse(memo.DocID)); err != nil {
		t.Fatal(err)
	}
	var cleanupJobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM artifact_cleanup_jobs WHERE document_id=$1 AND state='QUEUED'", uuid.MustParse(memo.DocID)).Scan(&cleanupJobs); err != nil {
		t.Fatal(err)
	}
	if cleanupJobs != 1 {
		t.Fatalf("soft delete must atomically schedule one artifact cleanup job, got %d", cleanupJobs)
	}
}

func TestCollectionRejectsForeignDocuments(t *testing.T) {
	ctx := context.Background()
	databaseURL := testDatabaseURL()
	migrate(t, databaseURL)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	queries := dbgen.New(pool)
	ownerID := createAccount(t, ctx, queries, "collection-owner@example.com")
	foreignID := createAccount(t, ctx, queries, "collection-foreign@example.com")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM accounts WHERE id IN ($1,$2)", ownerID, foreignID)
	})
	service := library.NewService(pool)
	foreignDocument, err := service.CreateMemo(ctx, foreignID, library.MemoInput{Content: "private foreign memo"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateCollection(ctx, ownerID, library.CreateCollectionInput{Name: "mine", DocIDs: []string{foreignDocument.DocID}})
	if err == nil {
		t.Fatal("foreign document must not be added to another account's collection")
	}
}

func TestHybridRetrievalUsesPGVectorOnPostgres18(t *testing.T) {
	ctx := context.Background()
	databaseURL := testDatabaseURL()
	migrate(t, databaseURL)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	queries := dbgen.New(pool)
	ownerID := createAccount(t, ctx, queries, "retrieval-integration@example.com")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM accounts WHERE id=$1", ownerID) })

	libraryService := library.NewService(pool)
	document, err := libraryService.CreateMemo(ctx, ownerID, library.MemoInput{Content: "MiniStack is a lightweight local AWS S3 and SQS emulator."})
	if err != nil {
		t.Fatal(err)
	}
	documentID := uuid.MustParse(document.DocID)
	provider := enrichment.NewHashProvider(1536)
	vectors, err := provider.Embed(ctx, []string{"MiniStack is a lightweight local AWS S3 and SQS emulator."})
	if err != nil {
		t.Fatal(err)
	}
	if err := queries.CreateChunk(ctx, dbgen.CreateChunkParams{
		ID: idgen.New(), DocumentID: documentID, DocumentVersion: 1, ChunkIndex: 0,
		Content:        "MiniStack is a lightweight local AWS S3 and SQS emulator.",
		EmbeddingModel: provider.Name(), Embedding: pgvector.NewVector(vectors[0]),
	}); err != nil {
		t.Fatal(err)
	}

	retrievalService := retrieval.NewService(queries, provider, slog.New(slog.NewTextHandler(io.Discard, nil)))
	result, err := retrievalService.Search(ctx, ownerID, "MiniStack SQS emulator", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DocumentIDs) == 0 || result.DocumentIDs[0] != documentID {
		t.Fatalf("expected %s first, got %v", documentID, result.DocumentIDs)
	}
	if len(result.Chunks) == 0 || result.Chunks[0].DocumentID != documentID {
		t.Fatalf("expected a semantic chunk for %s, got %#v", documentID, result.Chunks)
	}
}

func TestWithdrawSoftDeletesDocumentsAndSchedulesCleanup(t *testing.T) {
	ctx := context.Background()
	databaseURL := testDatabaseURL()
	migrate(t, databaseURL)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	queries := dbgen.New(pool)
	accountID := createAccount(t, ctx, queries, "withdraw-integration@example.com")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM accounts WHERE id=$1", accountID) })
	var originalEmail string
	if err := pool.QueryRow(ctx, "SELECT email::text FROM accounts WHERE id=$1", accountID).Scan(&originalEmail); err != nil {
		t.Fatal(err)
	}
	document, err := library.NewService(pool).CreateMemo(ctx, accountID, library.MemoInput{Content: "delete all my account data"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := identity.NewTokenManager("integration-jwt-secret-long-enough", "integration", 15*time.Minute, 24*time.Hour)
	service := identity.NewService(pool, tokens, identity.NoopCodeSender{}, "integration-challenge-secret-long-enough", true)
	challenge, err := service.RequestWithdrawCode(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.VerifyWithdrawCode(ctx, accountID, identity.VerifyCodeInput{ChallengeID: challenge.ChallengeID, Code: challenge.DebugCode})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Withdraw(ctx, accountID, verified.VerificationToken); err != nil {
		t.Fatal(err)
	}

	var accountDeleted, documentDeleted bool
	var cleanupState string
	if err := pool.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM accounts WHERE id=$1", accountID).Scan(&accountDeleted); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM documents WHERE id=$1", uuid.MustParse(document.DocID)).Scan(&documentDeleted); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT state::text FROM artifact_cleanup_jobs WHERE document_id=$1", uuid.MustParse(document.DocID)).Scan(&cleanupState); err != nil {
		t.Fatal(err)
	}
	if !accountDeleted || !documentDeleted || cleanupState != "QUEUED" {
		t.Fatalf("withdraw invariant: account_deleted=%v document_deleted=%v cleanup=%s", accountDeleted, documentDeleted, cleanupState)
	}
	rejoinedID := idgen.New()
	if _, err := queries.CreateAccount(ctx, dbgen.CreateAccountParams{ID: rejoinedID, Email: originalEmail}); err != nil {
		t.Fatalf("withdrawn email must be reusable after immediate anonymization: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM accounts WHERE id=$1", rejoinedID) })
}

func testDatabaseURL() string {
	if value := os.Getenv("TEST_DATABASE_URL"); value != "" {
		return value
	}
	return "postgres://remak:remak@localhost:5432/remak?sslmode=disable"
}

func migrate(t *testing.T, databaseURL string) {
	t.Helper()
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(database, "."); err != nil {
		t.Fatal(err)
	}
}

func createAccount(t *testing.T, ctx context.Context, queries *dbgen.Queries, email string) uuid.UUID {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("integration-password1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	id := idgen.New()
	email = id.String() + "+" + email
	if _, err := queries.CreateAccount(ctx, dbgen.CreateAccountParams{ID: id, Email: email, PasswordHash: pgutil.Text(string(hash))}); err != nil {
		t.Fatal(err)
	}
	return id
}
