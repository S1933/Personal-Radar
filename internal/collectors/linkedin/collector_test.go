package linkedin

import (
	"context"
	"testing"

	"github.com/S1933/personal-radar/internal/config"
)

func TestCollectorReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short")
	}
	c := NewCollector(config.LinkedInConfig{
		Enabled: true,
		Pages: []config.LinkedInPage{
			{Name: "OpenAI", URL: "https://www.linkedin.com/company/openai/"},
		},
	}, discard{})
	items, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	// Best-effort: pages may have no extractable posts. Accept 0 but log.
	t.Logf("openai page -> %d items", len(items))
	for _, it := range items {
		if it.Source != "linkedin" {
			t.Errorf("source = %q", it.Source)
		}
	}
}

type discard struct{}

func (discard) Warn(msg string, kv ...any) {}
