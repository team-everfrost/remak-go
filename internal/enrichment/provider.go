package enrichment

import "context"

type Provider interface {
	Name() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Analyze(ctx context.Context, title, content string) (Analysis, error)
	Answer(ctx context.Context, question, context string) (string, error)
}

type Analysis struct {
	Summary string
	Tags    []string
}
