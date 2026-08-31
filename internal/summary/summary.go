// Package summary generates short French summaries of bookmarked items
// on demand for the web dashboard. Results are cached in memory and
// persisted on the item row (summary_fr column) so we only ever call
// the LLM once per item.
//
// The summarizer is a no-op when no LLM endpoint is configured — the
// dashboard falls back to displaying the first sentences of the original
// content. This keeps the dashboard usable when running offline.
package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/S1933/personal-radar/internal/config"
)

// MaxCacheEntries bounds the in-memory LRU. 256 items * ~200 bytes ≈ 50KB,
// well within reason; LRU evicts cold entries to avoid unbounded growth
// during long dashboard sessions.
const MaxCacheEntries = 256

// Summary is a generated French title + bullet-list recap.
type Summary struct {
	Title  string   `json:"title"`
	Points []string `json:"points"`
}

// Service generates and caches French summaries.
type Service struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client

	// mu guards cache + inflight.
	mu      sync.Mutex
	cache   map[int64]Summary // item id -> summary (LRU)
	order   []int64           // most-recent at the end
	inflight map[int64]chan struct{} // dedupes concurrent generation
}

// New builds a Service. When cfg has no base URL / api key, the service
// still works but Summarize() always returns an empty Summary (caller
// should fall back to a content excerpt).
func New(cfg config.ModelsConfig) *Service {
	return &Service{
		baseURL:  cfg.BaseURL,
		apiKey:   cfg.APIKey,
		model:    cfg.Synth.Model,
		client:   &http.Client{Timeout: 30 * time.Second},
		cache:    make(map[int64]Summary, MaxCacheEntries),
		inflight: make(map[int64]chan struct{}),
	}
}

// Enabled reports whether the service can actually call an LLM. Callers
// skip the LLM path entirely when this is false.
func (s *Service) Enabled() bool { return s.baseURL != "" && s.apiKey != "" }

// Get returns a cached summary if any.
func (s *Service) Get(id int64) (Summary, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sg, ok := s.cache[id]
	return sg, ok
}

// Summarize returns the French summary for the given item, generating
// and caching it if necessary. Persist is called with the produced
// summary so the caller can save it to the DB.
//
// If the service is disabled (no LLM configured) or the generation
// fails, returns the zero Summary — the caller is expected to fall back
// to a content excerpt from the DB.
func (s *Service) Summarize(ctx context.Context, id int64, title, content, source string, existing Summary) (Summary, error) {
	if existing.Title != "" || len(existing.Points) > 0 {
		s.put(id, existing)
		return existing, nil
	}
	if sg, ok := s.Get(id); ok && (sg.Title != "" || len(sg.Points) > 0) {
		return sg, nil
	}
	if !s.Enabled() {
		return Summary{}, nil
	}

	// Dedupe concurrent calls for the same item.
	s.mu.Lock()
	if ch, ok := s.inflight[id]; ok {
		s.mu.Unlock()
		select {
		case <-ch:
			sg, _ := s.Get(id)
			return sg, nil
		case <-ctx.Done():
			return Summary{}, ctx.Err()
		}
	}
	ch := make(chan struct{})
	s.inflight[id] = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		close(ch)
		delete(s.inflight, id)
		s.mu.Unlock()
	}()

	sg, err := s.call(ctx, title, content, source)
	if err != nil {
		// Don't pollute the cache with errors — just log and let the
		// caller fall back. Next request will retry.
		log.Printf("summary: LLM call failed for id=%d: %v", id, err)
		return Summary{}, err
	}
	if sg.Title == "" && len(sg.Points) == 0 {
		return Summary{}, nil
	}
	s.put(id, sg)
	return sg, nil
}

// put stores a summary in the LRU cache, evicting the oldest entry
// when MaxCacheEntries is exceeded.
func (s *Service) put(id int64, sg Summary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.cache[id]; !exists {
		s.order = append(s.order, id)
	}
	s.cache[id] = sg
	for len(s.order) > MaxCacheEntries {
		old := s.order[0]
		s.order = s.order[1:]
		delete(s.cache, old)
	}
}

// call hits the LLM with a prompt asking for a one-sentence French
// title and a 5-bullet recap, returned as strict JSON.
func (s *Service) call(ctx context.Context, title, content, source string) (Summary, error) {
	prompt := fmt.Sprintf(
		"Source: %s\nTitre: %s\nExtrait: %s\n\nRéponds UNIQUEMENT en JSON valide, sans markdown, format exact : {\"title\": \"...\", \"points\": [\"...\", \"...\", \"...\", \"...\", \"...\"]}\n\n"+
			"- title : UNE phrase (max 90 caractères) qui résume l'essentiel, lisible comme un titre de card.\n"+
			"- points : exactement 5 puces courtes et factuelles (max 120 caractères chacune) : sujet, point clé, puis 3 détails utiles.\n"+
			"- Tout en français. Interdit : introduction (« Cet article... »), conclusion (« L'enjeu est... »), marketing, emoji.", source, title, truncate(content, 700))

	body, err := json.Marshal(map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": "Tu produis des résumés en français : un titre d'une phrase et 5 puces factuelles, style télégraphique, sans fluff. Sortie JSON strict."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
		"max_tokens":  400,
	})
	if err != nil {
		return Summary{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Summary{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	res, err := s.client.Do(req)
	if err != nil {
		return Summary{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 256))
		return Summary{}, fmt.Errorf("HTTP %d %s", res.StatusCode, string(b))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return Summary{}, err
	}
	if len(parsed.Choices) == 0 {
		return Summary{}, errors.New("empty choices")
	}
	result := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if result == "" {
		result = strings.TrimSpace(parsed.Choices[0].Message.ReasoningContent)
	}
	return parseSummary(result), nil
}

// parseSummary extracts the title + 5 bullets from the LLM output,
// tolerating markdown fences around the JSON.
func parseSummary(s string) Summary {
	s = strings.TrimSpace(s)
	// strip ```json ... ``` fences if present
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	var sg struct {
		Title  string   `json:"title"`
		Points []string `json:"points"`
	}
	if err := json.Unmarshal([]byte(s), &sg); err == nil {
		sg.Title = cleanOneLine(sg.Title, 90)
		pts := make([]string, 0, 5)
		for _, p := range sg.Points {
			p = cleanOneLine(p, 120)
			if p != "" {
				pts = append(pts, p)
			}
		}
		if sg.Title != "" && len(pts) >= 3 {
			return Summary{Title: sg.Title, Points: pts}
		}
	}
	// fallback: try legacy free-text summary (kept for old models)
	txt := cleanOneLine(s, 320)
	if txt == "" {
		return Summary{}
	}
	return Summary{Points: []string{txt}}
}

func cleanOneLine(s string, max int) string {
	s = stripQuotes(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = s[:max-1] + "…"
	}
	return s
}

func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
