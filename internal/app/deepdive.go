package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/model"
)

// escapeHTML mirrors internal/briefing.escapeHTML; duplicated here to
// avoid a cross-package import for one call. Until the briefing and
// app packages can share a helper, this stays a deliberate duplicate.
func escapeHTML(s string) string {
	return html.EscapeString(s)
}

// DeepDiveItem fetches an item by DB id and returns an LLM analysis.
// Returns a user-facing error message on any failure (non-fatal).
func (a *App) DeepDiveItem(ctx context.Context, id int64) (string, error) {
	if a.DeepDive == nil || a.Cfg.Models.BaseURL == "" || a.Cfg.Models.APIKey == "" {
		return "🔬 Deep dive non configuré (modèles LLM absents).", nil
	}
	it, err := a.Store.ItemByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("item %d introuvable: %w", id, err)
	}
	analysis, err := a.DeepDive.Analyze(ctx, it)
	if err != nil {
		return "", fmt.Errorf("deepdive échoué: %w", err)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔬 <b>DEEP DIVE</b> — %s\n\n", escapeHTML(it.Title)))
	// Escape the LLM output too: the prompt asks for markdown,
	// which is mostly safe, but code samples and <, >, & characters
	// in technical prose trip Telegram's HTML parser. Without
	// this escape a single "<10" or "R&D" makes the whole message
	// fail with "can't parse entities", and the user gets nothing.
	b.WriteString(escapeHTML(analysis))
	b.WriteString(fmt.Sprintf("\n\n🔗 %s\n", html.EscapeString(it.URL)))
	return b.String(), nil
}

// DeepDive produces a structured analysis of a single item using the
// OpenAI-compatible endpoint configured under models.base_url. It reuses
// the same key/model plumbing as ranking and synthesis.
type DeepDive struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewDeepDive(cfg config.ModelsConfig) *DeepDive {
	model := cfg.DeepDive.Model
	if model == "" {
		model = cfg.Filter.Model // fallback to any configured chat model
	}
	return &DeepDive{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Analyze returns a markdown-formatted deep dive for the given item.
func (d *DeepDive) Analyze(ctx context.Context, it model.Item) (string, error) {
	prompt := buildDeepDivePrompt(it)

	body, err := json.Marshal(map[string]any{
		"model": d.model,
		"messages": []map[string]string{
			{"role": "system", "content": "Tu es l'assistant de recherche d'un ingénieur backend senior. Réponds toujours en français, de façon précise et technique, sans fluff. Utilise le markdown."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.4,
		"max_tokens":  700,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiKey)
	}

	res, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return "", fmt.Errorf("deepdive: HTTP %d %s", res.StatusCode, string(b))
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
		return "", fmt.Errorf("deepdive: empty choices")
	}
	content := parsed.Choices[0].Message.Content
	if content == "" {
		content = parsed.Choices[0].Message.ReasoningContent
	}
	if content == "" {
		return "", fmt.Errorf("deepdive: empty content")
	}
	return content, nil
}

func buildDeepDivePrompt(it model.Item) string {
	topics := ""
	for _, t := range it.Topics {
		topics += "- " + t + "\n"
	}
	content := it.Content
	if len(content) > 4000 {
		content = content[:4000] + "..."
	}
	return fmt.Sprintf(`Analyse cet item pour un ingénieur backend senior (Go, PHP/Symfony, agents IA, outillage développeur). Réponds obligatoirement en français.

Titre : %s
Source : %s
Auteur : %s
URL : %s
Sujets :
%s

Contenu :
%s

Produis une analyse markdown concise avec ces sections :
## 🔍 Résumé
(2-3 phrases, en quoi ça consiste réellement)
## 🔑 Points clés
(liste à puces, faits techniques)
## 💡 Pourquoi ça compte
(impact concret sur le travail backend / agents IA)
## 🚀 Next steps
(que lire/faire ensuite, en citant l'URL ci-dessus)`,
		it.Title, it.Source, it.Author, it.URL, topics, content)
}
