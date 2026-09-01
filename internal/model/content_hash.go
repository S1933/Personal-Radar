package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ContentHash builds a stable hash of the meaningful parts of an item,
// used for cross-source content deduplication. Lowercased + trimmed
// title and the first 2 KB of lowercased + trimmed content are
// hashed. Living in package model — not store — because the hash is a
// property of the item, not of the storage layer, and ingestion
// needs to compute it without importing the store package (cycle).
//
// Items with neither title nor content (rare but real: malformed
// RSS, posts with only a link) return "". The FindDuplicate SQL
// guard "content_hash <> ''" then refuses to merge them, so two
// empty-content items don't end up fused under a single hash.
func ContentHash(it Item) string {
	title := strings.ToLower(strings.TrimSpace(it.Title))
	c := strings.ToLower(strings.TrimSpace(it.Content))
	if title == "" && c == "" {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(title))
	h.Write([]byte{0})
	if len(c) > 2048 {
		c = c[:2048]
	}
	h.Write([]byte(c))
	return hex.EncodeToString(h.Sum(nil))
}
