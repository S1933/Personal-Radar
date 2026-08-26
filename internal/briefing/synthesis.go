package briefing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/S1933/personal-radar/internal/config"
)

// synthesizer calls an OpenAI-compatible chat endpoint to generate a short
// "why it matters" rationale per item. Used as an optional enrichment when
// models.base_url + models.api_key are configured.
type synthesizer struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func newSynthesizer(cfg config.ModelsConfig) *synthesizer {
	return &synthesizer{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Synth.Model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Rationale asks the LLM for a one-line "why this matters" for an item.
func (s *synthesizer) Rationale(ctx context.Context, title, content, source string) (string, error) {
	prompt := fmt.Sprintf(
		"Source: %s\nTitle: %s\nExcerpt: %s\n\nIn one concise sentence (max 140 chars), explain why this matters to a backend/senior engineer interested in AI agents, Go, and developer tooling.",
		source, title, truncate(content, 600))

	body, err := json.Marshal(map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a sharp tech radar editor. Be concise, no fluff, no marketing."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.3,
		"max_tokens":  60,
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
		return "", fmt.Errorf("synthesis: HTTP %d %s", res.StatusCode, string(b))
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
		return "", fmt.Errorf("synthesis: empty choices")
	}
	content := parsed.Choices[0].Message.Content
	// Some reasoning models (e.g. glm-5.3) emit the answer in
	// reasoning_content and leave content empty.
	if content == "" {
		content = parsed.Choices[0].Message.ReasoningContent
	}
	return cleanRationale(content), nil
}

func cleanRationale(s string) string {
	s = stripQuotes(s)
	s = trimNewlines(s)
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func stripQuotes(s string) string {
	s = trimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '	') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '	') {
		end--
	}
	return s[start:end]
}

func trimNewlines(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			if len(out) > 0 && out[len(out)-1] == ' ' {
				continue
			}
			out = append(out, ' ')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
