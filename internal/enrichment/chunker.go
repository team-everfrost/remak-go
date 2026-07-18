package enrichment

import "strings"

const (
	defaultChunkRunes   = 1200
	defaultOverlapRunes = 180
)

func ChunkText(content string) []string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(runes)/defaultChunkRunes+1)
	start := 0
	for start < len(runes) {
		end := start + defaultChunkRunes
		if end >= len(runes) {
			end = len(runes)
		} else {
			minimumBreak := start + defaultChunkRunes/2
			for candidate := end; candidate > minimumBreak; candidate-- {
				if runes[candidate-1] == '\n' || runes[candidate-1] == '.' || runes[candidate-1] == '。' {
					end = candidate
					break
				}
			}
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			result = append(result, chunk)
		}
		if end == len(runes) {
			break
		}
		start = end - defaultOverlapRunes
		if start < 0 {
			start = 0
		}
	}
	return result
}
