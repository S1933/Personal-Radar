package ingestion

import (
	"context"

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
	store Store
	log   Logger
}

type Store interface {
	InsertItem(ctx context.Context, it model.Item) (int64, bool, error)
	AddItemSource(ctx context.Context, itemID int64, source, ref string) error
	// FindDuplicate returns the existing item id when canonicalURL
	// or content_hash matches a row already in items. Returns
	// (0, false, nil) when the item is new. Used to merge cross-source
	// coverage (the same story arrives via RSS, X and Reddit).
	FindDuplicate(ctx context.Context, canonicalURL, hash string) (int64, bool, error)
}

type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

func New(store Store, log Logger) *Service {
	return &Service{store: store, log: log}
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
			s.log.Info("dedup merge", "collector", collector, "item_id", id,
				"source_id", it.SourceID, "title", it.Title)
			continue
		}

		id, isNew, err := s.store.InsertItem(ctx, it)
		if err != nil {
			s.log.Warn("insert item", "collector", collector, "source_id", it.SourceID, "error", err)
			continue
		}
		if isNew {
			inserted++
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
