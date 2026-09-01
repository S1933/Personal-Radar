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
			min:      0.5, // strong match
			max:      1.0,
		},
		{
			name:     "non-matching text",
			text:     "Kardashian launches crypto pump",
			keywords: []string{"kubernetes", "terraform", "docker"},
			min:      0.0,
			max:      0.01,
		},
		{
			name:     "phrase keyword",
			text:     "New coding agent released by OpenAI",
			keywords: []string{"coding agent", "copilot"},
			min:      0.3,
			// 1 of 2 keyword groups matched: raw=2 (both tokens
			// of "coding agent"), matched=1, coverage=0.5.
			// score = 2 * (0.6+0.2) = 1.6 → clamped to 1.0.
			max: 1.01,
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

// TestBM25RichTopicNotPenalised locks in the new normalisation. The
// previous version divided by len(keywords), so a topic with 12
// lemmas could never score above 0.1 even when every lemma was
// matched. The new score is the average BM25 saturation across
// matched groups, times a coverage bonus. We assert two things:
//   (1) a fully-matched rich topic scores at least as well as a
//       fully-matched small topic (the bug we're fixing);
//   (2) a partially-matched rich topic scores in a sensible
//       range above the no-match floor.
func TestBM25RichTopicNotPenalised(t *testing.T) {
	m := newBM25()
	rich := []string{"ai", "openai", "agent", "gpt", "model", "inference",
		"claude", "anthropic", "gemini", "deepseek", "mistral", "llama"}
	small := []string{"openai"}

	// (1) Full coverage: rich must outscore small.
	full := "OpenAI GPT agent AI model inference claude anthropic gemini deepseek mistral llama"
	if m.score(full, rich) < m.score(full, small) {
		t.Errorf("rich topic fully matched must outscore a small one; got rich=%.3f, small=%.3f",
			m.score(full, rich), m.score(full, small))
	}
	// (2) Partial coverage: rich must still beat the floor.
	partial := "OpenAI ships a new agent"
	if got := m.score(partial, rich); got < 0.5 {
		t.Errorf("rich topic with 3/12 matched should score >= 0.5, got %.3f", got)
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
