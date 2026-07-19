package library

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/events"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
	"github.com/team-everfrost/remak-go/internal/platform/idgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

const maxContentBytes = 5 << 20

type Service struct {
	pool    *pgxpool.Pool
	queries *dbgen.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: dbgen.New(pool)}
}

func (s *Service) CreateMemo(ctx context.Context, ownerID uuid.UUID, input MemoInput) (Document, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" || len(content) > maxContentBytes || !utf8.ValidString(content) {
		return Document{}, httpx.BadRequest("invalid_content", "메모 내용이 비어 있거나 너무 큽니다")
	}
	documentID := idgen.New()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if err := lockActiveAccount(ctx, queries, ownerID); err != nil {
		return Document{}, err
	}
	row, err := queries.CreateMemo(ctx, dbgen.CreateMemoParams{
		ID: documentID, OwnerID: ownerID, Title: pgutil.Text(titleFromContent(content)), Content: pgutil.Text(content),
	})
	if err != nil {
		return Document{}, httpx.Internal(fmt.Errorf("create memo: %w", err))
	}
	if err := s.createVersionAndJob(ctx, queries, row, dbgen.IngestionJobTypeENRICH, ""); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, httpx.Internal(err)
	}
	return documentFromRow(row, nil), nil
}

func (s *Service) CreateWebpage(
	ctx context.Context,
	ownerID uuid.UUID,
	traceID string,
	input WebpageInput,
) (Document, error) {
	cleanURL, err := validatePublicURL(input.URL)
	if err != nil {
		return Document{}, err
	}
	documentID := idgen.New()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if err := lockActiveAccount(ctx, queries, ownerID); err != nil {
		return Document{}, err
	}
	row, err := queries.CreateWebpage(ctx, dbgen.CreateWebpageParams{
		ID:        documentID,
		OwnerID:   ownerID,
		Title:     pgutil.Text(strings.TrimSpace(input.Title)),
		SourceUrl: pgutil.Text(cleanURL),
	})
	if err != nil {
		return Document{}, httpx.Internal(fmt.Errorf("create webpage: %w", err))
	}
	if err := s.createVersionAndJob(ctx, queries, row, dbgen.IngestionJobTypeSCRAPE, traceID); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, httpx.Internal(err)
	}
	return documentFromRow(row, nil), nil
}

func lockActiveAccount(ctx context.Context, queries *dbgen.Queries, ownerID uuid.UUID) error {
	if _, err := queries.LockActiveAccount(ctx, ownerID); errors.Is(err, pgx.ErrNoRows) {
		return httpx.Unauthorized("account_inactive", "계정이 비활성화되었습니다")
	} else if err != nil {
		return httpx.Internal(fmt.Errorf("lock active account: %w", err))
	}
	return nil
}

func (s *Service) UpdateMemo(ctx context.Context, ownerID, documentID uuid.UUID, input MemoInput) (Document, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" || len(content) > maxContentBytes || !utf8.ValidString(content) {
		return Document{}, httpx.BadRequest("invalid_content", "메모 내용이 비어 있거나 너무 큽니다")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	row, err := queries.UpdateMemo(
		ctx,
		dbgen.UpdateMemoParams{
			ID:      documentID,
			OwnerID: ownerID,
			Title:   pgutil.Text(titleFromContent(content)),
			Content: pgutil.Text(content),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, httpx.NotFound("document_not_found", "문서를 찾을 수 없습니다")
	}
	if err != nil {
		return Document{}, httpx.Internal(fmt.Errorf("update memo: %w", err))
	}
	if err := s.createVersionAndJob(ctx, queries, row, dbgen.IngestionJobTypeENRICH, ""); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, httpx.Internal(err)
	}
	return documentFromRow(row, nil), nil
}

