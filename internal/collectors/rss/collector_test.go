package rss

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/model"
)

type stubFetcher struct {
	calls    int
	etag     string
	lastMod  string
	response string
}

func (s *stubFetcher) Get(ctx context.Context, url, etag, lastModified string) (*FetchResult, error) {
	s.calls++
	s.etag = etag
	s.lastMod = lastModified
	if etag == `"v1"` {
		return &FetchResult{NotModified: true}, nil
	}
	return &FetchResult{
		Body:         io.NopCloser(strings.NewReader(s.response)),
		ETag:         `"v1"`,
		LastModified: "Mon, 25 Aug 2026 06:00:00 GMT",
	}, nil
}

type stubState struct {
	etag, lastMod string
	saved         bool
}

func (s *stubState) GetFeedState(ctx context.Context, name string) (string, string, error) {
	return s.etag, s.lastMod, nil
}

func (s *stubState) SaveFeedState(ctx context.Context, name, etag, lastModified string) error {
	s.etag, s.lastMod, s.lastMod = etag, lastModified, lastModified
	s.saved = true
	return nil
}

const atomXML = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Test Feed</title>
  <id>urn:uuid:test</id>
  <updated>2026-08-25T06:00:00Z</updated>
  <entry>
    <id>urn:uuid:entry-1</id>
    <title>OpenAI releases new agent SDK</title>
    <link href="https://example.com/blog/agent-sdk?utm_source=rss"/>
    <published>2026-08-25T05:00:00Z</published>
    <summary>Summary of the release.</summary>
    <author><name>OpenAI</name></author>
  </entry>
  <entry>
    <id>urn:uuid:entry-2</id>
    <title>Second post</title>
    <link href="https://example.com/blog/second"/>
    <published>2026-08-24T05:00:00Z</published>
  </entry>
</feed>`

const rssXML = `<?xml version="1.0"?>
<rss version="2.0"><channel>
  <title>RSS2 Feed</title>
  <item>
    <guid isPermaLink="false">post-42</guid>
    <title>Go 1.27 released</title>
    <link>https://go.dev/blog/go1.27</link>
    <pubDate>Mon, 25 Aug 2026 05:00:00 +0000</pubDate>
    <description>Go 1.27 ships with improvements.</description>
  </item>
</channel></rss>`

func TestCollectAtom(t *testing.T) {
	f := &stubFetcher{response: atomXML}
	c := NewCollector(config.RSSConfig{Feeds: []config.RSSFeed{
		{Name: "test", URL: "https://example.com/feed.xml", Topics: []string{"ai"}},
	}}, discard{})
	c.SetFetcher(f)

	items, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	first := items[0]
	if first.Source != "rss" {
		t.Errorf("source = %q, want rss", first.Source)
	}
	if first.SourceID != "urn:uuid:entry-1" {
		t.Errorf("source_id = %q", first.SourceID)
	}
	if first.Author != "OpenAI" {
		t.Errorf("author = %q", first.Author)
	}
	// Canonical URL must strip tracking params.
	if first.CanonicalURL != "https://example.com/blog/agent-sdk" {
		t.Errorf("canonical_url = %q", first.CanonicalURL)
	}
	if first.PublishedAt.IsZero() {
		t.Errorf("published_at not parsed")
	}
	if len(first.Topics) != 1 || first.Topics[0] != "ai" {
		t.Errorf("topics = %v", first.Topics)
	}
}

func TestCollectRSS2(t *testing.T) {
	f := &stubFetcher{response: rssXML}
	c := NewCollector(config.RSSConfig{Feeds: []config.RSSFeed{
		{Name: "go", URL: "https://go.dev/feed.xml"},
	}}, discard{})
	c.SetFetcher(f)

	items, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].SourceID != "post-42" {
		t.Errorf("source_id = %q, want post-42", items[0].SourceID)
	}
	if !strings.Contains(items[0].Content, "Go 1.27") {
		t.Errorf("content not parsed: %q", items[0].Content)
	}
}

func TestConditionalGet(t *testing.T) {
	f := &stubFetcher{response: atomXML}
	state := &stubState{etag: `"v1"`}
	c := NewCollector(config.RSSConfig{Feeds: []config.RSSFeed{
		{Name: "test", URL: "https://example.com/feed.xml"},
	}}, discard{})
	c.SetFetcher(f)
	c.SetStateStore(state)

	items, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("304 should yield 0 items, got %d", len(items))
	}
	if f.etag != `"v1"` {
		t.Errorf("If-None-Match not sent: %q", f.etag)
	}
}

func TestFeedIsolation(t *testing.T) {
	// First feed returns a parse error, second works: Collect must still
	// return the second feed's items.
	broken := &stubFetcher{response: "not-xml"}
	ok := &stubFetcher{response: atomXML}
	c := NewCollector(config.RSSConfig{Feeds: []config.RSSFeed{
		{Name: "broken", URL: "https://broken.example/feed"},
		{Name: "ok", URL: "https://ok.example/feed"},
	}}, discard{})
	multi := &multiFetcher{feeds: map[string]Fetcher{"broken": broken, "ok": ok}}
	c.SetFetcher(multi)

	items, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect should not fail when one feed fails: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items from healthy feed, got %d", len(items))
	}
}

type multiFetcher struct {
	feeds map[string]Fetcher
}

func (m *multiFetcher) Get(ctx context.Context, url, etag, lastModified string) (*FetchResult, error) {
	for name, f := range m.feeds {
		if strings.Contains(url, name) {
			return f.Get(ctx, url, etag, lastModified)
		}
	}
	return f404()
}

func f404() (*FetchResult, error) {
	return nil, errNotFound
}

var errNotFound = &simpleError{}

type simpleError struct{}

func (*simpleError) Error() string { return "feed not found" }

type discard struct{}

func (discard) Warn(msg string, kv ...any) {}

var _ model.Item // model import kept for interface docs
