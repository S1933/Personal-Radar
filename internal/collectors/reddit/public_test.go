package reddit

import (
	"context"
	"strings"
	"testing"

	"github.com/S1933/personal-radar/internal/config"
)

func TestSubName(t *testing.T) {
	cases := map[string]string{
		"opencodeCLI":                          "opencodeCLI",
		"r/opencodeCLI":                        "opencodeCLI",
		"https://www.reddit.com/r/opencodeCLI": "opencodeCLI",
		"https://www.reddit.com/r/opencodeCLI/s/gLbU8Id468": "opencodeCLI",
		"LocalLLaMA": "LocalLLaMA",
	}
	for in, want := range cases {
		if got := subName(in); got != want {
			t.Errorf("subName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPublicCollectorReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short")
	}
	c := NewPublicCollector(config.RedditConfig{
		Subreddits: []string{"opencodeCLI"},
		Limit:      10,
	}, discard{})
	items, err := c.Collect(context.Background())
	// Reddit public RSS is rate-limited (HTTP 429) for unauthenticated
	// requests, especially from shared IPs. That is expected best-effort
	// behaviour, not a logic failure.
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 429") {
			t.Skipf("reddit public RSS rate-limited (429), expected best-effort: %v", err)
		}
		t.Fatalf("collect: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one item from r/opencodeCLI")
	}
	for _, it := range items {
		if it.Source != "reddit" {
			t.Errorf("source = %q", it.Source)
		}
		if it.SourceID == "" {
			t.Errorf("empty source_id for %q", it.Title)
		}
		if it.Title == "" {
			t.Errorf("empty title")
		}
	}
}

type discard struct{}

func (discard) Warn(msg string, kv ...any) {}
