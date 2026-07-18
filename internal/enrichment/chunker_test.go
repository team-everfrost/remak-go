package enrichment

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestChunkTextPreservesUnicodeAndBounds(t *testing.T) {
	content := strings.Repeat("한글 문장입니다. ", 400)
	chunks := ChunkText(content)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > defaultChunkRunes {
			t.Fatalf("chunk exceeded rune limit: %d", len([]rune(chunk)))
		}
	}
}

func TestHashProviderIsDeterministic(t *testing.T) {
	provider := NewHashProvider(32)
	first, err := provider.Embed(context.Background(), []string{"alpha beta"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Embed(context.Background(), []string{"alpha beta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first[0]) != 32 || !reflect.DeepEqual(first, second) {
		t.Fatalf("hash embeddings are not deterministic: %#v %#v", first, second)
	}
}