func (s *Service) UpdateWebpage(
	ctx context.Context,
	ownerID, documentID uuid.UUID,
	traceID string,
	input WebpageInput,
) (Document, error) {
	cleanURL, err := validatePublicURL(input.URL)
	if err != nil {
		return Document{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	row, err := queries.BeginWebpageRefresh(
		ctx,
		dbgen.BeginWebpageRefreshParams{
			ID:        documentID,
			OwnerID:   ownerID,
			Title:     pgutil.Text(strings.TrimSpace(input.Title)),
			SourceUrl: pgutil.Text(cleanURL),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, httpx.NotFound("document_not_found", "문서를 찾을 수 없습니다")
	}
	if err != nil {
		return Document{}, httpx.Internal(fmt.Errorf("refresh webpage: %w", err))
	}
	if err := s.createVersionAndJob(ctx, queries, row, dbgen.IngestionJobTypeSCRAPE, traceID); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, httpx.Internal(err)
	}
	return documentFromRow(row, nil), nil
}

func (s *Service) createVersionAndJob(
	ctx context.Context,
	queries *dbgen.Queries,
	row dbgen.Document,
	jobType dbgen.IngestionJobType,
	traceID string,
) error {
	if _, err := queries.CreateDocumentVersion(ctx, dbgen.CreateDocumentVersionParams{
		ID: idgen.New(), DocumentID: row.ID, Version: row.CurrentVersion, Title: row.Title, Content: row.Content,
	}); err != nil {
		return httpx.Internal(fmt.Errorf("create document version: %w", err))
	}
	jobID := idgen.New()
	if _, err := queries.CreateIngestionJob(ctx, dbgen.CreateIngestionJobParams{
		ID: jobID, DocumentID: row.ID, DocumentVersion: row.CurrentVersion, Type: jobType,
	}); err != nil {
		return httpx.Internal(fmt.Errorf("create ingestion job: %w", err))
	}
	if jobType != dbgen.IngestionJobTypeSCRAPE {
		return nil
	}
	event := events.New(events.ScrapeRequested, traceID, events.ScrapeRequestData{
		JobID: jobID, DocumentID: row.ID, DocumentVersion: row.CurrentVersion, URL: pgutil.String(row.SourceUrl),
	})
	payload, err := events.Marshal(event)
	if err != nil {
		return httpx.Internal(fmt.Errorf("marshal scrape request: %w", err))
	}
	if err := queries.CreateOutboxEvent(ctx, dbgen.CreateOutboxEventParams{
		ID: event.EventID, EventType: event.EventType, AggregateID: row.ID, Payload: payload,
	}); err != nil {
		return httpx.Internal(fmt.Errorf("create outbox event: %w", err))
	}
	return nil
}

func (s *Service) Get(ctx context.Context, ownerID, documentID uuid.UUID) (Document, error) {
	row, err := s.queries.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: documentID, OwnerID: ownerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, httpx.NotFound("document_not_found", "문서를 찾을 수 없습니다")
	}
	if err != nil {
		return Document{}, httpx.Internal(fmt.Errorf("get document: %w", err))
	}
	result, err := s.withTags(ctx, ownerID, []dbgen.Document{row})
	if err != nil {
		return Document{}, err
	}
	return result[0], nil
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID, cursor Cursor) ([]Document, error) {
	rows, err := s.queries.ListDocuments(
		ctx,
		dbgen.ListDocumentsParams{
			OwnerID:  ownerID,
			Limit:    boundedLimit(cursor.Limit),
			CursorID: nullableUUID(cursor.DocID),
		},
	)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list documents: %w", err))
	}
	return s.withTags(ctx, ownerID, rows)
}

func (s *Service) ListByTag(ctx context.Context, ownerID uuid.UUID, name string, cursor Cursor) ([]Document, error) {
	rows, err := s.queries.ListDocumentsByTag(
		ctx,
		dbgen.ListDocumentsByTagParams{
			OwnerID:  ownerID,
			Name:     name,
			Limit:    boundedLimit(cursor.Limit),
			CursorID: nullableUUID(cursor.DocID),
		},
	)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list documents by tag: %w", err))
	}
	return s.withTags(ctx, ownerID, rows)
}

func (s *Service) ListByCollection(
	ctx context.Context,
	ownerID uuid.UUID,
	name string,
	cursor Cursor,
) ([]Document, error) {
	rows, err := s.queries.ListDocumentsByCollection(
		ctx,
		dbgen.ListDocumentsByCollectionParams{
			OwnerID:  ownerID,
			Name:     name,
			Limit:    boundedLimit(cursor.Limit),
			CursorID: nullableUUID(cursor.DocID),
		},
	)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list documents by collection: %w", err))
	}
	return s.withTags(ctx, ownerID, rows)
}

func (s *Service) SearchText(
	ctx context.Context,
	ownerID uuid.UUID,
	query string,
	limit, offset int32,
) ([]Document, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Document{}, nil
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.queries.SearchDocumentsText(
		ctx,
		dbgen.SearchDocumentsTextParams{
			OwnerID:    ownerID,
			Query:      pgutil.Text(query),
			PageSize:   boundedLimit(limit),
			PageOffset: offset,
		},
	)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("search documents: %w", err))
	}
	return s.withTags(ctx, ownerID, rows)
}

