// Package web serves the Personal Radar bookmark dashboard. The HTTP API
// runs alongside the existing radar pipeline and reads the same PostgreSQL
// pool as collect / rank / briefing.
//
// Bind address defaults to 127.0.0.1:8081 (localhost only). The dashboard is
// meant to be tunneled (ssh -L, cloudflared) or proxied by an existing
// trust boundary — the server intentionally has no built-in auth.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/S1933/personal-radar/internal/logging"
	"github.com/S1933/personal-radar/internal/store"
)

//go:embed static
var staticFS embed.FS

// Config configures the bookmark server.
type Config struct {
	// Addr is the listen address. Default: "127.0.0.1:8081".
	Addr string
	// ReadHeaderTimeout protects against slowloris. Zero = no timeout.
	ReadHeaderTimeout time.Duration
}

// Server is the bookmark HTTP API + dashboard.
type Server struct {
	cfg   Config
	store *store.Store
	log   *logging.Logger
	srv   *http.Server
}

// New constructs a Server. Call Start to listen; Stop to shut down.
func New(cfg Config, st *store.Store, log *logging.Logger) *Server {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8081"
	}
	if cfg.ReadHeaderTimeout == 0 {
		cfg.ReadHeaderTimeout = 5 * time.Second
	}
	s := &Server{cfg: cfg, store: st, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bookmarks", s.handleList)
	mux.HandleFunc("/api/bookmarks/", s.handleItem) // /api/bookmarks/{id} and /{id}/read etc.
	mux.Handle("/", s.handleStatic())
	s.srv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           withLogging(mux, log),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	return s
}

// Start blocks until the server exits. Returns nil on graceful shutdown.
func (s *Server) Start() error {
	s.log.Info("bookmark server listening", "addr", s.cfg.Addr)
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop performs a graceful shutdown with a short drain window.
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// handleList serves GET /api/bookmarks?filter=unread|read|all&limit=&offset=
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	filter := store.BookmarkFilter(r.URL.Query().Get("filter"))
	switch filter {
	case store.BookmarkUnread, store.BookmarkRead, store.BookmarkAll, "":
		// ok
	default:
		writeError(w, http.StatusBadRequest, `filter must be "unread", "read", or "all"`)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	items, total, err := s.store.ListBookmarks(r.Context(), filter, limit, offset)
	if err != nil {
		s.log.Error("list bookmarks", "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	if items == nil {
		items = []store.Bookmark{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
		"filter": string(filterOr(filter, store.BookmarkUnread)),
	})
}

// handleItem dispatches /api/bookmarks/{id} and /api/bookmarks/{id}/{action}.
func (s *Server) handleItem(w http.ResponseWriter, r *http.Request) {
	// Path is /api/bookmarks/{id} or /api/bookmarks/{id}/{action}
	path := r.URL.Path[len("/api/bookmarks/"):]
	if path == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var idStr, action string
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			idStr = path[:i]
			action = path[i+1:]
			break
		}
	}
	if idStr == "" {
		idStr = path
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	switch {
	case r.Method == http.MethodDelete && action == "":
		s.dispatchDelete(w, r, id)
	case r.Method == http.MethodPost && action == "read":
		s.dispatchSetRead(w, r, id, true)
	case r.Method == http.MethodPost && action == "unread":
		s.dispatchSetRead(w, r, id, false)
	case r.Method == http.MethodGet && action == "":
		s.dispatchGet(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) dispatchDelete(w http.ResponseWriter, r *http.Request, id int64) {
	if err := s.store.HardDelete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.log.Error("hard delete", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) dispatchSetRead(w http.ResponseWriter, r *http.Request, id int64, v bool) {
	var err error
	if v {
		err = s.store.MarkRead(r.Context(), id)
	} else {
		err = s.store.MarkUnread(r.Context(), id)
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		s.log.Error("set read flag", "id", id, "value", v, "error", err)
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "is_read": v})
}

func (s *Server) dispatchGet(w http.ResponseWriter, r *http.Request, id int64) {
	// Single-item view; not used by the current dashboard but handy for
	// debugging from curl.
	items, _, err := s.store.ListBookmarks(r.Context(), store.BookmarkAll, 1, 0)
	_ = items
	_ = err
	// Reuse ItemByID for the canonical view.
	it, err := s.store.ItemByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// handleStatic serves the embedded dashboard. Falls back to index.html for
// any non-API path so the SPA-style UI works without server-side routing.
func (s *Server) handleStatic() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// Should never happen — embed validated at compile time.
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html for the root and unknown paths.
		if r.URL.Path == "/" || r.URL.Path == "" {
			data, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, "index missing", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(data)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		fileServer.ServeHTTP(w, r)
	})
}

// withLogging emits a one-line access log per request via the structured logger.
func withLogging(next http.Handler, log *logging.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) {
	s.status = c
	s.ResponseWriter.WriteHeader(c)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func filterOr(f, def store.BookmarkFilter) store.BookmarkFilter {
	if f == "" {
		return def
	}
	return f
}
