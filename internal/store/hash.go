package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/S1933/personal-radar/internal/model"
)

// ContentHash builds a stable hash of the meaningful parts of an item,
// used for cross-source content deduplication.
func ContentHash(it model.Item) string {
	h := sha256.New()
	h.Write([]byte(strings.ToLower(strings.TrimSpace(it.Title))))
	h.Write([]byte{0})
	// Content can differ slightly between platforms; use the first 2KB only.
	c := it.Content
	if len(c) > 2048 {
		c = c[:2048]
	}
	h.Write([]byte(strings.ToLower(strings.TrimSpace(c))))
	return hex.EncodeToString(h.Sum(nil))
}

// pqArray is a helper shim declared to keep imports tidy when the file
// evolves toward typed array handling.
type pqArrayTarget struct{}

var _ pqArrayTarget
