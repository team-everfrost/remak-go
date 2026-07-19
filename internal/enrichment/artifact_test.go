package enrichment

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/team-everfrost/remak-go/internal/dbgen"
)

type artifactS3Stub struct {
	body        []byte
	contentType string
}

func (s artifactS3Stub) GetObject(
	context.Context,
	*s3.GetObjectInput,
	...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(s.body)), ContentType: aws.String(s.contentType)}, nil
}

type imageProviderStub struct{ *HashProvider }

func (p imageProviderStub) DescribeImage(context.Context, string, string, []byte) (string, error) {
	return "고양이 사진이며 표지판에 REMAK이라고 쓰여 있다.", nil
}

func TestArtifactExtractorImageUsesVisionProvider(t *testing.T) {
	extractor := NewS3ArtifactExtractor(
		artifactS3Stub{body: []byte("image"), contentType: "image/png"},
		"documents",
		imageProviderStub{NewHashProvider(1536)},
	)
	document := dbgen.Document{
		ID:    uuid.New(),
		Type:  dbgen.DocumentTypeIMAGE,
		Title: pgtype.Text{String: "cat.png", Valid: true},
	}
	version := dbgen.DocumentVersion{
		RawArtifactKey: pgtype.Text{String: "cat", Valid: true},
		MediaType:      pgtype.Text{String: "image/png", Valid: true},
	}
	content, method, err := extractor.Extract(context.Background(), document, version)
	if err != nil {
		t.Fatal(err)
	}
	if method != "ai-vision-ocr" || !strings.Contains(content, "REMAK") {
		t.Fatalf("unexpected extraction result: method=%q content=%q", method, content)
	}
}

func TestExtractPDFReadsPageText(t *testing.T) {
	content, err := extractPDF(minimalPDF("Hello Remak PDF"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "Hello Remak PDF") || !strings.Contains(content, "[page 1]") {
		t.Fatalf("unexpected PDF text: %q", content)
	}
}

func TestArtifactExtractorRejectsScannedPDFWithoutText(t *testing.T) {
	extractor := NewS3ArtifactExtractor(
		artifactS3Stub{body: minimalPDF(""), contentType: "application/pdf"},
		"documents",
		NewHashProvider(1536),
	)
	document := dbgen.Document{
		ID:    uuid.New(),
		Type:  dbgen.DocumentTypeFILE,
		Title: pgtype.Text{String: "scan.pdf", Valid: true},
	}
	version := dbgen.DocumentVersion{
		RawArtifactKey: pgtype.Text{String: "scan", Valid: true},
		MediaType:      pgtype.Text{String: "application/pdf", Valid: true},
	}
	_, _, err := extractor.Extract(context.Background(), document, version)
	if err == nil || !strings.Contains(err.Error(), "scanned PDFs require OCR") {
		t.Fatalf("expected explicit scanned PDF error, got %v", err)
	}
}

func TestNormalizeSuggestedTagsBoundsAndDeduplicates(t *testing.T) {
	result := normalizeSuggestedTags([]string{"#AI", " ai ", "Backend", "Go", "PostgreSQL", "RAG", "ignored"})
	want := []string{"AI", "Backend", "Go", "PostgreSQL", "RAG"}
	if fmt.Sprint(result) != fmt.Sprint(want) {
		t.Fatalf("normalize tags: got %v, want %v", result, want)
	}
}

func minimalPDF(text string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf(
			"<< /Length %d >>\nstream\nBT /F1 12 Tf 72 720 Td (%s) Tj ET\nendstream",
			len("BT /F1 12 Tf 72 720 Td () Tj ET\n")+len(text),
			text,
		),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&output, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}
