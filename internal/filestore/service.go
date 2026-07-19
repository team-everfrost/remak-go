package filestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/library"
	"github.com/team-everfrost/remak-go/internal/platform/httpx"
	"github.com/team-everfrost/remak-go/internal/platform/idgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

const (
	maxFiles          = 10
	maxFileBytes      = int64(10 << 20)
	freeStorageBytes  = int64(1 << 30)
	plusStorageBytes  = int64(10 << 30)
	adminStorageBytes = int64(1 << 40)
)

type s3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type Service struct {
	pool            *pgxpool.Pool
	queries         *dbgen.Queries
	s3              s3API
	presign         *s3.PresignClient
	bucket          string
	maxRequestBytes int64
}

type uploadedFile struct {
	documentID uuid.UUID
	name       string
	objectKey  string
	mediaType  string
	size       int64
	content    string
	typeName   dbgen.DocumentType
}

func NewService(pool *pgxpool.Pool, client *s3.Client, bucket string, maxRequestBytes int64) *Service {
	return &Service{
		pool:            pool,
		queries:         dbgen.New(pool),
		s3:              client,
		presign:         s3.NewPresignClient(client),
		bucket:          bucket,
		maxRequestBytes: maxRequestBytes,
	}
}

func (s *Service) Upload(w http.ResponseWriter, r *http.Request, ownerID uuid.UUID) ([]library.Document, error) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes+(1<<20))
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		return nil, httpx.BadRequest("invalid_multipart", "파일 업로드 형식이나 크기가 올바르지 않습니다")
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 || len(files) > maxFiles {
		return nil, httpx.BadRequest("invalid_file_count", "파일은 한 번에 1개 이상 10개 이하로 올려 주세요")
	}
	uploaded := make([]uploadedFile, 0, len(files))
	for _, header := range files {
		file, err := s.uploadOne(r.Context(), ownerID, header)
		if err != nil {
			s.compensate(uploaded)
			return nil, err
		}
		uploaded = append(uploaded, file)
	}
	result, err := s.persist(r.Context(), ownerID, uploaded)
	if err != nil {
		s.compensate(uploaded)
		return nil, err
	}
	return result, nil
}

func (s *Service) uploadOne(
	ctx context.Context,
	ownerID uuid.UUID,
	header *multipart.FileHeader,
) (uploadedFile, error) {
	if header.Size <= 0 || header.Size > maxFileBytes {
		return uploadedFile{}, httpx.BadRequest("invalid_file_size", "파일 하나의 크기는 10MB 이하여야 합니다")
	}
	name := safeFilename(header.Filename)
	if name == "" {
		return uploadedFile{}, httpx.BadRequest("invalid_filename", "파일 이름이 올바르지 않습니다")
	}
	file, err := header.Open()
	if err != nil {
		return uploadedFile{}, httpx.Internal(fmt.Errorf("open upload: %w", err))
	}
	defer func() { _ = file.Close() }()
	probe := make([]byte, 512)
	read, err := io.ReadFull(file, probe)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return uploadedFile{}, httpx.BadRequest("invalid_file", "파일을 읽을 수 없습니다")
	}
	mediaType := http.DetectContentType(probe[:read])
	typeName, textFile, ok := classify(mediaType)
	if !ok {
		return uploadedFile{}, httpx.BadRequest("unsupported_file_type", "이미지, PDF, 텍스트 파일만 올릴 수 있습니다")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return uploadedFile{}, httpx.Internal(err)
	}
	content := ""
	if textFile {
		body, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
		if err != nil || !utf8.Valid(body) {
			return uploadedFile{}, httpx.BadRequest("invalid_text_file", "텍스트 파일은 UTF-8이어야 합니다")
		}
		content = string(bytes.TrimSpace(body))
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return uploadedFile{}, httpx.Internal(err)
		}
	}
	documentID := idgen.New()
	objectKey := fmt.Sprintf("accounts/%s/documents/%s/original", ownerID, documentID)
	putInput := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(objectKey),
		Body:          file,
		ContentLength: aws.Int64(header.Size),
		ContentType:   aws.String(mediaType),
	}
	if _, err := s.s3.PutObject(ctx, putInput); err != nil {
		return uploadedFile{}, httpx.Internal(fmt.Errorf("store file: %w", err))
	}
	return uploadedFile{
		documentID: documentID,
		name:       name,
		objectKey:  objectKey,
		mediaType:  mediaType,
		size:       header.Size,
		content:    content,
		typeName:   typeName,
	}, nil
}

