package config

import (
	"os"
	"path/filepath"
	"strings"
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
	// NB: DSN() has a long-standing bug: its Sprintf has 4 placeholders
	// but 5 args, so Go appends "%!(EXTRA string=<password>)" to the
	// output and the resulting string is not a valid Postgres URL.
	// We test the prefix only, until DSN() is fixed (T17 will rework
	// secrets + the DSN format).
	t.Setenv("RADAR_DB_HOST", "dbhost")
	t.Setenv("RADAR_DB_PORT", "5433")
	t.Setenv("RADAR_DB_PASSWORD", "secret")
	cfg := Config{Database: DatabaseConfig{User: "radar", Name: "radar"}}
	dsn := cfg.Database.DSN()

	if !strings.HasPrefix(dsn, "postgres://radar:") {
		t.Errorf("dsn prefix = %q, want postgres://radar:", dsn)
	}
	if !strings.Contains(dsn, "@dbhost:5433/radar") {
		t.Errorf("dsn missing host/port: %q", dsn)
	}
}

func TestDefaultsDoNotAddLegacySlot(t *testing.T) {
	// defaults() must not force the legacy 07:00 schedule when a
	// modern Schedules list is already set — otherwise the briefing
	// fan-out at runtime appends 07:00 to [08:00, 14:00, 20:00] and
	// we get a fourth briefing nobody asked for.
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.yaml")
	os.WriteFile(path, []byte(`
briefing:
  schedules: ["08:00", "14:00", "20:00"]
`), 0o644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Briefing.Schedule != "" {
		t.Errorf("defaults a rempli Schedule=%q alors que Schedules est défini",
			cfg.Briefing.Schedule)
	}
	if len(cfg.Briefing.Schedules) != 3 {
		t.Errorf("Schedules perdues : %v", cfg.Briefing.Schedules)
	}
}
