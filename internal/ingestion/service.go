package ingestion

import (
	"context"

	"github.com/S1933/personal-radar/internal/model"
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
		id, isNew, err := s.store.InsertItem(ctx, it)
		if err != nil {
			s.log.Warn("insert item", "collector", collector, "source_id", it.SourceID, "error", err)
			continue
		}
		if isNew {
			inserted++
			continue
		}
		// Seen before through a possibly different feed/account: record the
		// additional provenance.
		ref := it.Source + ":" + it.SourceID
		if err := s.store.AddItemSource(ctx, id, it.Source, ref); err != nil {
			s.log.Warn("add item source", "collector", collector, "error", err)
		}
	}
	return inserted, nil
}