func (s *Service) persist(ctx context.Context, ownerID uuid.UUID, files []uploadedFile) ([]library.Document, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)
	account, err := queries.LockAccountForStorage(ctx, ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("account_not_found", "계정을 찾을 수 없습니다")
	}
	if err != nil {
		return nil, httpx.Internal(err)
	}
	usage, err := queries.GetStorageUsage(ctx, ownerID)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	var addition int64
	for _, file := range files {
		addition += file.size
	}
	if usage+addition > storageLimit(account) {
		return nil, httpx.BadRequest("storage_limit_exceeded", "저장 공간이 부족합니다")
	}
	result := make([]library.Document, 0, len(files))
	for _, file := range files {
		status := dbgen.DocumentStatusENRICHPENDING
		row, err := queries.CreateFileDocument(
			ctx,
			dbgen.CreateFileDocumentParams{
				ID:       file.documentID,
				OwnerID:  ownerID,
				Title:    pgutil.Text(file.name),
				Type:     file.typeName,
				Content:  pgutil.Text(file.content),
				Status:   status,
				FileSize: file.size,
			},
		)
		if err != nil {
			return nil, httpx.Internal(fmt.Errorf("create file document: %w", err))
		}
		versionParams := dbgen.CreateDocumentVersionParams{
			ID:         idgen.New(),
			DocumentID: row.ID,
			Version:    1,
			Title:      row.Title,
			Content:    row.Content,
			MediaType:  pgutil.Text(file.mediaType),
		}
		if _, err := queries.CreateDocumentVersion(ctx, versionParams); err != nil {
			return nil, httpx.Internal(err)
		}
		completeParams := dbgen.CompleteDocumentVersionParams{
			DocumentID:       row.ID,
			Version:          1,
			RawArtifactKey:   pgutil.Text(file.objectKey),
			Title:            row.Title,
			Content:          row.Content,
			ExtractionMethod: pgutil.Text("direct-upload"),
		}
		if affected, err := queries.CompleteDocumentVersion(ctx, completeParams); err != nil {
			return nil, httpx.Internal(fmt.Errorf("attach file artifact: %w", err))
		} else if affected != 1 {
			return nil, httpx.Internal(fmt.Errorf("attach file artifact: affected=%d", affected))
		}
		jobParams := dbgen.CreateIngestionJobParams{
			ID:              idgen.New(),
			DocumentID:      row.ID,
			DocumentVersion: 1,
			Type:            dbgen.IngestionJobTypeENRICH,
		}
		if _, err := queries.CreateIngestionJob(ctx, jobParams); err != nil {
			return nil, httpx.Internal(err)
		}
		result = append(result, library.DocumentFromRow(row))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, httpx.Internal(err)
	}
	return result, nil
}

func (s *Service) DownloadURL(ctx context.Context, ownerID, documentID uuid.UUID) (string, error) {
	version, err := s.queries.GetOwnedCurrentDocumentVersion(
		ctx,
		dbgen.GetOwnedCurrentDocumentVersionParams{ID: documentID, OwnerID: ownerID},
	)
	if errors.Is(err, pgx.ErrNoRows) || !version.RawArtifactKey.Valid {
		return "", httpx.NotFound("file_not_found", "파일을 찾을 수 없습니다")
	}
	if err != nil {
		return "", httpx.Internal(err)
	}
	document, err := s.queries.GetOwnedDocument(ctx, dbgen.GetOwnedDocumentParams{ID: documentID, OwnerID: ownerID})
	if err != nil {
		return "", httpx.NotFound("file_not_found", "파일을 찾을 수 없습니다")
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": pgutil.String(document.Title)})
	presigned, err := s.presign.PresignGetObject(
		ctx,
		&s3.GetObjectInput{
			Bucket:                     aws.String(s.bucket),
			Key:                        aws.String(version.RawArtifactKey.String),
			ResponseContentDisposition: aws.String(disposition),
		},
		s3.WithPresignExpires(10*time.Minute),
	)
	if err != nil {
		return "", httpx.Internal(fmt.Errorf("presign file: %w", err))
	}
	return presigned.URL, nil
}

func (s *Service) compensate(files []uploadedFile) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, file := range files {
		_, _ = s.s3.DeleteObject(
			ctx,
			&s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(file.objectKey)},
		)
	}
}

func classify(mediaType string) (dbgen.DocumentType, bool, bool) {
	if strings.HasPrefix(mediaType, "image/") {
		return dbgen.DocumentTypeIMAGE, false, true
	}
	if mediaType == "application/pdf" {
		return dbgen.DocumentTypeFILE, false, true
	}
	if strings.HasPrefix(mediaType, "text/") {
		return dbgen.DocumentTypeFILE, true, true
	}
	return "", false, false
}

func safeFilename(raw string) string {
	name := filepath.Base(strings.TrimSpace(raw))
	runes := make([]rune, 0, len(name))
	for _, current := range name {
		if !unicode.IsControl(current) {
			runes = append(runes, current)
		}
	}
	if len(runes) > 255 {
		runes = runes[:255]
	}
	return strings.TrimSpace(string(runes))
}

func storageLimit(account dbgen.Account) int64 {
	if account.Role == dbgen.AccountRoleADMIN {
		return adminStorageBytes
	}
	if account.Plan == dbgen.AccountPlanPLUS {
		return plusStorageBytes
	}
	return freeStorageBytes
}
