package briefing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/S1933/personal-radar/internal/logging"
	"github.com/S1933/personal-radar/internal/ranking"
	"github.com/S1933/personal-radar/internal/store"
)

// Options configures briefing generation.
type Options struct {
	MaxItems  int
	MaxTrends int
	Location  *time.Location
}

// sendOption is toggled by SendOption.
type sendKey struct{}

// Service generates the daily briefing markdown.
type Service struct {
	opts     Options
	store    *store.Store
	ranker   *ranking.Service
	synth    *synthesizer
	log      *logging.Logger
	telegram Sender
}

// Sender delivers the briefing (telegram client).
type Sender interface {
	Send(ctx context.Context, text string) error
}

func New(opts Options, st *store.Store, ranker *ranking.Service, log *logging.Logger) *Service {
	s := &Service{opts: opts, store: st, ranker: ranker, log: log}
	// Stage-2 LLM synthesis is active only when an endpoint + key are set.
	if cfg := ranker.ModelsConfig(); cfg.BaseURL != "" && cfg.APIKey != "" {
		s.synth = newSynthesizer(cfg)
	}
	return s
}

func (s *Service) SetTelegram(tg Sender) { s.telegram = tg }

// SendOption returns a context value enabling actual delivery.
func SendOption(send bool) context.Context {
	return context.WithValue(context.Background(), sendKey{}, send)
}

// Generate builds the briefing over the last 24h. When the ctx carries
// SendOption(true), the briefing is also delivered via Telegram.
func (s *Service) Generate(ctx context.Context, opts ...context.Context) (string, error) {
	// Rank anything pending first so the selection is fresh.
	if _, err := s.ranker.RankPending(ctx); err != nil {
		s.log.Warn("rank before briefing", "error", err)
	}

	items, err := s.store.TopScoredItems(ctx, 24*time.Hour, s.opts.MaxItems*3)
	if err != nil {
		return "", fmt.Errorf("top items: %w", err)
	}
	if len(items) == 0 {
		return "", fmt.Errorf("no items in the last 24h")
	}

	selected := items
	if len(selected) > s.opts.MaxItems {
		selected = selected[:s.opts.MaxItems]
	}

	trends := s.detectTrends(items)

	content := s.render(ctx, selected, trends)

	date := time.Now().In(s.loc()).Format("2006-01-02")
	ids := make([]int64, 0, len(selected))
	for _, it := range selected {
		ids = append(ids, it.DBID)
	}
	if err := s.store.SaveBriefing(ctx, date, content, ids); err != nil {
		s.log.Warn("save briefing", "error", err)
	}

	// Auto-bookmark every item that made it into the briefing. The web
	// dashboard surfaces this list (with read/unread/delete). Dedupe-by-title
	// in render() may drop some ids; mark only the ones that actually
	// reached the message.
	if err := s.store.MarkBookmarked(ctx, ids); err != nil {
		s.log.Warn("auto-bookmark", "error", err)
	}

	// Delivery when requested via SendOption.
	if len(opts) > 0 {
		if send, _ := opts[0].Value(sendKey{}).(bool); send && s.telegram != nil {
			if err := s.telegram.Send(ctx, content); err != nil {
				s.log.Error("send briefing", "error", err)
			} else {
				s.log.Info("briefing sent", "items", len(selected))
			}
		}
	}
	return content, nil
}

// detectTrends clusters items sharing >= 3 significant words in the title.
func (s *Service) detectTrends(items []store.ScoredItem) []string {
	type cluster struct {
		key    string
		titles []string
		count  int
	}
	var clusters []cluster
	for _, it := range items {
		kws := keywords(it.Title, 6)
		if len(kws) < 2 {
			continue
		}
		key := strings.Join(kws, " ")
		found := false
		for i := range clusters {
			if overlap(kws, strings.Split(clusters[i].key, " ")) >= 3 {
				clusters[i].count++
				clusters[i].titles = append(clusters[i].titles, it.Title)
				found = true
				break
			}
		}
		if !found {
			clusters = append(clusters, cluster{key: key, titles: []string{it.Title}, count: 1})
		}
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].count > clusters[j].count })
	var out []string
	for _, c := range clusters {
		if c.count >= 3 && len(out) < s.opts.MaxTrends {
			out = append(out, fmt.Sprintf("• %s (%d sources)", c.titles[0], c.count))
		}
	}
	return out
}

func keywords(title string, n int) []string {
	stop := map[string]bool{"the": true, "a": true, "an": true, "for": true, "and": true, "with": true, "new": true, "how": true, "to": true, "of": true, "in": true, "is": true, "it": true}
	words := strings.Fields(strings.ToLower(title))
	var out []string
	seen := map[string]bool{}
	for _, w := range words {
		w = strings.Trim(w, ".,!?:;\"'()[]")
		if len(w) < 3 || stop[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == n {
			break
		}
	}
	return out
}

func overlap(a, b []string) int {
	m := map[string]bool{}
	for _, w := range a {
		m[w] = true
	}
	var n int
	for _, w := range b {
		if m[w] {
			n++
		}
	}
	return n
}

func (s *Service) loc() *time.Location {
	if s.opts.Location != nil {
		return s.opts.Location
	}
	return time.UTC
}

func (s *Service) render(ctx context.Context, items []store.ScoredItem, trends []string) string {
	var b strings.Builder
	now := time.Now().In(s.loc())
	fmt.Fprintf(&b, "☀️ *DAILY RADAR*\n%s\n\n", now.Format("Monday 2 January 2006"))

	if len(items) == 0 {
		b.WriteString("Rien de marquant aujourd'hui — calme plat.\n")
		return b.String()
	}

	// De-duplicate by normalized title so the same story (e.g. a tweet
	// surfaced twice by X) does not appear twice in the briefing.
	seen := make(map[string]bool)
	var deduped []store.ScoredItem
	for _, it := range items {
		key := normTitle(it.Title)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, it)
	}

	fmt.Fprintf(&b, "🔥 *À NE PAS MANQUER*\n\n")
	for i, it := range deduped {
		icon := sourceIcon(it.Source)
		fmt.Fprintf(&b, "%d. %s [*%s*](%s)\n", i+1, icon, escape(it.Title), it.URL)
		// Optional LLM "why it matters" line (best-effort).
		if s.synth != nil {
			if why, err := s.synth.Rationale(ctx, it.Title, it.Content, it.Source); err == nil && why != "" {
				fmt.Fprintf(&b, "   💡 %s\n", why)
			}
		}
	}

	if len(trends) > 0 {
		b.WriteString("\n🧠 *TENDANCES*\n\n")
		for _, t := range trends {
			b.WriteString(t + "\n")
		}
	}

	b.WriteString("\n💡 _Réponds 👍/👎/🔥/📌 ou /save N pour affiner le radar._")
	return b.String()
}

// sourceIcon maps a collector source name to a recognizable emoji.
func sourceIcon(src string) string {
	switch src {
	case "github":
		return "🐙"
	case "x":
		return "🐦"
	case "reddit", "reddit-public":
		return "🔴"
	case "rss":
		return "📰"
	default:
		return "•"
	}
}

// normTitle lowercases and strips punctuation/space so near-identical
// titles collapse to the same dedup key.
func normTitle(t string) string {
	t = strings.ToLower(t)
	var b strings.Builder
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "[", "(")
	s = strings.ReplaceAll(s, "]", ")")
	return s
}
