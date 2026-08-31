package briefing

import (
	"testing"

	"github.com/S1933/personal-radar/internal/store"
)

func TestApplySourceQuota(t *testing.T) {
	mk := func(id int64, src string, score float64) store.ScoredItem {
		return store.ScoredItem{DBID: id, Source: src, Score: store.Score{Final: score}}
	}
	// 12 reddit (high), 15 x (low), 3 rss
	items := []store.ScoredItem{}
	for i := int64(0); i < 12; i++ {
		items = append(items, mk(100+i, "reddit", 0.9-float64(i)*0.01))
	}
	for i := int64(0); i < 15; i++ {
		items = append(items, mk(200+i, "x", 0.5-float64(i)*0.001))
	}
	for i := int64(0); i < 3; i++ {
		items = append(items, mk(300+i, "rss", 0.4-float64(i)*0.01))
	}
	sel := applySourceQuota(items, 10)
	counts := map[string]int{}
	for _, it := range sel {
		counts[it.Source]++
	}
	if counts["x"] < 2 {
		t.Errorf("x should have >= 2 seats, got %d", counts["x"])
	}
	if counts["reddit"] > 4 {
		t.Errorf("reddit should have <= 4 seats, got %d", counts["reddit"])
	}
	if counts["rss"] < 2 {
		t.Errorf("rss should have >= 2 seats, got %d", counts["rss"])
	}
	if len(sel) != 10 {
		t.Errorf("should select 10, got %d", len(sel))
	}
	t.Logf("selected: %v", counts)
}

func TestApplySourceQuotaRespectsMax(t *testing.T) {
	mk := func(src string, score float64) store.ScoredItem {
		return store.ScoredItem{Source: src, Score: store.Score{Final: score}}
	}
	var items []store.ScoredItem
	// 6 sources × 5 items each, descending score within each source
	// so the top-scoring source visits the phase-1 loop first.
	for _, src := range []string{"rss", "github", "reddit", "x", "linkedin", "hn"} {
		for i := 0; i < 5; i++ {
			items = append(items, mk(src, 1.0-float64(i)*0.01))
		}
	}
	// 6 sources × minSeats 2 = 12 > max 10: this is the case that
	// used to overflow. The fix bounds the last batch to fit.
	got := applySourceQuota(items, 10)
	if len(got) > 10 {
		t.Fatalf("quota a renvoyé %d items pour un max de 10", len(got))
	}
	if len(got) == 0 {
		t.Fatal("quota n'a rien sélectionné")
	}
}

func TestDedupeByTitle(t *testing.T) {
	in := []store.ScoredItem{
		{Title: "Go 1.26 released", Source: "rss"},
		{Title: "Go 1.26 released!", Source: "x"}, // same key after normTitle
		{Title: "Rust 1.90 released", Source: "rss"},
		{Title: "go 1.26 RELEASED", Source: "reddit"}, // still the same key
	}
	got := dedupeByTitle(in)
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2", len(got))
	}
	if got[0].Source != "rss" {
		t.Errorf("la première occurrence doit gagner (ordre de score préservé), got %q", got[0].Source)
	}
	if got[1].Source != "rss" {
		t.Errorf("Rust doit venir de rss, got %q", got[1].Source)
	}
}

func TestDedupeByTitleEmpty(t *testing.T) {
	if got := dedupeByTitle(nil); len(got) != 0 {
		t.Errorf("nil → empty, got %d", len(got))
	}
}
