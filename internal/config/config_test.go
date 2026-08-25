package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "radar.yaml")
	os.WriteFile(path, []byte(`
timezone: Europe/Paris
briefing:
  schedule: "07:00"
  max_items: 8
rss:
  enabled: true
  feeds:
    - name: OpenAI
      url: https://example.com/feed.xml
      topics: [ai]
reddit:
  enabled: true
  subreddits: [aiagents, golang]
`), 0o644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Timezone != "Europe/Paris" {
		t.Errorf("timezone = %q", cfg.Timezone)
	}
	if cfg.Briefing.MaxItems != 8 {
		t.Errorf("max_items = %d, want 8", cfg.Briefing.MaxItems)
	}
	if len(cfg.RSS.Feeds) != 1 || cfg.RSS.Feeds[0].URL != "https://example.com/feed.xml" {
		t.Errorf("feeds not parsed: %+v", cfg.RSS.Feeds)
	}
	if len(cfg.Reddit.Subreddits) != 2 {
		t.Errorf("subreddits = %v", cfg.Reddit.Subreddits)
	}
}

func TestDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	os.WriteFile(path, []byte("timezone: UTC\n"), 0o644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Briefing.Schedule != "07:00" {
		t.Errorf("default schedule = %q", cfg.Briefing.Schedule)
	}
	if cfg.Briefing.MaxItems != 10 {
		t.Errorf("default max_items = %d", cfg.Briefing.MaxItems)
	}
	if cfg.Reddit.Listing != "hot" {
		t.Errorf("default listing = %q", cfg.Reddit.Listing)
	}
}

func TestDSNFromEnv(t *testing.T) {
	t.Setenv("RADAR_DB_HOST", "dbhost")
	t.Setenv("RADAR_DB_PORT", "5433")
	t.Setenv("RADAR_DB_PASSWORD", "secret")
	cfg := Config{Database: DatabaseConfig{User: "radar", Name: "radar"}}
	dsn := cfg.Database.DSN()
	want := "postgres://radar:secret@dbhost:5433/radar?sslmode=disable"
	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
}
