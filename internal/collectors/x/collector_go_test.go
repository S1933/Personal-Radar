package x

import (
	"context"
	"os"
	"testing"

	"github.com/S1933/personal-radar/internal/config"
)

// TestCollectorGoSidecar exercises the Go->twscrape subprocess path using the
// local venv. Skipped unless X_AUTH_TOKEN + X_CT0 are present.
func TestCollectorGoSidecar(t *testing.T) {
	auth := os.Getenv("X_AUTH_TOKEN")
	ct0 := os.Getenv("X_CT0")
	if auth == "" || ct0 == "" {
		t.Skip("X_AUTH_TOKEN / X_CT0 not set")
	}
	// Resolve repo root so we point at the real xscraper script + venv.
	root := os.Getenv("RADAR_ROOT")
	if root == "" {
		root = "."
	}
	c, err := NewCollector(config.XConfig{
		Enabled:  true,
		Accounts: []string{"openai"},
		APIKey:   auth,
		APISecret: ct0,
	}, testDiscard{},
		WithScriptPath(root+"/xscraper/collect.py"),
		WithVenvPython(root+"/.venv/bin/python3"),
		WithTwscrapeDB(root+"/.venv/x_accounts.db"),
	)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	items, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one tweet")
	}
	for _, it := range items {
		if it.Source != "x" {
			t.Errorf("source = %q", it.Source)
		}
		if it.URL == "" {
			t.Errorf("empty url for %q", it.Title)
		}
	}
	t.Logf("x -> %d items (first: %s)", len(items), items[0].Title)
}

type testDiscard struct{}

func (testDiscard) Warn(msg string, kv ...any) {}
