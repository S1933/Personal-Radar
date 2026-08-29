package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestExcerpt verifies the excerpt collapses whitespace, drops HTML, and
// truncates on word boundaries.
func TestExcerpt(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"hello world", "hello world"},
		{"<p>hello <b>world</b></p> foo", "hello world foo"},
		{"a\n\nb\tc", "a b c"},
	}
	for _, c := range cases {
		got := excerpt(c.in, 200)
		if got != c.want {
			t.Errorf("excerpt(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestExcerptTruncates checks word-boundary truncation with ellipsis.
func TestExcerptTruncates(t *testing.T) {
	got := excerpt("un deux trois quatre cinq six sept huit neuf dix", 15)
	if len(got) > 18 { // 15 + ellipsis (3 bytes)
		t.Errorf("excerpt too long: %q (%d)", got, len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("excerpt missing ellipsis: %q", got)
	}
}

// TestExcerptStringEllipsis checks the ellipsis char directly. Cut short
// of half keeps the last word boundary only when it's past the middle.
func TestExcerptStringEllipsis(t *testing.T) {
	got := excerpt("un deux trois", 10) // "un deux t…" → boundary at 2 (≤5): keep
	want := "un deux…"
	if got != want {
		t.Errorf("excerpt = %q, want %q", got, want)
	}
}

// TestStripTags covers tag removal edge cases.
func TestStripTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no tags here", "no tags here"},
		{"<a href='x'>link</a>", "link"},
		{"a<b>c</b>d", "acd"},
		{"<img src=x>", ""},
	}
	for _, c := range cases {
		if got := stripTags(c.in); got != c.want {
			t.Errorf("stripTags(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestHandleSummaryRejectsBadID ensures the summary endpoint 400s on a
// malformed id without panicking.
func TestHandleSummaryRejectsBadID(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/summary/abc", nil)
	s.handleSummary(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}