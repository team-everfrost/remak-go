package enrichment

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
	"unicode"
)

// HashProvider is deterministic and dependency-free. It is intended for local
// development and tests; production should configure HTTPProvider.
type HashProvider struct {
	dimensions int
}

func NewHashProvider(dimensions int) *HashProvider { return &HashProvider{dimensions: dimensions} }

func (p *HashProvider) Name() string { return "local-hash-v1" }

func (p *HashProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for index, text := range texts {
		vector := make([]float32, p.dimensions)
		tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		for _, token := range tokens {
			digest := sha256.Sum256([]byte(token))
			position := binary.BigEndian.Uint32(digest[:4]) % uint32(p.dimensions)
			sign := float32(1)
			if digest[4]&1 == 1 {
				sign = -1
			}
			vector[position] += sign
		}
		var norm float64
		for _, value := range vector {
			norm += float64(value * value)
		}
		if norm > 0 {
			scale := float32(1 / math.Sqrt(norm))
			for position := range vector {
				vector[position] *= scale
			}
		}
		result[index] = vector
	}
	return result, nil
}

func (p *HashProvider) Analyze(_ context.Context, title, content string) (Analysis, error) {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) > 500 {
		runes = runes[:500]
	}
	return Analysis{Summary: strings.TrimSpace(string(runes)), Tags: []string{}}, nil
}

func (p *HashProvider) Answer(_ context.Context, question, sourceContext string) (string, error) {
	runes := []rune(strings.TrimSpace(sourceContext))
	if len(runes) > 800 {
		runes = runes[:800]
	}
	if len(runes) == 0 {
		return "관련 문서를 찾지 못했습니다.", nil
	}
	return "로컬 테스트 응답입니다. 질문: " + strings.TrimSpace(question) + "\n\n찾은 문서 근거:\n" + string(runes), nil
}
