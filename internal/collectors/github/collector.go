package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/model"
)

const apiBase = "https://api.github.com"

// Collector watches repository releases and organization events via the
// REST API with a read-only token (GITHUB_TOKEN).
type Collector struct {
	cfg    config.GitHubConfig
	log    Logger
	client *http.Client
	token  string
}

type Logger interface {
	Warn(msg string, kv ...any)
}

func NewCollector(cfg config.GitHubConfig, log Logger) (*Collector, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN not set")
	}
	return &Collector{
		cfg:    cfg,
		log:    log,
		client: &http.Client{Timeout: 30 * time.Second},
		token:  token,
	}, nil
}

func (c *Collector) Name() string { return "github" }

func (c *Collector) Collect(ctx context.Context) ([]model.Item, error) {
	var items []model.Item
	var lastErr error

	for _, repo := range c.cfg.Repositories {
		rel, err := c.fetchReleases(ctx, repo)
		if err != nil {
			c.log.Warn("github releases failed", "repo", repo, "error", err)
			lastErr = err
			continue
		}
		items = append(items, rel...)
	}
	for _, org := range c.cfg.Organizations {
		ev, err := c.fetchOrgRepos(ctx, org)
		if err != nil {
			c.log.Warn("github org failed", "org", org, "error", err)
			lastErr = err
			continue
		}
		items = append(items, ev...)
	}
	if len(items) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return items, nil
}

type release struct {
	ID          int64     `json:"id"`
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	RepoURL string `json:"-"`
}

func (c *Collector) fetchReleases(ctx context.Context, repo string) ([]model.Item, error) {
	var releases []release
	if err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=10", apiBase, repo), &releases); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var items []model.Item
	for _, r := range releases {
		if r.Prerelease {
			continue
		}
		name := r.Name
		if name == "" {
			name = r.TagName
		}
		it := model.Item{
			Source:       "github",
			SourceID:     fmt.Sprintf("release:%s:%d", repo, r.ID),
			Author:       r.Author.Login,
			Title:        fmt.Sprintf("%s %s released", repo, r.TagName),
			Content:      r.Body,
			URL:          r.HTMLURL,
			CanonicalURL: r.HTMLURL,
			PublishedAt:  r.PublishedAt,
			CollectedAt:  now,
			Topics:       []string{"open-source"},
			Language:     "en",
			Metadata: map[string]string{
				"repo":     repo,
				"tag":      r.TagName,
				"kind":     "release",
				"language": detectLanguage(repo),
			},
		}
		items = append(items, it)
	}
	return items, nil
}

type repo struct {
	ID          int64     `json:"id"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	HTMLURL     string    `json:"html_url"`
	Stars       int       `json:"stargazers_count"`
	PushedAt    time.Time `json:"pushed_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// fetchOrgRepos lists org repos and keeps the recently created ones.
func (c *Collector) fetchOrgRepos(ctx context.Context, org string) ([]model.Item, error) {
	var repos []repo
	if err := c.getJSON(ctx, fmt.Sprintf("%s/orgs/%s/repos?sort=pushed&per_page=10", apiBase, org), &repos); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var items []model.Item
	for _, r := range repos {
		// New repos are information; old pushes are noise.
		if now.Sub(r.CreatedAt) > 14*24*time.Hour {
			continue
		}
		title := fmt.Sprintf("New repo in %s: %s", org, r.FullName)
		if r.Description != "" {
			title = title + " — " + r.Description
		}
		it := model.Item{
			Source:       "github",
			SourceID:     fmt.Sprintf("repo:%s:%d", r.FullName, r.ID),
			Author:       org,
			Title:        title,
			Content:      r.Description,
			URL:          r.HTMLURL,
			CanonicalURL: r.HTMLURL,
			PublishedAt:  r.CreatedAt,
			CollectedAt:  now,
			Topics:       []string{"open-source"},
			Language:     "en",
			Engagement:   model.Engagement{Score: int64(r.Stars)},
			Metadata:     map[string]string{"repo": r.FullName, "kind": "new-repo", "language": detectLanguage(r.FullName)},
		}
		items = append(items, it)
	}
	return items, nil
}

func (c *Collector) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "personal-radar/0.1")
	req.Header.Set("Authorization", "Bearer "+c.token)

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("github: HTTP %d %s", res.StatusCode, string(b))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// detectLanguage is a cheap heuristic for repo topics.
func detectLanguage(repo string) string {
	r := strings.ToLower(repo)
	switch {
	case strings.Contains(r, "go") && !strings.Contains(r, "goa"):
		return "go"
	case strings.Contains(r, "php"), strings.Contains(r, "symfony"), strings.Contains(r, "drupal"):
		return "php"
	case strings.Contains(r, "ts"), strings.Contains(r, "typescript"), strings.Contains(r, "bun"):
		return "typescript"
	default:
		return ""
	}
}
