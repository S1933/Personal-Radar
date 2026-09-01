package model

import "testing"

func TestContentHashStable(t *testing.T) {
	a := Item{Title: "  OpenAI Releases Agent SDK  ", Content: "The SDK is available now."}
	b := Item{Title: "openai releases agent sdk", Content: "The SDK is available now."}
	if ContentHash(a) != ContentHash(b) {
		t.Errorf("case/whitespace variations must hash equal")
	}
}

func TestContentHashDiffers(t *testing.T) {
	a := Item{Title: "OpenAI releases agent SDK", Content: "content one"}
	b := Item{Title: "Anthropic releases Claude 5", Content: "content two"}
	if ContentHash(a) == ContentHash(b) {
		t.Errorf("different items must hash differently")
	}
}

func TestContentHashTruncated(t *testing.T) {
	long := ""
	for i := 0; i < 10000; i++ {
		long += "x"
	}
	a := Item{Title: "t", Content: long}
	b := Item{Title: "t", Content: long + "y"}
	if ContentHash(a) != ContentHash(b) {
		t.Errorf("hash should only consider the first 2KB of content")
	}
}

func TestContentHashEmpty(t *testing.T) {
	// An item with neither title nor content has nothing to
	// identify it by; the empty hash prevents FindDuplicate
	// from merging two distinct empty-content items under the
	// same SHA-256.
	if h := ContentHash(Item{}); h != "" {
		t.Errorf("empty item should hash to empty string, got %q", h)
	}
	if h := ContentHash(Item{Title: "   "}); h != "" {
		t.Errorf("whitespace-only title should hash to empty, got %q", h)
	}
}
