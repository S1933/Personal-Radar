package ranking

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/logging"
	"github.com/S1933/personal-radar/internal/store"
)

// Weights of the final score. Configurable in future config revisions.
var Weights = struct {
	Relevance       float64
	Importance      float64
	Novelty         float64
	Actionability   float64
	Personalization float64
}{0.40, 0.25, 0.15, 0.10, 0.10}

// Service scores unscored items. POC uses a deterministic heuristic scorer;
// an LLM stage can be plugged via Scorer.
type Service struct {
	models config.ModelsConfig
	store  *store.Store
	log    *logging.Logger
	scorer Scorer
}

// Scorer produces sub-scores for one item (heuristic or LLM-backed).
type Scorer interface {
	Score(ctx context.Context, it store.ScoredItem) (store.Score, error)
}

func New(models config.ModelsConfig, st *store.Store, log *logging.Logger) *Service {
	s := &Service{models: models, store: st, log: log}
	s.scorer = &heuristicScorer{}
	return s
}

// SetScorer overrides the scoring strategy (tests, future LLM stage).
func (s *Service) SetScorer(sc Scorer) { s.scorer = sc }

// RankPending scores items collected in the last 48h without a score.
func (s *Service) RankPending(ctx context.Context) (int, error) {
	items, err := s.store.UnscoredItems(ctx, 48*time.Hour)
	if err != nil {
		return 0, err
	}
	prefs, _ := s.store.AllPreferences(ctx)

	var n int
	for _, it := range items {
		sc, err := s.scorer.Score(ctx, it)
		if err != nil {
			s.log.Warn("score item", "id", it.DBID, "error", err)
			continue
		}
		sc.Personalization = personalizationScore(it, prefs)
		sc.Final = finalScore(sc)
		sc.Model = "heuristic-v1"
		if err := s.store.SaveScore(ctx, it.DBID, sc); err != nil {
			s.log.Warn("save score", "id", it.DBID, "error", err)
			continue
		}
		n++
	}
	if n > 0 {
		s.log.Info("ranked", "items", n)
	}
	return n, nil
}

func finalScore(sc store.Score) float64 {
	return sc.Relevance*Weights.Relevance +
		sc.Importance*Weights.Importance +
		sc.Novelty*Weights.Novelty +
		sc.Actionability*Weights.Actionability +
		sc.Personalization*Weights.Personalization
}

func personalizationScore(it store.ScoredItem, prefs map[string]map[string]float64) float64 {
	if len(prefs) == 0 {
		return 0
	}
	var sum float64
	var count int
	for _, t := range it.Topics {
		if w, ok := prefs["topic"][t]; ok {
			sum += w
			count++
		}
	}
	if w, ok := prefs["source"][it.Source]; ok {
		sum += w
		count++
	}
	if it.Author != "" {
		if w, ok := prefs["author"][it.Author]; ok {
			sum += w
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return clamp(sum/float64(count), -1, 1)
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// heuristicScorer computes deterministic sub-scores from item fields.
type heuristicScorer struct{}

var avoidRe = regexp.MustCompile(`(?i)\b(celebrity|sport|kardashian|crypto\s*pump|nft\s*drop|horoscope)\b`)

var topicKeywords = map[string][]string{
	"ai":                   {"ai", "llm", "gpt", "model", "inference", "agent", "openai", "anthropic", "claude", "gemini", "deepseek", "mistral"},
	"coding-agents":        {"coding agent", "copilot", "cursor", "code assistant", "agentic", "swe-bench"},
	"software-engineering": {"engineering", "developer", "api", "typescript", "rust", "compiler", "refactor", "architecture"},
	"devops":               {"kubernetes", "docker", "ci/cd", "devops", "terraform", "observability", "deployment"},
	"open-source":          {"open source", "open-source", "github", "release", "license", "mit", "repository"},
	"go":                   {"golang", " go ", "goroutine", "go 1.", "go team"},
	"php":                  {"php", "symfony", "drupal", "laravel", "composer"},
	"typescript":           {"typescript", "tsx", "bun", "deno", "node.js", "vite"},
}

func (h *heuristicScorer) Score(_ context.Context, it store.ScoredItem) (store.Score, error) {
	text := strings.ToLower(it.Title + "\n" + it.Content)

	var sc store.Score

	// Relevance: keyword overlap with known topics + configured topics.
	relevant := 0
	for _, kw := range topicKeywords {
		for _, k := range kw {
			if strings.Contains(text, k) {
				relevant++
				break
			}
		}
	}
	sc.Relevance = clamp(float64(relevant)/4, 0, 1)

	// Avoid-list hard penalty.
	if avoidRe.MatchString(text) {
		sc.Relevance = 0
		sc.Importance = 0
	}

	// Importance: engagement (log scale) + title length sanity.
	sc.Importance = clamp(math.Log1p(float64(it.Engagement))/10, 0, 1)

	// Novelty: release/new keywords boost.
	if strings.Contains(text, "release") || strings.Contains(text, "v1.") || strings.Contains(text, "launch") ||
		strings.Contains(text, "announc") || strings.Contains(text, "new repo") {
		sc.Novelty = 0.8
	} else {
		sc.Novelty = 0.3
	}

	// Actionability: tutorials, how-tos, migrations.
	if strings.Contains(text, "how to") || strings.Contains(text, "tutorial") ||
		strings.Contains(text, "guide") || strings.Contains(text, "migration") ||
		strings.Contains(text, "breaking change") {
		sc.Actionability = 0.7
	} else {
		sc.Actionability = 0.2
	}

	return sc, nil
}

var _ = fmt.Sprintf // keep fmt for future debug logging