func (s *Service) GetMany(ctx context.Context, ownerID uuid.UUID, documentIDs []uuid.UUID) ([]Document, error) {
	if len(documentIDs) == 0 {
		return []Document{}, nil
	}
	rows, err := s.queries.GetOwnedDocumentsByIDs(
		ctx,
		dbgen.GetOwnedDocumentsByIDsParams{OwnerID: ownerID, DocumentIds: documentIDs},
	)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("get documents by ids: %w", err))
	}
	byID := make(map[uuid.UUID]dbgen.Document, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	ordered := make([]dbgen.Document, 0, len(documentIDs))
	for _, documentID := range documentIDs {
		if row, ok := byID[documentID]; ok {
			ordered = append(ordered, row)
		}
	}
	return s.withTags(ctx, ownerID, ordered)
}

func (s *Service) Delete(ctx context.Context, ownerID, documentID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	affected, err := queries.SoftDeleteDocument(ctx, dbgen.SoftDeleteDocumentParams{ID: documentID, OwnerID: ownerID})
	if err != nil {
		return httpx.Internal(fmt.Errorf("delete document: %w", err))
	}
	if affected != 1 {
		return httpx.NotFound("document_not_found", "문서를 찾을 수 없습니다")
	}
	cleanupParams := dbgen.CreateArtifactCleanupJobParams{
		ID:         idgen.New(),
		DocumentID: documentID,
	}
	if err := queries.CreateArtifactCleanupJob(ctx, cleanupParams); err != nil {
		return httpx.Internal(fmt.Errorf("schedule artifact cleanup: %w", err))
	}
	if err := queries.DeleteDocumentTags(ctx, documentID); err != nil {
		return httpx.Internal(fmt.Errorf("remove document tags: %w", err))
	}
	if err := queries.DeleteDocumentCollections(ctx, documentID); err != nil {
		return httpx.Internal(fmt.Errorf("remove document collections: %w", err))
	}
	if err := queries.DeleteUnusedTags(ctx, ownerID); err != nil {
		return httpx.Internal(fmt.Errorf("remove unused tags: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return httpx.Internal(err)
	}
	return nil
}

func (s *Service) ListTags(ctx context.Context, ownerID uuid.UUID, query string, limit, offset int32) ([]Tag, error) {
	if offset < 0 {
		offset = 0
	}
	rows, err := s.queries.ListTags(
		ctx,
		dbgen.ListTagsParams{
			OwnerID:    ownerID,
			Query:      strings.TrimSpace(query),
			PageSize:   boundedLimit(limit),
			PageOffset: offset,
		},
	)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list tags: %w", err))
	}
	result := make([]Tag, len(rows))
	for index, row := range rows {
		result[index] = Tag{Name: row.Name, Count: row.DocumentCount}
	}
	return result, nil
}

func (s *Service) ListCollections(ctx context.Context, ownerID uuid.UUID, limit, offset int32) ([]Collection, error) {
	if offset < 0 {
		offset = 0
	}
	rows, err := s.queries.ListCollections(
		ctx,
		dbgen.ListCollectionsParams{OwnerID: ownerID, Limit: boundedLimit(limit), Offset: offset},
	)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list collections: %w", err))
	}
	result := make([]Collection, len(rows))
	for index, row := range rows {
		result[index] = Collection{
			Name:        row.Name,
			Description: pgutil.String(row.Description),
			Count:       row.DocumentCount,
		}
	}
	return result, nil
}

