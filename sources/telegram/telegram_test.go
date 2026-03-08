package telegram

import (
	"strings"
	"testing"
)

func TestSplitMessageShort(t *testing.T) {
	text := "hello world"
	chunks := SplitMessage(text)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("expected %q, got %q", text, chunks[0])
	}
}

func TestSplitMessageExactLimit(t *testing.T) {
	text := strings.Repeat("a", telegramMaxLength)
	chunks := SplitMessage(text)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for exact limit, got %d", len(chunks))
	}
}

func TestSplitMessageParagraphBoundary(t *testing.T) {
	// Build text: ~3000 chars, paragraph break, ~2000 chars
	part1 := strings.Repeat("a", 3000)
	part2 := strings.Repeat("b", 2000)
	text := part1 + "\n\n" + part2

	chunks := SplitMessage(text)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (paragraph split), got %d", len(chunks))
	}
	if chunks[0] != part1 {
		t.Errorf("first chunk should be part1")
	}
	if chunks[1] != part2 {
		t.Errorf("second chunk should be part2")
	}
}

func TestSplitMessageLineBoundary(t *testing.T) {
	// No paragraph breaks, only line breaks
	part1 := strings.Repeat("a", 3000)
	part2 := strings.Repeat("b", 2000)
	text := part1 + "\n" + part2

	chunks := SplitMessage(text)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (line split), got %d", len(chunks))
	}
}

func TestSplitMessageHardCut(t *testing.T) {
	// No breaks at all
	text := strings.Repeat("x", telegramMaxLength+100)
	chunks := SplitMessage(text)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (hard cut), got %d", len(chunks))
	}
	if len(chunks[0]) != telegramMaxLength {
		t.Errorf("first chunk should be exactly %d chars, got %d", telegramMaxLength, len(chunks[0]))
	}
}

func TestSplitMessageLargeText(t *testing.T) {
	// 15000 chars with paragraph breaks every 2000
	var parts []string
	for i := 0; i < 8; i++ {
		parts = append(parts, strings.Repeat("a", 2000))
	}
	text := strings.Join(parts, "\n\n")

	chunks := SplitMessage(text)
	for _, chunk := range chunks {
		if len(chunk) > telegramMaxLength {
			t.Errorf("chunk exceeds limit: %d > %d", len(chunk), telegramMaxLength)
		}
	}
	// Verify no content lost (approximate — trim may remove some whitespace)
	totalLen := 0
	for _, c := range chunks {
		totalLen += len(c)
	}
	if totalLen < len(text)-len(chunks)*2 { // account for trimmed separators
		t.Errorf("content lost: original %d, chunks total %d", len(text), totalLen)
	}
}

func TestSplitMessageEmpty(t *testing.T) {
	chunks := SplitMessage("")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty, got %d", len(chunks))
	}
}

func TestFileQueueKey(t *testing.T) {
	key := fileQueueKey(12345, 67890)
	if key != "12345:67890" {
		t.Errorf("expected '12345:67890', got '%s'", key)
	}
}

func TestParseChatID(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"12345", 12345, false},
		{"-100123456", -100123456, false},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		id, err := parseChatID(tt.input)
		if tt.wantErr && err == nil {
			t.Errorf("parseChatID(%q): expected error", tt.input)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("parseChatID(%q): unexpected error: %v", tt.input, err)
		}
		if !tt.wantErr && id != tt.expected {
			t.Errorf("parseChatID(%q): expected %d, got %d", tt.input, tt.expected, id)
		}
	}
}
