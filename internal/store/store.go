package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/S1933/personal-radar/internal/db"
	"github.com/S1933/personal-radar/internal/model"
)

// Store is the persistence layer. All SQL lives here.
type Store struct {
	db *db.DB
}

func New(d *db.DB) *Store { return &Store{db: d} }

// InsertItem stores a normalized item. Returns the item id and true when the
// row was newly inserted (false = duplicate source+source_id, merged).
func (s *Store) InsertItem(ctx context.Context, it model.Item) (int64, bool, error) {
	meta, err := json.Marshal(it.Metadata)
	if err != nil {
		meta = []byte("{}")
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO items (source, source_id, url, canonical_url, author, author_id,
		                   title, content, published_at, collected_at, content_hash,
		                   topics, language, engagement, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (source, source_id) DO NOTHING
		RETURNING id`,
		it.Source, it.SourceID, it.URL, it.CanonicalURL, it.Author, it.AuthorID,
		it.Title, it.Content, sql.NullTime{Time: it.PublishedAt, Valid: !it.PublishedAt.IsZero()},
		time.Now().UTC(), ContentHash(it), it.Topics, it.Language,
		it.Engagement.Score, string(meta),
	).Scan(&id)
	if err == sql.ErrNoRows {
		// Conflict: fetch existing id.
		if err2 := s.db.QueryRowContext(ctx,
			`SELECT id FROM items WHERE source = $1 AND source_id = $2`, it.Source, it.SourceID,
		).Scan(&id); err2 != nil {
			return 0, false, err2
		}
		return id, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// AddItemSource records that an item was also seen through another source ref.
func (s *Store) AddItemSource(ctx context.Context, itemID int64, source, ref string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO item_sources (item_id, source, source_ref) VALUES ($1,$2,$3)
		ON CONFLICT DO NOTHING`, itemID, source, ref)
	return err
}

// ItemByID loads a single item.
func (s *Store) ItemByID(ctx context.Context, id int64) (model.Item, error) {
	var it model.Item
	var meta []byte
	var published sql.NullTime
	var id64 int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, source, source_id, url, canonical_url, author, author_id,
		       title, content, published_at, topics, language, engagement, metadata
		FROM items WHERE id = $1`, id,
	).Scan(&id64, &it.Source, &it.SourceID, &it.URL, &it.CanonicalURL, &it.Author, &it.AuthorID,
		&it.Title, &it.Content, &published, pqArray(&it.Topics), &it.Language, &it.Engagement.Score, &meta)
	if err != nil {
		return it, err
	}
	it.ID = it.Source + ":" + it.SourceID
	_ = id64
	if published.Valid {
		it.PublishedAt = published.Time
	}
	json.Unmarshal(meta, &it.Metadata)
	return it, nil
}

// UnscoredItems returns items of the last `since` window without a score.
func (s *Store) UnscoredItems(ctx context.Context, since time.Duration) ([]ScoredItem, error) {
	return s.queryItems(ctx, `
		SELECT i.id, i.source, i.source_id, i.url, i.canonical_url, i.author,
		       i.title, i.content, i.published_at, i.topics, i.engagement
		FROM items i
		LEFT JOIN scores s ON s.item_id = i.id
		WHERE s.item_id IS NULL
		  AND i.collected_at > now() - make_interval(secs => $1)
		ORDER BY i.collected_at DESC
		LIMIT 500`, int64(since.Seconds()))
}

// TopScoredItems returns the best items collected since the given duration,
// for briefing generation.
func (s *Store) TopScoredItems(ctx context.Context, since time.Duration, limit int) ([]ScoredItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.source, i.source_id, i.url, i.canonical_url, i.author,
		       i.title, i.content, i.published_at, i.topics, i.engagement,
		       s.importance, s.relevance, s.novelty, s.actionability,
		       s.personalization, s.final_score, s.model
		FROM items i
		JOIN scores s ON s.item_id = i.id
		WHERE i.collected_at > now() - make_interval(secs => $1)
		ORDER BY s.final_score DESC
		LIMIT $2`, int64(since.Seconds()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScoredItem
	for rows.Next() {
		var it ScoredItem
		var published sql.NullTime
		if err := rows.Scan(&it.DBID, &it.Source, &it.SourceID, &it.URL, &it.CanonicalURL,
			&it.Author, &it.Title, &it.Content, &published, pqArray(&it.Topics), &it.Engagement,
			&it.Score.Importance, &it.Score.Relevance, &it.Score.Novelty, &it.Score.Actionability,
			&it.Score.Personalization, &it.Score.Final, &it.Score.Model); err != nil {
			return nil, err
		}
		if published.Valid {
			it.PublishedAt = published.Time
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ScoredItem is an item row joined with its score.
type ScoredItem struct {
	DBID         int64
	Source       string
	SourceID     string
	URL          string
	CanonicalURL string
	Author       string
	Title        string
	Content      string
	PublishedAt  time.Time
	Topics       []string
	Engagement   int64

	Score Score
}

// Score mirrors the scores table.
type Score struct {
	Importance      float64
	Relevance       float64
	Novelty         float64
	Actionability   float64
	Personalization float64
	Final           float64
	Model           string
}

func (s *Store) queryItems(ctx context.Context, query string, args ...any) ([]ScoredItem, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ScoredItem
	for rows.Next() {
		var it ScoredItem
		var published sql.NullTime
		if err := rows.Scan(&it.DBID, &it.Source, &it.SourceID, &it.URL, &it.CanonicalURL,
			&it.Author, &it.Title, &it.Content, &published, pqArray(&it.Topics), &it.Engagement); err != nil {
			return nil, err
		}
		if published.Valid {
			it.PublishedAt = published.Time
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SaveScore upserts the score of an item.
func (s *Store) SaveScore(ctx context.Context, itemID int64, sc Score) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scores (item_id, importance, relevance, novelty, actionability,
		                    personalization, final_score, model)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (item_id) DO UPDATE SET
		    importance = EXCLUDED.importance,
		    relevance = EXCLUDED.relevance,
		    novelty = EXCLUDED.novelty,
		    actionability = EXCLUDED.actionability,
		    personalization = EXCLUDED.personalization,
		    final_score = EXCLUDED.final_score,
		    model = EXCLUDED.model,
		    created_at = now()`,
		itemID, sc.Importance, sc.Relevance, sc.Novelty, sc.Actionability,
		sc.Personalization, sc.Final, sc.Model)
	return err
}

// AddFeedback records a user action on an item.
func (s *Store) AddFeedback(ctx context.Context, itemID int64, action string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feedback (item_id, action) VALUES ($1,$2)`, itemID, action)
	return err
}

// FeedbackCounts returns action -> count for recent feedback, used by the
// personalization layer.
func (s *Store) FeedbackCounts(ctx context.Context, sinceDays int) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT action, count(*) FROM feedback
		WHERE created_at > now() - ($1 || ' days')::interval
		GROUP BY action`, sinceDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var a string
		var c int
		if err := rows.Scan(&a, &c); err != nil {
			return nil, err
		}
		out[a] = c
	}
	return out, rows.Err()
}

// SaveBriefing stores the daily briefing content.
func (s *Store) SaveBriefing(ctx context.Context, date string, content string, itemIDs []int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO briefings (date, content, item_ids) VALUES ($1,$2,$3)
		ON CONFLICT (date) DO UPDATE SET content = EXCLUDED.content, item_ids = EXCLUDED.item_ids`,
		date, content, int64Array(itemIDs))
	return err
}

// SaveRun records a pipeline run for observability.
func (s *Store) SaveRun(ctx context.Context, kind, source string, start, end time.Time, collected, failed int, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (kind, source, start_time, end_time, items_collected, items_failed, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		kind, source, start, end, collected, failed, errMsg)
	return err
}

// GetFeedState / SaveFeedState persist RSS conditional-GET tokens.
func (s *Store) GetFeedState(ctx context.Context, name string) (etag, lastModified string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT etag, last_modified FROM feed_state WHERE name = $1`, name,
	).Scan(&etag, &lastModified)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return
}

func (s *Store) SaveFeedState(ctx context.Context, name, etag, lastModified string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feed_state (name, etag, last_modified, last_fetched) VALUES ($1,$2,$3,now())
		ON CONFLICT (name) DO UPDATE SET etag = EXCLUDED.etag,
		    last_modified = EXCLUDED.last_modified, last_fetched = now()`,
		name, etag, lastModified)
	return err
}

// PreferenceWeight reads a personalization weight (0 when unknown).
func (s *Store) PreferenceWeight(ctx context.Context, kind, name string) (float64, error) {
	var w float64
	err := s.db.QueryRowContext(ctx,
		`SELECT weight FROM user_preferences WHERE kind = $1 AND name = $2`, kind, name).Scan(&w)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return w, err
}

// AdjustPreference applies delta to a preference weight (clamped to [-3, 3]).
func (s *Store) AdjustPreference(ctx context.Context, kind, name string, delta float64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_preferences (kind, name, weight) VALUES ($1,$2,$3)
		ON CONFLICT (kind, name) DO UPDATE SET
		    weight = LEAST(3, GREATEST(-3, user_preferences.weight + $3))`,
		kind, name, delta)
	return err
}

// AllPreferences returns every stored preference weight.
func (s *Store) AllPreferences(ctx context.Context) (map[string]map[string]float64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, name, weight FROM user_preferences`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]float64{}
	for rows.Next() {
		var kind, name string
		var w float64
		if err := rows.Scan(&kind, &name, &w); err != nil {
			return nil, err
		}
		if out[kind] == nil {
			out[kind] = map[string]float64{}
		}
		out[kind][name] = w
	}
	return out, rows.Err()
}
