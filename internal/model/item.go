package model

import "time"

// Item is the unified normalized record every collector must produce.
// The pipeline never manipulates source-specific payloads — only Items.
type Item struct {
	ID          string
	Source      string // rss | reddit | github | x | linkedin
	SourceID    string // unique id within the source
	Author      string
	AuthorID    string
	Title       string
	Content     string
	URL         string
	CanonicalURL string
	PublishedAt time.Time
	CollectedAt time.Time

	Topics    []string
	Language  string
	Engagement Engagement
	Metadata  map[string]string
}

type Engagement struct {
	Score  int64 // likes/score/points depending on source
	Comments int64
}

// Clone returns a deep copy safe to mutate.
func (i Item) Clone() Item {
	out := i
	out.Topics = append([]string(nil), i.Topics...)
	out.Metadata = make(map[string]string, len(i.Metadata))
	for k, v := range i.Metadata {
		out.Metadata[k] = v
	}
	return out
}
