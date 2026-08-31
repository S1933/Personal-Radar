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
