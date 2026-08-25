package dedup

import (
	"context"

	"github.com/S1933/personal-radar/internal/model"
)

// Service merges items that describe the same underlying event across
// sources. POC level 1: canonical URL + content hash + normalized source id.
// Embedding similarity (level 2) is a later stage; the interface is ready.
type Service struct {
	store Store
}

type Store interface {
	InsertItem(ctx context.Context, it model.Item) (int64, bool, error)
	AddItemSource(ctx context.Context, itemID int64, source, ref string) error
}

func New(store Store) *Service { return &Service{store: store} }

// Merge attempts to link an incoming item to an existing event and returns
// the canonical item id. POC: handled at insert time by unique constraints +
// item_sources recording.
func (s *Service) Merge(ctx context.Context, it model.Item) (int64, bool, error) {
	return s.store.InsertItem(ctx, it)
}
