package ranking

import (
	"context"
	"os"
	"testing"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/store"
)

func TestLLMScorerReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short")
	}
	base := os.Getenv("OPENAI_BASE_URL")
	key := os.Getenv("OPENAI_API_KEY")
	if base == "" || key == "" {
		t.Skip("OPENAI_BASE_URL / OPENAI_API_KEY not set")
	}
	s := newLLMScorer(config.ModelsConfig{
		BaseURL: base,
		APIKey:  key,
		Rank:    config.ModelConfig{Model: "deepseek-v4-flash"},
	})
	sc, err := s.Score(context.Background(), store.ScoredItem{
		Title:   "OpenCode releases autonomous coding agent",
		Source:  "github",
		Content: "A new release adds background task execution and file watching to the coding agent.",
	})
	if err != nil {
		t.Fatalf("llm score: %v", err)
	}
	if sc.Final == 0 {
		t.Fatal("expected non-zero final score")
	}
	t.Logf("importance=%.1f relevance=%.1f novelty=%.1f actionability=%.1f final=%.3f",
		sc.Importance, sc.Relevance, sc.Novelty, sc.Actionability, sc.Final)
}
