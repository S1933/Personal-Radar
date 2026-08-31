package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/db"
	"github.com/S1933/personal-radar/internal/store"
)

func TestDeepDiveReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short")
	}
	key := os.Getenv("OPENAI_API_KEY")
	base := os.Getenv("OPENAI_BASE_URL")
	if key == "" || base == "" {
		t.Skip("OPENAI_API_KEY / OPENAI_BASE_URL not set")
	}
	dsn := os.Getenv("RADAR_DB_DSN")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=radar password=radar dbname=radar sslmode=disable"
	}
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
		Cfg:     &config.Config{Models: config.ModelsConfig{BaseURL: base, APIKey: key, DeepDive: config.ModelConfig{Model: "deepseek-v4-flash"}}},
		Store:   st,
		DeepDive: NewDeepDive(config.ModelsConfig{BaseURL: base, APIKey: key, DeepDive: config.ModelConfig{Model: "deepseek-v4-flash"}}),
	}
	out, err := a.DeepDiveItem(context.Background(), items[0].DBID)
	if err != nil {
		t.Fatalf("deepdive: %v", err)
	}
	if len(out) < 50 {
		t.Fatalf("suspiciously short deep dive: %q", out)
	}
	t.Logf("deepdive (%d chars):\n%s", len(out), out)
}
