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

// Service generates and caches French summaries.
type Service struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client

	// mu guards cache + inflight.
	mu      sync.Mutex
	cache   map[int64]string // item id -> summary (LRU)
	order   []int64          // most-recent at the end
	inflight map[int64]chan struct{} // dedupes concurrent generation
}

// New builds a Service. When cfg has no base URL / api key, the service
// still works but Summarize() always returns an empty string (caller
// should fall back to a content excerpt).
func New(cfg config.ModelsConfig) *Service {
	return &Service{
		baseURL:  cfg.BaseURL,
		apiKey:   cfg.APIKey,
		model:    cfg.Synth.Model,
		client:   &http.Client{Timeout: 30 * time.Second},
		cache:    make(map[int64]string, MaxCacheEntries),
		inflight: make(map[int64]chan struct{}),
	}
}

// Enabled reports whether the service can actually call an LLM. Callers
// skip the LLM path entirely when this is false.
func (s *Service) Enabled() bool { return s.baseURL != "" && s.apiKey != "" }

// Get returns a cached summary if any, or "".
func (s *Service) Get(id int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache[id]
}

// Summarize returns the French summary for the given item, generating
// and caching it if necessary. Persist is called with the produced
// summary so the caller can save it to the DB.
//
// If the service is disabled (no LLM configured) or the generation
// fails, returns "" — the caller is expected to fall back to a content
// excerpt from the DB.
func (s *Service) Summarize(ctx context.Context, id int64, title, content, source, existing string) (string, error) {
	if existing != "" {
		s.put(id, existing)
		return existing, nil
	}
	if s.Get(id) != "" {
		return s.Get(id), nil
	}
	if !s.Enabled() {
		return "", nil
	}

	// Dedupe concurrent calls for the same item.
	s.mu.Lock()
	if ch, ok := s.inflight[id]; ok {
		s.mu.Unlock()
		select {
		case <-ch:
			return s.Get(id), nil
		case <-ctx.Done():
			return "", ctx.Err()
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

	summary, err := s.call(ctx, title, content, source)
	if err != nil {
		// Don't pollute the cache with errors — just log and let the
		// caller fall back. Next request will retry.
		log.Printf("summary: LLM call failed for id=%d: %v", id, err)
		return "", err
	}
	if summary == "" {
		return "", nil
	}
	s.put(id, summary)
	return summary, nil
}

// put stores a summary in the LRU cache, evicting the oldest entry
// when MaxCacheEntries is exceeded.
func (s *Service) put(id int64, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.cache[id]; !exists {
		s.order = append(s.order, id)
	}
	s.cache[id] = summary
	for len(s.order) > MaxCacheEntries {
		old := s.order[0]
		s.order = s.order[1:]
		delete(s.cache, old)
	}
}

// call hits the LLM with the same prompt shape as the briefing
// synthesizer: French, concise, no fluff.
func (s *Service) call(ctx context.Context, title, content, source string) (string, error) {
	prompt := fmt.Sprintf(
		"Source: %s\nTitre: %s\nExtrait: %s\n\nEn français, rédige une description complète (3 à 5 phrases, max 500 caractères) de cet article pour un développeur senior qui parcourt sa veille tech. Couvre l'essentiel : sujet, point clé, enjeu. Factuel, pas de superlatifs, pas de marketing, pas d'émoticônes.",
		source, title, truncate(content, 900))

	body, err := json.Marshal(map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": "Tu es un éditeur de veille tech. Réponds toujours en français, factuellement, sans marketing, sans fluff, sans emoji."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.3,
		"max_tokens":  220,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 256))
		return "", fmt.Errorf("HTTP %d %s", res.StatusCode, string(b))
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
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("empty choices")
	}
	result := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if result == "" {
		result = strings.TrimSpace(parsed.Choices[0].Message.ReasoningContent)
	}
	return clean(result), nil
}

func clean(s string) string {
	s = stripQuotes(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	if len(s) > 520 {
		s = s[:517] + "..."
	}
	return strings.TrimSpace(s)
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
