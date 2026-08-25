package store

import (
	"testing"

	"github.com/S1933/personal-radar/internal/model"
)

func TestContentHashStable(t *testing.T) {
	a := model.Item{Title: "  OpenAI Releases Agent SDK  ", Content: "The SDK is available now."}
	b := model.Item{Title: "openai releases agent sdk", Content: "The SDK is available now."}
	if ContentHash(a) != ContentHash(b) {
		t.Errorf("case/whitespace variations must hash equal")
	}
}

func TestContentHashDiffers(t *testing.T) {
	a := model.Item{Title: "OpenAI releases agent SDK", Content: "content one"}
	b := model.Item{Title: "Anthropic releases Claude 5", Content: "content two"}
	if ContentHash(a) == ContentHash(b) {
		t.Errorf("different items must hash differently")
	}
}

func TestContentHashTruncated(t *testing.T) {
	long := ""
	for i := 0; i < 10000; i++ {
		long += "x"
	}
	a := model.Item{Title: "t", Content: long}
	b := model.Item{Title: "t", Content: long + "y"}
	// Beyond 2KB the hash must ignore trailing differences.
	if ContentHash(a) != ContentHash(b) {
		t.Errorf("hash should only consider the first 2KB of content")
	}
}
