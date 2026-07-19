package enrichment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ledongthuc/pdf"

	"github.com/team-everfrost/remak-go/internal/dbgen"
	"github.com/team-everfrost/remak-go/internal/platform/pgutil"
)

const (
	maxArtifactBytes = int64(10 << 20)
	maxContentBytes  = 5 << 20
)

type artifactS3 interface {
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type ImageDescriber interface {
	DescribeImage(ctx context.Context, title, mediaType string, image []byte) (string, error)
}

type ArtifactExtractor interface {
	Extract(context.Context, dbgen.Document, dbgen.DocumentVersion) (content, method string, err error)
}

type S3ArtifactExtractor struct {
	s3       artifactS3
	bucket   string
	provider Provider
}

func NewS3ArtifactExtractor(client artifactS3, bucket string, provider Provider) *S3ArtifactExtractor {
	return &S3ArtifactExtractor{s3: client, bucket: bucket, provider: provider}
}

func (e *S3ArtifactExtractor) Extract(
	ctx context.Context,
	document dbgen.Document,
	version dbgen.DocumentVersion,
) (string, string, error) {
	if content := strings.TrimSpace(pgutil.String(version.Content)); content != "" {
		return content, pgutil.String(version.ExtractionMethod), nil
	}
	if !version.RawArtifactKey.Valid {
		return "", "", errors.New("document has no content or raw artifact")
	}
	response, err := e.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(e.bucket),
		Key:    aws.String(version.RawArtifactKey.String),
	})
	if err != nil {
		return "", "", fmt.Errorf("download raw artifact: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxArtifactBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read raw artifact: %w", err)
	}
	if int64(len(body)) > maxArtifactBytes {
		return "", "", errors.New("raw artifact exceeds extraction limit")
	}
	mediaType := pgutil.String(version.MediaType)
	if mediaType == "" {
		mediaType = aws.ToString(response.ContentType)
	}

	var content, method string
	switch document.Type {
	case dbgen.DocumentTypeIMAGE:
		describer, ok := e.provider.(ImageDescriber)
		if !ok {
			return "", "", errors.New("configured AI provider does not support image understanding")
		}
		content, err = describer.DescribeImage(ctx, pgutil.String(document.Title), mediaType, body)
		method = "ai-vision-ocr"
	case dbgen.DocumentTypeFILE:
		isPDF := mediaType == "application/pdf" ||
			strings.HasSuffix(strings.ToLower(pgutil.String(document.Title)), ".pdf")
		switch {
		case isPDF:
			content, err = extractPDF(body)
			method = "go-pdf-text"
		case strings.HasPrefix(mediaType, "text/") || utf8.Valid(body):
			content = string(body)
			method = "utf8-text"
		default:
			err = fmt.Errorf("unsupported artifact media type %q", mediaType)
		}
	case dbgen.DocumentTypeWEBPAGE, dbgen.DocumentTypeMEMO:
		err = fmt.Errorf("document type %s cannot be extracted from document bucket", document.Type)
	}
	if err != nil {
		return "", "", err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", errors.New("artifact contained no extractable text; scanned PDFs require OCR")
	}
	if len(content) > maxContentBytes {
		return "", "", errors.New("extracted content exceeds 5 MiB limit")
	}
	if !utf8.ValidString(content) {
		return "", "", errors.New("extracted content is not valid UTF-8")
	}
	return content, method, nil
}

func extractPDF(body []byte) (content string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			content = ""
			err = fmt.Errorf("pdf parser panic: %v", recovered)
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	var result strings.Builder
	for pageNumber := 1; pageNumber <= reader.NumPage(); pageNumber++ {
		pageText, pageErr := reader.Page(pageNumber).GetPlainText(nil)
		if pageErr != nil {
			return "", fmt.Errorf("extract pdf page %d: %w", pageNumber, pageErr)
		}
		pageText = strings.TrimSpace(pageText)
		if pageText == "" {
			continue
		}
		if result.Len() > 0 {
			result.WriteString("\n\n")
		}
		fmt.Fprintf(&result, "[page %d]\n%s", pageNumber, pageText)
	}
	return result.String(), nil
}
