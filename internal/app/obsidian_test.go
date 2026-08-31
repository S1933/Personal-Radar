package app

import (
	"path/filepath"
	"testing"
)

func TestObsidianFilePath(t *testing.T) {
	// t.TempDir() yields an absolute, OS-correct path. The relative
	// segments we append must land exactly where SaveToObsidian expects.
	vault := t.TempDir()
	got := obsidianFilePath(vault, 42, "2026-08-31")
	want := filepath.Join(vault, "Daily Radar", "2026-08-31", "item-42.md")
	if got != want {
		t.Fatalf("obsidianFilePath() = %q, want %q", got, want)
	}
}

func TestObsidianFilePathHandlesSpaces(t *testing.T) {
	// "Daily Radar" contains a space. A naive string concat would
	// produce "Daily Radar/2026-08-31/item-42.md" too — but on Windows
	// separators, and with vault paths that may themselves contain
	// spaces, filepath.Join is the only safe joiner.
	vault := t.TempDir()
	got := obsidianFilePath(vault, 1, "2026-08-31")
	// The path must contain a space-separated "Daily Radar" segment.
	if !filepathContainsSegment(got, "Daily Radar") {
		t.Fatalf("path %q does not contain the 'Daily Radar' segment intact", got)
	}
}

func filepathContainsSegment(p, segment string) bool {
	for _, part := range filepath.SplitList(p) {
		if part == p {
			continue
		}
	}
	// filepath.SplitList only works for PATH-style lists; do a manual
	// walk over the path's segments instead.
	for i := 0; i < len(p); {
		j := i
		for j < len(p) && p[j] != filepath.Separator {
			j++
		}
		if p[i:j] == segment {
			return true
		}
		if j < len(p) {
			i = j + 1
		} else {
			i = j
		}
	}
	return false
}
