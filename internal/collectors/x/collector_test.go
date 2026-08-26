package x

import (
	"context"
	"os"
	"testing"

	"github.com/S1933/personal-radar/internal/config"
)

func TestCollectorReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short")
	}
	tok := os.Getenv("X_BEARER_TOKEN")
	if tok == "" {
		t.Skip("X_BEARER_TOKEN not set")
	}
	c, err := NewCollector(config.XConfig{
		Enabled:     true,
		Accounts:    []string{"openai"},
		BearerToken: tok,
	}, discard{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	items, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	t.Logf("openai -> %d items", len(items))
	for _, it := range items {
		if it.Source != "x" {
			t.Errorf("source = %q", it.Source)
		}
		if it.URL == "" {
			t.Errorf("empty url for %q", it.Title)
		}
	}
}

type discard struct{}

func (discard) Warn(msg string, kv ...any) {}
