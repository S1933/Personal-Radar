package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/db"
	"github.com/S1933/personal-radar/internal/store"
)

// TestSaveToObsidianReal verifies /save writes a markdown note into the
// configured vault path. Requires RADAR_DB_* env + a vault path.
func TestSaveToObsidianReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network/fs test in -short")
	}
	vault := os.Getenv("RADAR_OBSIDIAN_TEST_VAULT")
	if vault == "" {
		t.Skip("RADAR_OBSIDIAN_TEST_VAULT not set")
	}
	dsn := envOr("RADAR_DB_DSN", "host=localhost port=5499 user=radar password=radar dbname=radar sslmode=disable")
	database, err := db.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	st := store.New(database)

	items, err := st.TopScoredItems(context.Background(), 48*time.Hour, 1)
	if err != nil || len(items) == 0 {
		t.Fatalf("no items: %v", err)
	}
	a := &App{
		Cfg:   &config.Config{Obsidian: config.ObsidianConfig{Enabled: true, VaultPath: vault}},
		Store: st,
	}
	if err := a.SaveToObsidian(context.Background(), items[0].DBID); err != nil {
		t.Fatalf("save: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(vault, "**", "item-*.md"))
	if len(matches) == 0 {
		// Fallback: recursive walk in case glob ** is unsupported.
		_ = filepath.Walk(vault, func(p string, info os.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(p, ".md") && strings.Contains(p, "item-") {
				matches = append(matches, p)
			}
			return nil
		})
	}
	if len(matches) == 0 {
		t.Fatal("expected a markdown note in vault")
	}
	t.Logf("wrote: %s", matches[0])
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