func (s *Service) GetCollection(ctx context.Context, ownerID uuid.UUID, name string) (Collection, error) {
	row, err := s.queries.GetCollectionByName(ctx, dbgen.GetCollectionByNameParams{OwnerID: ownerID, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, httpx.NotFound("collection_not_found", "컬렉션을 찾을 수 없습니다")
	}
	if err != nil {
		return Collection{}, httpx.Internal(fmt.Errorf("get collection: %w", err))
	}
	return Collection{Name: row.Name, Description: pgutil.String(row.Description)}, nil
}

func (s *Service) CreateCollection(
	ctx context.Context,
	ownerID uuid.UUID,
	input CreateCollectionInput,
) (Collection, error) {
	name, err := validateCollectionName(input.Name)
	if err != nil {
		return Collection{}, err
	}
	documentIDs, err := parseDocumentIDs(input.DocIDs)
	if err != nil {
		return Collection{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Collection{}, httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if err := lockActiveAccount(ctx, queries, ownerID); err != nil {
		return Collection{}, err
	}
	if err := validateOwnedDocuments(ctx, queries, ownerID, documentIDs); err != nil {
		return Collection{}, err
	}
	row, err := queries.CreateCollection(
		ctx,
		dbgen.CreateCollectionParams{
			ID:          idgen.New(),
			OwnerID:     ownerID,
			Name:        name,
			Description: pgutil.Text(strings.TrimSpace(input.Description)),
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Collection{}, httpx.Conflict("collection_already_exists", "같은 이름의 컬렉션이 이미 있습니다")
		}
		return Collection{}, httpx.Internal(fmt.Errorf("create collection: %w", err))
	}
	for _, documentID := range documentIDs {
		params := dbgen.AddDocumentToCollectionParams{
			CollectionID: row.ID,
			DocumentID:   documentID,
			OwnerID:      ownerID,
		}
		if err := queries.AddDocumentToCollection(ctx, params); err != nil {
			return Collection{}, httpx.Internal(fmt.Errorf("add document to collection: %w", err))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Collection{}, httpx.Internal(err)
	}
	return Collection{Name: row.Name, Description: pgutil.String(row.Description), Count: int64(len(documentIDs))}, nil
}

func (s *Service) AddDocumentsToCollection(ctx context.Context, ownerID uuid.UUID, name string, rawIDs []string) error {
	row, err := s.queries.GetCollectionByName(ctx, dbgen.GetCollectionByNameParams{OwnerID: ownerID, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("collection_not_found", "컬렉션을 찾을 수 없습니다")
	}
	if err != nil {
		return httpx.Internal(fmt.Errorf("get collection: %w", err))
	}
	documentIDs, err := parseDocumentIDs(rawIDs)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	if err := validateOwnedDocuments(ctx, queries, ownerID, documentIDs); err != nil {
		return err
	}
	for _, documentID := range documentIDs {
		params := dbgen.AddDocumentToCollectionParams{
			CollectionID: row.ID,
			DocumentID:   documentID,
			OwnerID:      ownerID,
		}
		if err := queries.AddDocumentToCollection(ctx, params); err != nil {
			return httpx.Internal(fmt.Errorf("add document to collection: %w", err))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return httpx.Internal(err)
	}
	return nil
}

func (s *Service) UpdateCollection(
	ctx context.Context,
	ownerID uuid.UUID,
	currentName string,
	input UpdateCollectionInput,
) (Collection, error) {
	current, err := s.queries.GetCollectionByName(
		ctx,
		dbgen.GetCollectionByNameParams{OwnerID: ownerID, Name: currentName},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Collection{}, httpx.NotFound("collection_not_found", "컬렉션을 찾을 수 없습니다")
	}
	if err != nil {
		return Collection{}, httpx.Internal(fmt.Errorf("get collection: %w", err))
	}
	name := current.Name
	if input.NewName != nil {
		name, err = validateCollectionName(*input.NewName)
		if err != nil {
			return Collection{}, err
		}
	}
	description := pgutil.String(current.Description)
	if input.Description != nil {
		description = strings.TrimSpace(*input.Description)
	}
	added, err := parseDocumentIDs(input.AddedDocIDs)
	if err != nil {
		return Collection{}, err
	}
	removed, err := parseDocumentIDs(input.RemovedDocIDs)
	if err != nil {
		return Collection{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Collection{}, httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	allChanged := append(append([]uuid.UUID{}, added...), removed...)
	if err := validateOwnedDocuments(ctx, queries, ownerID, allChanged); err != nil {
		return Collection{}, err
	}
	updated, err := queries.UpdateCollection(
		ctx,
		dbgen.UpdateCollectionParams{
			OwnerID:     ownerID,
			Name:        currentName,
			Name_2:      name,
			Description: pgutil.Text(description),
		},
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Collection{}, httpx.Conflict("collection_already_exists", "같은 이름의 컬렉션이 이미 있습니다")
		}
		return Collection{}, httpx.Internal(fmt.Errorf("update collection: %w", err))
	}
	for _, documentID := range added {
		params := dbgen.AddDocumentToCollectionParams{
			CollectionID: current.ID,
			DocumentID:   documentID,
			OwnerID:      ownerID,
		}
		if err := queries.AddDocumentToCollection(ctx, params); err != nil {
			return Collection{}, httpx.Internal(err)
		}
	}
	for _, documentID := range removed {
		params := dbgen.RemoveOwnedDocumentFromCollectionParams{
			CollectionID: current.ID,
			DocumentID:   documentID,
			OwnerID:      ownerID,
		}
		if err := queries.RemoveOwnedDocumentFromCollection(ctx, params); err != nil {
			return Collection{}, httpx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Collection{}, httpx.Internal(err)
	}
	return Collection{Name: updated.Name, Description: pgutil.String(updated.Description)}, nil
}

func (s *Service) DeleteCollection(ctx context.Context, ownerID uuid.UUID, name string) error {
	affected, err := s.queries.DeleteCollection(ctx, dbgen.DeleteCollectionParams{OwnerID: ownerID, Name: name})
	if err != nil {
		return httpx.Internal(fmt.Errorf("delete collection: %w", err))
	}
	if affected != 1 {
		return httpx.NotFound("collection_not_found", "컬렉션을 찾을 수 없습니다")
	}
	return nil
}

func (s *Service) withTags(ctx context.Context, ownerID uuid.UUID, rows []dbgen.Document) ([]Document, error) {
	result := make([]Document, len(rows))
	if len(rows) == 0 {
		return result, nil
	}
	ids := make([]uuid.UUID, len(rows))
	byID := make(map[uuid.UUID]int, len(rows))
	for index, row := range rows {
		ids[index] = row.ID
		byID[row.ID] = index
		result[index] = documentFromRow(row, []string{})
	}
	tags, err := s.queries.ListDocumentTagNames(
		ctx,
		dbgen.ListDocumentTagNamesParams{OwnerID: ownerID, DocumentIds: ids},
	)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list document tags: %w", err))
	}
	for _, tag := range tags {
		if index, ok := byID[tag.DocumentID]; ok {
			result[index].Tags = append(result[index].Tags, tag.Name)
		}
	}
	return result, nil
}

func documentFromRow(row dbgen.Document, tags []string) Document {
	if tags == nil {
		tags = []string{}
	}
	return Document{
		DocID:        row.ID.String(),
		Title:        pgutil.String(row.Title),
		Type:         string(row.Type),
		URL:          pgutil.String(row.SourceUrl),
		Content:      pgutil.String(row.Content),
		Summary:      pgutil.String(row.Summary),
		Status:       string(row.Status),
		ThumbnailURL: pgutil.String(row.ThumbnailUrl),
		FileSize:     row.FileSize,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
		Tags:         tags,
	}
}

func DocumentFromRow(row dbgen.Document) Document {
	return documentFromRow(row, []string{})
}

func validatePublicURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		parsed.User != nil {
		return "", httpx.BadRequest("invalid_url", "http 또는 https 웹 주소가 필요합니다")
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func titleFromContent(content string) string {
	first, _, _ := strings.Cut(content, "\n")
	first = strings.TrimSpace(first)
	runes := []rune(first)
	if len(runes) > 120 {
		first = string(runes[:120])
	}
	return first
}

func boundedLimit(limit int32) int32 {
	if limit <= 0 {
		return 20
	}
	if limit > 20 {
		return 20
	}
	return limit
}

func validateCollectionName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", httpx.BadRequest("invalid_collection_name", "컬렉션 이름은 1자 이상 100자 이하여야 합니다")
	}
	return name, nil
}

func parseDocumentIDs(rawIDs []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(rawIDs))
	seen := make(map[uuid.UUID]struct{}, len(rawIDs))
	for _, raw := range rawIDs {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return nil, httpx.BadRequest("invalid_document_id", "문서 ID 형식이 올바르지 않습니다")
		}
		if _, ok := seen[parsed]; ok {
			continue
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result, nil
}

func validateOwnedDocuments(
	ctx context.Context,
	queries *dbgen.Queries,
	ownerID uuid.UUID,
	documentIDs []uuid.UUID,
) error {
	if len(documentIDs) == 0 {
		return nil
	}
	count, err := queries.CountOwnedDocumentsByIDs(
		ctx,
		dbgen.CountOwnedDocumentsByIDsParams{OwnerID: ownerID, DocumentIds: documentIDs},
	)
	if err != nil {
		return httpx.Internal(fmt.Errorf("validate document ownership: %w", err))
	}
	if count != int64(len(documentIDs)) {
		return httpx.NotFound("document_not_found", "문서를 찾을 수 없습니다")
	}
	return nil
}

func nullableUUID(raw string) pgtype.UUID {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: [16]byte(parsed), Valid: true}
}
