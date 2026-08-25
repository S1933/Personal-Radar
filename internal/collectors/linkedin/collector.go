package linkedin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/model"
)

// Collector reads company page activity without the official API.
// LinkedIn's r_organization_social permission is reserved for organizations
// the authenticated member administers, which does not match a simple
// "list of pages to watch" use case. This public adapter is best-effort and
// must stay within LinkedIn's acceptable use policy. It mirrors the Reddit
// public-pages pattern used elsewhere in the POC.
type Collector struct {
	cfg    config.LinkedInConfig
	log    Logger
	client *http.Client
}

type Logger interface {
	Warn(msg string, kv ...any)
}

func NewCollector(cfg config.LinkedInConfig, log Logger) *Collector {
	return &Collector{
		cfg:    cfg,
		log:    log,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Collector) Name() string { return "linkedin" }

func (c *Collector) Collect(ctx context.Context) ([]model.Item, error) {
	var items []model.Item
	var lastErr error
	for _, page := range c.cfg.Pages {
		posts, err := c.fetchPage(ctx, page)
		if err != nil {
			c.log.Warn("linkedin page failed", "page", page.Name, "error", err)
			lastErr = err
			continue
		}
		items = append(items, posts...)
	}
	if len(items) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return items, nil
}

// fetchPage scrapes the public company page for recent post titles.
// Best-effort: LinkedIn markup changes frequently; missing posts are not
// an error, just a skipped cycle.
func (c *Collector) fetchPage(ctx context.Context, page config.LinkedInPage) ([]model.Item, error) {
	u := page.URL
	if !strings.HasPrefix(u, "http") {
		u = "https://www.linkedin.com/company/" + strings.TrimPrefix(u, "company/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PersonalRadar/0.1; +https://github.com/S1933/personal-radar)")

	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linkedin %s: HTTP %d", "", res.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20)) // cap 2MB
	if err != nil {
		return nil, err
	}
	titles := extractPostTitles(string(body))
	now := time.Now().UTC()
	var items []model.Item
	for i, t := range titles {
		it := model.Item{
			Source:      "linkedin",
			SourceID:    fmt.Sprintf("public:%s:%d", pageSlug(page.URL), i),
			Author:      page.Name,
			Title:       t,
			URL:         page.URL,
			PublishedAt: now,
			CollectedAt: now,
			Topics:      []string{"software-engineering", "open-source"},
			Language:    "en",
			Metadata: map[string]string{
				"page": page.Name,
				"mode": "public",
			},
		}
		items = append(items, it)
	}
	return items, nil
}

// extractPostTitles pulls post headlines from LinkedIn's embedded JSON.
// LinkedIn ships post data inside <code> JSON blocks; we grab short,
// title-like strings from the "articleTitle"/"text" fields.
func extractPostTitles(html string) []string {
	// Look for JSON blobs containing post content.
	re := regexp.MustCompile(`"text":"(.*?)"`)
	matches := re.FindAllStringSubmatch(html, -1)
	var out []string
	seen := map[string]bool{}
	for _, m := range matches {
		t := strings.TrimSpace(m[1])
		if len(t) < 20 || len(t) > 200 {
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func pageSlug(u string) string {
	u = strings.TrimSuffix(u, "/")
	parts := strings.Split(u, "/")
	return parts[len(parts)-1]
}
