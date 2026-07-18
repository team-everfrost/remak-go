package chat

import "testing"

func TestSplitRunesDoesNotBreakUTF8(t *testing.T) {
	parts := splitRunes("가나다라마바사", 3)
	if len(parts) != 3 || parts[0] != "가나다" || parts[2] != "사" {
		t.Fatalf("unexpected parts: %#v", parts)
	}
}
