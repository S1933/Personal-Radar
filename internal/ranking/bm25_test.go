package ranking

import (
	"testing"
)

func TestBM25Score(t *testing.T) {
	m := newBM25()
	cases := []struct {
		name     string
		text     string
		keywords []string
		min      float64 // inclusive
		max      float64 // exclusive (or vice-versa, see below)
	}{
		{
			name:     "ai item matches ai topic",
			text:     "OpenAI releases a new GPT-4o model with agent capabilities",
			keywords: []string{"ai", "gpt", "agent"},
			min:      0.1, // strong match (post-shift)
			max:      1.0,
		},
		{
			name:     "non-matching text",
			text:     "Kardashian launches crypto pump",
			keywords: []string{"kubernetes", "terraform", "docker"},
			min:      0.0,
			max:      0.01, // zero matches → zero
		},
		{
			name:     "phrase keyword",
			text:     "New coding agent released by OpenAI",
			keywords: []string{"coding agent", "copilot"},
			min:      0.3,
			max:      1.01, // 1 of 2 keyword groups matched, full BM25 sat
		},
		{
			name:     "no keywords",
			text:     "anything",
			keywords: nil,
			min:      0,
			max:      0.01,
		},
		{
			name:     "empty text",
			text:     "",
			keywords: []string{"ai"},
			min:      0,
			max:      0.01,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := m.score(tc.text, tc.keywords)
			if got < tc.min || got >= tc.max {
				t.Errorf("score(%q, %v) = %.3f, want in [%.2f, %.2f)",
					tc.text, tc.keywords, got, tc.min, tc.max)
			}
		})
	}
}

func TestBM25Tokenise(t *testing.T) {
	toks := tokenise("Hello, world! Bonjour—le monde")
	want := []string{"hello", "world", "bonjour", "le", "monde"}
	if len(toks) != len(want) {
		t.Fatalf("got %v, want %v", toks, want)
	}
	for i, w := range want {
		if toks[i] != w {
			t.Errorf("[%d] got %q, want %q", i, toks[i], w)
		}
	}
}
