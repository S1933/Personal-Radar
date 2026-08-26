package briefing

import (
	"context"
	"os"
	"testing"

	"github.com/S1933/personal-radar/internal/config"
)

func TestSynthesizerReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short")
	}
	base := os.Getenv("OPENAI_BASE_URL")
	key := os.Getenv("OPENAI_API_KEY")
	if base == "" || key == "" {
		t.Skip("OPENAI_BASE_URL / OPENAI_API_KEY not set")
	}
	s := newSynthesizer(config.ModelsConfig{
		BaseURL: base,
		APIKey:  key,
		Synth:   config.ModelConfig{Model: "deepseek-v4-flash"},
	})
	why, err := s.Rationale(context.Background(),
		"OpenCode releases autonomous coding agent",
		"A new release adds background task execution and file watching to the coding agent.",
		"github")
	if err != nil {
		t.Fatalf("rationale: %v", err)
	}
	if why == "" {
		t.Fatal("empty rationale")
	}
	t.Logf("why: %s", why)
}
