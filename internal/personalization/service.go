package personalization

import (
	"context"

	"github.com/S1933/personal-radar/internal/store"
)

// Service adjusts preference weights from user feedback.
// POC: simple additive weights on topics/sources/authors, clamped in store.
type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service { return &Service{store: st} }

// FeedbackKind maps a Telegram action to preference deltas.
// Positive: more_like_this, save, thumbs_up, fire.
// Negative: less_like_this, ignore, thumbs_down.
func Apply(ctx context.Context, s *Service, itemID int64, action string) error {
	var delta float64
	switch action {
	case "more_like_this", "save", "thumbs_up", "fire", "pin":
		delta = +0.25
	case "less_like_this", "ignore", "thumbs_down":
		delta = -0.25
	default:
		return nil
	}
	if err := s.store.AddFeedback(ctx, itemID, action); err != nil {
		return err
	}

	it, err := s.store.ItemByID(ctx, itemID)
	if err != nil {
		return nil // item may be gone; feedback is still recorded
	}

	for _, t := range it.Topics {
		if err := s.store.AdjustPreference(ctx, "topic", t, delta); err != nil {
			return err
		}
	}
	if it.Source != "" {
		if err := s.store.AdjustPreference(ctx, "source", it.Source, delta); err != nil {
			return err
		}
	}
	if it.Author != "" {
		if err := s.store.AdjustPreference(ctx, "author", it.Author, delta); err != nil {
			return err
		}
	}
	return nil
}
