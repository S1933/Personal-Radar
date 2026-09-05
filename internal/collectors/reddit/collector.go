package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/model"
)

const (
	tokenURL  = "https://www.reddit.com/api/v1/access_token"
	apiBase   = "https://oauth.reddit.com"
	userAgent = "personal-radar/0.1 by u-s1933"
)

// Collector polls subreddit listings via OAuth (read-only, script app).
type Collector struct {
	cfg    config.RedditConfig
	log    Logger
	client *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

type Logger interface {
	Warn(msg string, kv ...any)
}

// NewCollector creates the collector and performs the OAuth client-credentials
// grant. Fails when credentials are missing (collector disabled for the cycle).
func NewCollector(ctx context.Context, cfg config.RedditConfig, log Logger) (*Collector, error) {
	c := &Collector{
		cfg:    cfg,
		log:    log,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Collector) Name() string { return "reddit" }

func (c *Collector) Collect(ctx context.Context) ([]model.Item, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}
	var items []model.Item
	var lastErr error
	for _, sub := range c.cfg.Subreddits {
		listing, err := c.fetchListing(ctx, sub, c.cfg.Listing, c.cfg.Limit)
		if err != nil {
			c.log.Warn("subreddit failed", "sub", sub, "error", err)
			lastErr = err
			continue
		}
		items = append(items, listing...)
	}
	if len(items) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return items, nil
}

type listingResponse struct {
	Data struct {
		Children []struct {
			Data map[string]any `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

func (c *Collector) fetchListing(ctx context.Context, sub, listing string, limit int) ([]model.Item, error) {
	u := fmt.Sprintf("%s/r/%s/%s?limit=%d&raw_json=1", apiBase, sub, listing, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", "Bearer "+c.token)

	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("reddit %s: HTTP %d %s", sub, res.StatusCode, string(b))
	}

	var lr listingResponse
	if err := json.NewDecoder(res.Body).Decode(&lr); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var items []model.Item
	for _, child := range lr.Data.Children {
		d := child.Data
		it := model.Item{
			Source:       "reddit",
			SourceID:     str(d, "id"),
			Author:       str(d, "author"),
			AuthorID:     "u/" + str(d, "author"),
			Title:        str(d, "title"),
			Content:      selfText(d),
			URL:          str(d, "url"),
			CanonicalURL: normalizeURL(str(d, "url")),
			PublishedAt:  time.Unix(int64(num(d, "created_utc")), 0).UTC(),
			CollectedAt:  now,
			Topics:       topicsFor(sub),
			Language:     "en",
			Metadata: map[string]string{
				"subreddit":    str(d, "subreddit"),
				"permalink":    "https://www.reddit.com" + str(d, "permalink"),
				"upvote_ratio": fmt.Sprint(d["upvote_ratio"]),
				"flair":        str(d, "link_flair_text"),
			},
		}
		it.Engagement.Score = int64(num(d, "score"))
		it.Engagement.Comments = int64(num(d, "num_comments"))
		if it.SourceID == "" {
			continue
		}
		items = append(items, it)
	}
	return items, nil
}

// ensureToken performs client-credentials auth when needed.
func (c *Collector) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-time.Minute)) {
		return nil
	}
	id := envOr("REDDIT_CLIENT_ID", "")
	secret := envOr("REDDIT_CLIENT_SECRET", "")
	if id == "" || secret == "" {
		return fmt.Errorf("REDDIT_CLIENT_ID / REDDIT_CLIENT_SECRET not set")
	}
	form := url.Values{"grant_type": {"client_credentials"}, "device_id": {"radar-poc"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(id, secret)

	res, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("reddit token: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("reddit token: HTTP %d %s", res.StatusCode, string(b))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tr); err != nil {
		return err
	}
	if tr.AccessToken == "" {
		return fmt.Errorf("reddit token: empty access_token")
	}
	c.token = tr.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func num(m map[string]any, k string) float64 {
	if v, ok := m[k].(float64); ok {
		return v
	}
	return 0
}

func selfText(d map[string]any) string {
	t := str(d, "selftext")
	if t == "" {
		// Link posts: build a short descriptor from domain + title.
		return ""
	}
	return t
}

// topicsFor maps a subreddit to rough topic tags (POC heuristic).
func topicsFor(sub string) []string {
	s := strings.ToLower(sub)
	switch {
	case strings.Contains(s, "machinelearning") || strings.Contains(s, "aiagents") || strings.Contains(s, "localllama"):
		return []string{"ai"}
	case strings.Contains(s, "golang"):
		return []string{"go", "software-engineering"}
	case strings.Contains(s, "devops"):
		return []string{"devops"}
	case strings.Contains(s, "claude") || strings.Contains(s, "codex"):
		return []string{"ai", "coding-agents"}
	default:
		return []string{"software-engineering"}
	}
}

// normalizeURL strips tracking params and fragments.
func normalizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	u.RawQuery = ""
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}
