// Package topics enriches items with a stable, deterministic topic set.
//
// Collectors assign 1-2 coarse topics per item (subreddit or feed based).
// The dashboard displays up to 3 tags per card, so Enrich pads the topic
// list to 3 entries by keyword-matching the title + content against the
// same vocabulary the ranking scorer uses. No LLM call at ingestion time —
// a padded topic is always something the item's text actually mentions.
package topics

import (
	"sort"
	"strings"

	"github.com/S1933/personal-radar/internal/model"
)

// TargetCount is how many tags each card should show.
const TargetCount = 3

// keyword sets per topic — shared vocabulary with internal/ranking/topicKeywords.
var keywords = map[string][]string{
	"ai":                   {"ai", "llm", "gpt", "model", "inference", "agent", "openai", "anthropic", "claude", "gemini", "deepseek", "mistral", "machine learning", "neural"},
	"coding-agents":        {"coding agent", "copilot", "cursor", "code assistant", "agentic", "swe-bench"},
	"software-engineering": {"engineering", "developer", "api", "typescript", "rust", "compiler", "refactor", "architecture", "programming", "framework"},
	"devops":               {"kubernetes", "docker", "ci/cd", "devops", "terraform", "observability", "deployment", "sre", "slurm", "cluster", "infrastructure"},
	"open-source":          {"open source", "open-source", "github", "release", "license", "mit", "repository", "hugging face"},
	"go":                   {"golang", " go ", "goroutine", "go 1.", "go team", "mcp server", "go mcp"},
	"php":                  {"php", "symfony", "drupal", "laravel", "composer"},
	"typescript":           {"typescript", "tsx", "bun", "deno", "node.js", "vite"},
}

// Enrich fills it.Topics up to TargetCount entries using keyword matches
// against the item text. Existing topics are preserved first; new ones are
// appended in keyword-hit order (deterministic). The input slice is not
// mutated — a new slice is returned.
func Enrich(it model.Item) []string {
	known := knownTopics(keywords)
	text := strings.ToLower(it.Title + "\n" + it.Content)

	out := make([]string, 0, TargetCount)
	seen := make(map[string]bool, TargetCount)
	for _, t := range it.Topics {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= TargetCount {
			return out
		}
	}

	// Pad with the best-matching topics not already present.
	type cand struct {
		topic string
		hits  int
	}
	var cands []cand
	for t, kws := range keywords {
		if seen[t] {
			continue
		}
		hits := 0
		for _, k := range kws {
			if strings.Contains(text, k) {
				hits++
			}
		}
		if hits > 0 {
			cands = append(cands, cand{t, hits})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].hits > cands[j].hits })

	for _, c := range cands {
		if seen[c.topic] || !known[c.topic] {
			continue
		}
		seen[c.topic] = true
		out = append(out, c.topic)
		if len(out) >= TargetCount {
			break
		}
	}
	return out
}

// knownTopics returns the set of topics we are allowed to emit.
func knownTopics(m map[string][]string) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}