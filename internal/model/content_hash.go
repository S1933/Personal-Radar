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
func ContentHash(it Item) string {
	h := sha256.New()
	h.Write([]byte(strings.ToLower(strings.TrimSpace(it.Title))))
	h.Write([]byte{0})
	c := it.Content
	if len(c) > 2048 {
		c = c[:2048]
	}
	h.Write([]byte(strings.ToLower(strings.TrimSpace(c))))
	return hex.EncodeToString(h.Sum(nil))
}
