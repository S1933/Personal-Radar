package x

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/model"
)

// Collector reads recent posts from a fixed set of X accounts. Reading the
// authenticated user's full timeline requires an Elevated/Enterprise tier,
// which is out of scope for the POC; instead we poll specific accounts the
// user wants to watch (config: x.accounts). This mirrors the "targeted
// sources" approach used by the other connectors.
type Collector struct {
	cfg    config.XConfig
	log    Logger
	client *http.Client
}

type Logger interface {
	Warn(msg string, kv ...any)
}

func NewCollector(cfg config.XConfig, log Logger) (*Collector, error) {
	if cfg.BearerToken == "" {
		return nil, fmt.Errorf("x: X_BEARER_TOKEN required")
	}
	return &Collector{
		cfg:    cfg,
		log:    log,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *Collector) Name() string { return "x" }

func (c *Collector) Collect(ctx context.Context) ([]model.Item, error) {
	if len(c.cfg.Accounts) == 0 && len(c.cfg.Queries) == 0 {
		return nil, nil
	}
	var items []model.Item
	var lastErr error

	// Targeted accounts: fetch recent tweets per handle.
	for _, handle := range c.cfg.Accounts {
		posts, err := c.fetchUserTweets(ctx, handle)
		if err != nil {
			c.log.Warn("x account failed", "handle", handle, "error", err)
			lastErr = err
			continue
		}
		items = append(items, posts...)
	}

	// Keyword queries: recent search.
	for _, q := range c.cfg.Queries {
		posts, err := c.searchRecent(ctx, q)
		if err != nil {
			c.log.Warn("x query failed", "query", q, "error", err)
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

// fetchUserTweets resolves the user id then pulls recent tweets.
func (c *Collector) fetchUserTweets(ctx context.Context, handle string) ([]model.Item, error) {
	handle = strings.TrimPrefix(handle, "@")
	id, err := c.resolveUserID(ctx, handle)
	if err != nil {
		return nil, err
	}
	return c.getTweets(ctx, "https://api.twitter.com/2/users/"+id+"/tweets?"+
		url.Values{
			"max_results":      {"10"},
			"tweet.fields":     {"created_at,public_metrics,entities"},
			"expansions":       {"author_id"},
			"user.fields":      {"username,name"},
			"exclude":         {"retweets,replies"},
		}.Encode())
}

func (c *Collector) searchRecent(ctx context.Context, query string) ([]model.Item, error) {
	return c.getTweets(ctx, "https://api.twitter.com/2/tweets/search/recent?"+
		url.Values{
			"query":       {query + " -is:retweet"},
			"max_results": {"10"},
			"tweet.fields": {"created_at,public_metrics,entities"},
			"expansions":  {"author_id"},
			"user.fields": {"username,name"},
		}.Encode())
}

func (c *Collector) resolveUserID(ctx context.Context, handle string) (string, error) {
	u := "https://api.twitter.com/2/users/by/username/" + url.PathEscape(handle) +
		"?user.fields=username,name"
	resp, err := c.do(ctx, u)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Data struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"data"`
		Errors []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp).Decode(&parsed); err != nil {
		return "", fmt.Errorf("x: decode user: %w", err)
	}
	if parsed.Data.ID == "" {
		if len(parsed.Errors) > 0 {
			return "", fmt.Errorf("x: %s: %s", parsed.Errors[0].Title, parsed.Errors[0].Detail)
		}
		return "", fmt.Errorf("x: user %q not found", handle)
	}
	return parsed.Data.ID, nil
}

func (c *Collector) getTweets(ctx context.Context, endpoint string) ([]model.Item, error) {
	resp, err := c.do(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
		Includes struct {
			Users []struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Name     string `json:"name"`
			} `json:"users"`
		} `json:"includes"`
		Errors []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("x: decode tweets: %w", err)
	}
	if len(parsed.Data) == 0 {
		if len(parsed.Errors) > 0 {
			return nil, fmt.Errorf("x: %s: %s", parsed.Errors[0].Title, parsed.Errors[0].Detail)
		}
		return nil, nil
	}
	userByID := map[string]struct {
		Username string
		Name     string
	}{}
	for _, u := range parsed.Includes.Users {
		userByID[u.ID] = struct {
			Username string
			Name     string
		}{u.Username, u.Name}
	}

	now := time.Now().UTC()
	var items []model.Item
	for _, t := range parsed.Data {
		author := handleFrom(parsed.Includes.Users, t.ID)
		it := model.Item{
			Source:      "x",
			SourceID:    "x:" + t.ID,
			Author:      author,
			Title:       firstLine(t.Text),
			URL:         "https://twitter.com/" + author + "/status/" + t.ID,
			Content:     t.Text,
			PublishedAt: now,
			CollectedAt: now,
			Topics:      []string{"software-engineering", "open-source"},
			Language:    "en",
			Metadata: map[string]string{
				"mode": "targeted-account",
			},
		}
		if ta, err := time.Parse(time.RFC3339, t.CreatedAt); err == nil {
			it.PublishedAt = ta
		}
		items = append(items, it)
	}
	return items, nil
}

func (c *Collector) do(ctx context.Context, endpoint string) (io.Reader, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		res.Body.Close()
		return nil, fmt.Errorf("x: HTTP %d %s", res.StatusCode, string(b))
	}
	return res.Body, nil
}

func handleFrom(users []struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}, tweetAuthorID string) string {
	for _, u := range users {
		if u.ID == tweetAuthorID {
			return u.Username
		}
	}
	return "unknown"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}
