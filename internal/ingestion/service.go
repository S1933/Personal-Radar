package ingestion

import (
	"context"
	"strings"

	"github.com/S1933/personal-radar/internal/model"
	"github.com/S1933/personal-radar/internal/topics"
)

// Collector is the contract every source connector must implement.
// Implementations must be safe for concurrent use.
type Collector interface {
	Name() string
	// Collect fetches new items since the last call. It must return a
	// partial result alongside an error when possible (isolation rule).
	Collect(ctx context.Context) ([]model.Item, error)
}

// Service ingests batches of items into the store with dedup.
type Service struct {
	store      Store
	log        Logger
	summarizer Summarizer
	summaryDB  SummaryStore
}

// Summarizer generates the French summary that the dashboard displays on
// each card. Implementations must be safe for concurrent use; ingestion
// invokes Summarize from a single goroutine so concurrency is moot but
// the contract stays narrow. When nil, ingestion skips the LLM step and
// the dashboard renders without a French recap (the raw title is shown).
//
// The `existing` parameter mirrors summary.Service — when the caller has
// already produced a summary (cache hit, prior attempt), it short-circuits
// the LLM. ingestion always passes the zero value since the row is fresh.
type Summarizer interface {
	Enabled() bool
	Summarize(ctx context.Context, id int64, title, content, source string, existing ExistingSummary) (frTitle string, points []string, err error)
}

// ExistingSummary is the minimal shape ingestion needs from a cached
// summary. Kept here to avoid importing the summary package (which would
// create an import cycle: summary → ingestion).
type ExistingSummary struct {
	Title  string
	Points []string
}

// SummaryStore persists the produced summary on the item row.
type SummaryStore interface {
	SetSummaryFR(ctx context.Context, id int64, title, summary string) error
}

type Store interface {
	InsertItem(ctx context.Context, it model.Item) (int64, bool, error)
	AddItemSource(ctx context.Context, itemID int64, source, ref string) error
	// FindDuplicate returns the existing item id when canonicalURL
	// or content_hash matches a row already in items. Returns
	// (0, false, nil) when the item is new. Used to merge cross-source
	// coverage (the same story arrives via RSS, X and Reddit).
	FindDuplicate(ctx context.Context, canonicalURL, hash string) (int64, bool, error)
	// ItemSource returns the originating source column of an
	// item (the row in items.source, not the auxiliary
	// item_sources rows). Used to tell a real cross-source
	// merge from the same collector seeing its own record.
	ItemSource(ctx context.Context, itemID int64) (string, error)
}

type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

// New builds the ingestion service. summarizer and summaryDB may be nil —
// in that case the French summary is generated lazily by the web layer on
// first dashboard view, which preserves the old behaviour for tests and
// short-lived tooling that does not need the dashboard optimisation.
func New(store Store, log Logger, summarizer Summarizer, summaryDB SummaryStore) *Service {
	return &Service{store: store, log: log, summarizer: summarizer, summaryDB: summaryDB}
}

// IngestBatch normalizes, hashes and stores items. Returns the number of
// newly inserted rows (duplicates across collectors are merged, not stored
// twice).
func (s *Service) IngestBatch(ctx context.Context, collector string, items []model.Item) (int, error) {
	var inserted int
	for _, it := range items {
		if it.Source == "" {
			it.Source = collector
		}
		// Pad topics up to 3 so every dashboard card shows 3 tags.
		it.Topics = topics.Enrich(it)

		// Cross-source dedup: the same story may arrive via RSS, X
		// and Reddit under different URLs and source_ids. Before
		// inserting, look for an existing item that matches either
		// the canonical URL or the content hash, and attach the new
		// provenance to it instead of creating a duplicate.
		hash := model.ContentHash(it)
		if id, found, err := s.store.FindDuplicate(ctx, it.CanonicalURL, hash); err != nil {
			s.log.Warn("find duplicate", "collector", collector, "source_id", it.SourceID, "error", err)
			// Fall through to InsertItem — failing to look up a dup
			// is not a reason to lose the item.
		} else if found {
			ref := it.Source + ":" + it.SourceID
			if err := s.store.AddItemSource(ctx, id, it.Source, ref); err != nil {
				s.log.Warn("add item source", "collector", collector, "error", err)
			}
			// The previous version logged every match, which on
			// a 20-minute cycle meant ~30 RSS items per cycle
			// matching their own rows (canonical_url is by
			// construction identical to themselves) — the
			// "dedup merge" log lost its meaning. Only log
			// when the matching row came from a different
			// source, which is the only case the log was
			// supposed to surface.
			if orig, srcErr := s.store.ItemSource(ctx, id); srcErr == nil && orig != it.Source {
				s.log.Info("dedup merge", "item_id", id,
					"from_source", orig, "into_source", it.Source,
					"title", it.Title)
			}
			continue
		}

		id, isNew, err := s.store.InsertItem(ctx, it)
		if err != nil {
			s.log.Warn("insert item", "collector", collector, "source_id", it.SourceID, "error", err)
			continue
		}
		if isNew {
			inserted++
			s.summarizeNew(ctx, id, it)
			continue
		}
		// (source, source_id) already exists — the canonical_url /
		// content_hash did not match because the prior record was
		// stored with a different surface identity. Record the
		// additional provenance so the dedup merge is visible.
		ref := it.Source + ":" + it.SourceID
		if err := s.store.AddItemSource(ctx, id, it.Source, ref); err != nil {
			s.log.Warn("add item source", "collector", collector, "error", err)
		}
	}
	return inserted, nil
}

// summarizeNew generates and persists the French summary for a freshly
// inserted item. The goal is to have the dashboard ready to render the
// recap immediately — no lazy /api/summary/{id} round-trip, no placeholder.
// On LLM failure the row stays as-is; the dashboard falls back to the raw
// title. We do not retry: the next collector pass will see the row but
// the "newly inserted" path is bypassed by the (source, source_id)
// conflict guard. A future maintenance job can re-summarise the orphans.
func (s *Service) summarizeNew(ctx context.Context, id int64, it model.Item) {
	if s.summarizer == nil || s.summaryDB == nil || !s.summarizer.Enabled() {
		return
	}
	frTitle, points, err := s.summarizer.Summarize(ctx, id, it.Title, it.Content, it.Source, ExistingSummary{})
	if err != nil {
		s.log.Warn("summarize", "item_id", id, "error", err)
		return
	}
	if frTitle == "" && len(points) == 0 {
		return
	}
	if err := s.summaryDB.SetSummaryFR(ctx, id, frTitle, strings.Join(points, "\n")); err != nil {
		s.log.Warn("persist summary", "item_id", id, "error", err)
	}
}
