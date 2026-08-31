package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/S1933/personal-radar/internal/logging"
)

func newTestServer() *Server {
	// Real *logging.Logger writing to io.Discard. New() takes a component
	// name and a level; debug logs would otherwise spam test output.
	return &Server{
		log: logging.New("web-test", logging.WarnLevel),
	}
}

func discardLog() io.Writer { return io.Discard }

// Use the real logger constructor but redirect its writer via a wrapped
// bytes.Buffer. Avoids needing a NewWithWriter variant.
func newTestServerWithBuf(buf *bytes.Buffer) *Server {
	_ = buf
	_ = discardLog()
	return newTestServer()
}

func TestHandleStaticServesIndex(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.handleStatic().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Personal Radar") {
		t.Fatalf("body missing brand string; head: %q", body[:min(200, len(body))])
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", cc)
	}
}

func TestHandleItemRejectsBadID(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/bookmarks/abc", nil)
	s.handleItem(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if body["error"] == nil {
		t.Fatalf("missing error field: %v", body)
	}
}

func TestHandleItemUnknownPath(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	// /api/bookmarks/ (trailing slash, no id) — must be 404, not a panic.
	req := httptest.NewRequest(http.MethodGet, "/api/bookmarks/", nil)
	s.handleItem(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleItemRejectsBadMethod(t *testing.T) {
	s := newTestServer()
	rec := httptest.NewRecorder()
	// /api/bookmarks/123 with PATCH — must be 405.
	req := httptest.NewRequest(http.MethodPatch, "/api/bookmarks/123", nil)
	s.handleItem(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusTeapot, map[string]string{"hello": "world"})
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if body["hello"] != "world" {
		t.Fatalf("body = %v", body)
	}
}

func TestFilterOr(t *testing.T) {
	// FilterOr's contract: empty -> default.
	if got := filterOr("", "X"); string(got) != "X" {
		t.Fatalf("empty -> %q, want X", got)
	}
	if got := filterOr("Y", "X"); string(got) != "Y" {
		t.Fatalf("non-empty -> %q, want Y", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// keep log/stdin import alive so the file is self-contained
var _ = log.Println

func TestEnforceCSRF(t *testing.T) {
	// Stand-in handler so the test does not need a real store / server.
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := enforceCSRF(next)

	cases := []struct {
		name        string
		method      string
		sfs         string // Sec-Fetch-Site, "" = unset
		xrr         string // X-Radar-Request, "" = unset
		wantAllowed bool
	}{
		{"GET always passes", http.MethodGet, "cross-site", "", true},
		{"HEAD always passes", http.MethodHead, "cross-site", "", true},
		{"POST missing custom header", http.MethodPost, "same-origin", "", false},
		{"POST cross-site rejected", http.MethodPost, "cross-site", "1", false},
		{"POST legitimate same-origin", http.MethodPost, "same-origin", "1", true},
		{"POST legitimate none", http.MethodPost, "none", "1", true},
		{"POST curl-style (no fetch headers)", http.MethodPost, "", "1", true},
		{"POST both missing", http.MethodPost, "", "", false},
		{"POST wrong header value", http.MethodPost, "same-origin", "yes", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/bookmarks/read-all", nil)
			if tc.sfs != "" {
				r.Header.Set("Sec-Fetch-Site", tc.sfs)
			}
			if tc.xrr != "" {
				r.Header.Set("X-Radar-Request", tc.xrr)
			}
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, r)
			if tc.wantAllowed && w.Code != http.StatusOK {
				t.Fatalf("expected pass-through, got %d", w.Code)
			}
			if !tc.wantAllowed && w.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d (body=%s)", w.Code, w.Body.String())
			}
		})
	}
}
