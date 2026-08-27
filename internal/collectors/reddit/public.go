package reddit

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/model"
)

// PublicCollector reads subreddit feeds via the native Reddit RSS endpoint
// (https://www.reddit.com/r/<sub>/.rss). No credentials required.
//
// This is the "public-page" adapter: best-effort, rate-limited by Reddit,
// and must stay within Reddit's acceptable use policy. It is used when no
// OAuth app is configured, mirroring the LinkedIn public-pages pattern.
type PublicCollector struct {
	cfg    config.RedditConfig
	log    Logger
	client *http.Client
	parser *gofeed.Parser
}

func NewPublicCollector(cfg config.RedditConfig, log Logger) *PublicCollector {
	return &PublicCollector{
		cfg:    cfg,
		log:    log,
		client: &http.Client{Timeout: 30 * time.Second},
		parser: gofeed.NewParser(),
	}
}

func (c *PublicCollector) Name() string { return "reddit-public" }

// redditThrottle is the minimum spacing between subreddit requests so we stay
// under Reddit's anonymous-RSS rate limit (the 429s came from firing 17
// requests in a tight loop). 2.5s/sub * 17 subs ≈ 42s per cycle — well within
// a 60m poll and gentle on Reddit's edge.
const redditThrottle = 2500 * time.Millisecond

func (c *PublicCollector) Collect(ctx context.Context) ([]model.Item, error) {
	var items []model.Item
	var lastErr error
	for i, sub := range c.cfg.Subreddits {
		if i > 0 {
			select {
			case <-ctx.Done():
				return items, ctx.Err()
			case <-time.After(redditThrottle):
			}
		}
		feed, err := c.fetchSub(ctx, sub)
		if err != nil {
			c.log.Warn("reddit public feed failed", "sub", sub, "error", err)
			lastErr = err
			continue
		}
		items = append(items, feed...)
	}
	if len(items) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return items, nil
}

func (c *PublicCollector) fetchSub(ctx context.Context, sub string) ([]model.Item, error) {
	// Accept both a bare sub name and a share/permalink URL.
	name := subName(sub)
	if name == "" {
		return nil, fmt.Errorf("cannot parse subreddit from %q", sub)
	}
	u := fmt.Sprintf("https://www.reddit.com/r/%s/.rss?limit=%d", name, c.cfg.Limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PersonalRadar/0.1 (by /u/jenue1933; contact: jeanphilippenuel@gmail.com; +https://github.com/S1933/personal-radar)")
	// Reddit rate-limits anonymous RSS aggressively. Back off and retry a
	// few times so a transient 429 does not drop the whole subreddit.
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		res, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode == http.StatusTooManyRequests {
			res.Body.Close()
			lastErr = fmt.Errorf("reddit rss %s: HTTP 429", name)
			continue
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, fmt.Errorf("reddit rss %s: HTTP %d", name, res.StatusCode)
		}
		parsed, err := c.parser.Parse(res.Body)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		// build items...
		now := time.Now().UTC()
		var items []model.Item
		for _, entry := range parsed.Items {
			link := entry.Link
			it := model.Item{
				Source:       "reddit",
				SourceID:     redditGUID(entry, name),
				Author:       authorName(entry),
				Title:        strings.TrimSpace(entry.Title),
				Content:      entry.Content,
				URL:          link,
				CanonicalURL: normalizeURL(link),
				PublishedAt:  entryPublished(entry, now),
				CollectedAt:  now,
				Topics:       topicsFor(name),
				Language:     "en",
				Metadata: map[string]string{
					"subreddit": name,
					"mode":      "public",
					"permalink": link,
				},
			}
			if it.Title == "" {
				continue
			}
			items = append(items, it)
		}
		return items, nil
	}
	return nil, lastErr
}

// subName extracts the subreddit name from either "golang", "r/golang", or a
// full URL like https://www.reddit.com/r/opencodeCLI or
// https://www.reddit.com/r/opencodeCLI/s/gLbU8Id468.
func subName(in string) string {
	in = strings.TrimSpace(in)
	in = strings.TrimPrefix(in, "https://")
	in = strings.TrimPrefix(in, "http://")
	in = strings.TrimPrefix(in, "www.reddit.com/")
	in = strings.TrimPrefix(in, "old.reddit.com/")
	in = strings.TrimPrefix(in, "reddit.com/")
	in = strings.TrimPrefix(in, "r/")
	// Take the first path segment; drop anything after a slash.
	if i := strings.Index(in, "/"); i >= 0 {
		in = in[:i]
	}
	return in
}

func redditGUID(entry *gofeed.Item, sub string) string {
	if entry.GUID != "" {
		return "public:" + entry.GUID
	}
	if entry.Link != "" {
		return "public:" + entry.Link
	}
	return "public:" + sub + ":" + entry.Title
}

func authorName(entry *gofeed.Item) string {
	if entry.Author != nil && entry.Author.Name != "" {
		return entry.Author.Name
	}
	if len(entry.Authors) > 0 && entry.Authors[0].Name != "" {
		return entry.Authors[0].Name
	}
	return ""
}

func entryPublished(entry *gofeed.Item, fallback time.Time) time.Time {
	if entry.PublishedParsed != nil {
		return *entry.PublishedParsed
	}
	if entry.UpdatedParsed != nil {
		return *entry.UpdatedParsed
	}
	return fallback
}
