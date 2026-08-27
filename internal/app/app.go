package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/S1933/personal-radar/internal/briefing"
	"github.com/S1933/personal-radar/internal/collectors/github"
	"github.com/S1933/personal-radar/internal/collectors/linkedin"
	"github.com/S1933/personal-radar/internal/collectors/reddit"
	"github.com/S1933/personal-radar/internal/collectors/rss"
	"github.com/S1933/personal-radar/internal/collectors/x"
	"github.com/S1933/personal-radar/internal/config"
	"github.com/S1933/personal-radar/internal/db"
	"github.com/S1933/personal-radar/internal/dedup"
	"github.com/S1933/personal-radar/internal/ingestion"
	"github.com/S1933/personal-radar/internal/logging"
	"github.com/S1933/personal-radar/internal/personalization"
	"github.com/S1933/personal-radar/internal/ranking"
	"github.com/S1933/personal-radar/internal/scheduler"
	"github.com/S1933/personal-radar/internal/store"
	"github.com/S1933/personal-radar/internal/telegram"
)

// App wires every component of the radar together. It owns the database
// pool and the lifecycle of long-running workers (scheduler, telegram).
type App struct {
	Cfg *config.Config
	Log *logging.Logger

	DB        *db.DB
	Store     *store.Store
	Ingest    *ingestion.Service
	Dedup     *dedup.Service
	Ranker    *ranking.Service
	Prefs     *personalization.Service
	Briefer   *briefing.Service
	Telegram  *telegram.Client
	Scheduler *scheduler.Scheduler
	DeepDive  *DeepDive
}

// New builds the App: opens the DB pool and wires services. Collectors are
// registered per collection cycle based on configuration and credentials.
func New(ctx context.Context, cfg *config.Config, log *logging.Logger) (*App, error) {
	database, err := db.Open(ctx, cfg.Database.DSN())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	st := store.New(database)

	ranker := ranking.New(cfg.Models, st, log.With("sub", "ranking"))
	prefs := personalization.New(st)

	briefer := briefing.New(briefing.Options{
		MaxItems:  cfg.Briefing.MaxItems,
		MaxTrends: cfg.Briefing.MaxTrends,
		Location:  cfg.Location(),
	}, st, ranker, log.With("sub", "briefing"))

	var tg *telegram.Client
	if cfg.Telegram.Enabled {
		tg, err = telegram.NewClient(cfg.Telegram, log.With("sub", "telegram"))
		if err != nil {
			// Non-fatal: collect/rank/migrate must work without Telegram.
			log.Warn("telegram disabled", "error", err.Error())
			tg = nil
		} else {
			briefer.SetTelegram(tg)
		}
	}

	svc := ingestion.New(st, log.With("sub", "ingestion"))

	app := &App{
		Cfg:       cfg,
		Log:       log,
		DB:        database,
		Store:     st,
		Ingest:    svc,
		Dedup:     dedup.New(st),
		Ranker:    ranker,
		Prefs:     prefs,
		Briefer:   briefer,
		Telegram:  tg,
		Scheduler: scheduler.New(log.With("sub", "scheduler")),
		DeepDive:  NewDeepDive(cfg.Models),
	}
	return app, nil
}

// Close releases resources held by the app (DB pool).
func (a *App) Close() {
	if a.DB != nil {
		a.DB.Close()
	}
}

// Migrate applies all pending migrations.
func (a *App) Migrate(ctx context.Context) error {
	return a.DB.Migrate(ctx)
}

// collectors builds the enabled collector set for this cycle.
func (a *App) collectors(ctx context.Context) []ingestion.Collector {
	var out []ingestion.Collector
	if a.Cfg.RSS.Enabled && len(a.Cfg.RSS.Feeds) > 0 {
		out = append(out, rss.NewCollector(a.Cfg.RSS, a.Log.With("sub", "rss")))
	}
	if a.Cfg.Reddit.Enabled && len(a.Cfg.Reddit.Subreddits) > 0 && a.Cfg.Reddit.Every == 0 {
		c, err := reddit.NewCollector(ctx, a.Cfg.Reddit, a.Log.With("sub", "reddit"))
		if err == nil {
			out = append(out, c)
		} else {
			// No OAuth credentials: fall back to the public RSS adapter
			// (best-effort, same pattern as LinkedIn public pages).
			a.Log.Warn("reddit oauth unavailable, using public adapter", "error", err.Error())
			out = append(out, reddit.NewPublicCollector(a.Cfg.Reddit, a.Log.With("sub", "reddit-public")))
		}
	}
	if a.Cfg.GitHub.Enabled && (len(a.Cfg.GitHub.Repositories) > 0 || len(a.Cfg.GitHub.Organizations) > 0) {
		if c, err := github.NewCollector(a.Cfg.GitHub, a.Log.With("sub", "github")); err == nil {
			out = append(out, c)
		} else {
			a.Log.Warn("github disabled for this cycle", "error", err)
		}
	}
	if a.Cfg.LinkedIn.Enabled && len(a.Cfg.LinkedIn.Pages) > 0 {
		out = append(out, linkedin.NewCollector(a.Cfg.LinkedIn, a.Log.With("sub", "linkedin")))
	}
	if a.Cfg.X.Enabled && (len(a.Cfg.X.Accounts) > 0 || len(a.Cfg.X.Queries) > 0) {
		py := os.Getenv("X_PYTHON")
		if py == "" {
			py = "/usr/bin/python3" // system python with twscrape (Docker image)
		}
		c, err := x.NewCollector(a.Cfg.X, a.Log.With("sub", "x"),
			x.WithScriptPath("xscraper/collect.py"),
			x.WithVenvPython(py),
			x.WithTwscrapeDB("/app/x_accounts.db"),
		)
		if err != nil {
			a.Log.Warn("x disabled", "error", err.Error())
		} else {
			out = append(out, c)
		}
	}
	return out
}

// CollectOnce runs all enabled collectors, normalizes, dedupes and stores.
// A failing collector never aborts the cycle (isolation requirement).
func (a *App) CollectOnce(ctx context.Context) (int, error) {
	collectors := a.collectors(ctx)
	if len(collectors) == 0 {
		return 0, errors.New("no collector enabled")
	}
	var total int
	for _, c := range collectors {
		start := time.Now()
		items, err := c.Collect(ctx)
		if err != nil {
			a.Log.Warn("collector failed", "collector", c.Name(), "error", err, "duration_ms", time.Since(start).Milliseconds())
			continue
		}
		// Duplicate items returned by several collectors (or the same feed)
		// are merged into a single row on insert; existing rows are counted
		// via item_sources.
		inserted, err := a.Ingest.IngestBatch(ctx, c.Name(), items)
		if err != nil {
			a.Log.Warn("ingest failed", "collector", c.Name(), "error", err)
			continue
		}
		total += inserted
		a.Log.Info("collect ok", "collector", c.Name(), "items", len(items), "new", inserted, "duration_ms", time.Since(start).Milliseconds())
	}
	return total, nil
}

// collectReddit runs only the Reddit collector on its own (slower) interval
// to stay under Reddit's anonymous-RSS rate limit. It is used when
// reddit.every is configured; the global CollectOnce then skips Reddit.
func (a *App) collectReddit(ctx context.Context) error {
	var c ingestion.Collector
	var err error
	if c, err = reddit.NewCollector(ctx, a.Cfg.Reddit, a.Log.With("sub", "reddit")); err != nil {
		a.Log.Warn("reddit oauth unavailable, using public adapter", "error", err.Error())
		c = reddit.NewPublicCollector(a.Cfg.Reddit, a.Log.With("sub", "reddit-public"))
	}
	start := time.Now()
	items, err := c.Collect(ctx)
	if err != nil {
		a.Log.Warn("reddit collector failed", "error", err, "duration_ms", time.Since(start).Milliseconds())
		return err
	}
	inserted, err := a.Ingest.IngestBatch(ctx, c.Name(), items)
	if err != nil {
		a.Log.Warn("reddit ingest failed", "error", err)
		return err
	}
	a.Log.Info("collect ok", "collector", c.Name(), "items", len(items), "new", inserted, "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// RankPending scores items that do not have a score yet.
func (a *App) RankPending(ctx context.Context) (int, error) {
	return a.Ranker.RankPending(ctx)
}

// Briefing generates (and sends when configured) the daily briefing.
func (a *App) Briefing(ctx context.Context) (string, error) {
	return a.Briefer.Generate(ctx, briefing.SendOption(a.Cfg.Briefing.Send))
}

// Run starts the scheduler (collection + briefing) and the telegram listener.
func (a *App) Run(ctx context.Context) error {
	// Apply migrations at boot so fresh deployments work without manual steps.
	if err := a.Migrate(ctx); err != nil {
		a.Log.Warn("auto-migrate failed", "error", err.Error())
	}
	// Collection every 20 minutes (RSS, GitHub, X, and Reddit only when
	// Reddit has no dedicated interval configured).
	a.Scheduler.Add("collect", scheduler.Spec{
		Every: 20 * time.Minute,
		Run: func(ctx context.Context) {
			if _, err := a.CollectOnce(ctx); err != nil {
				a.Log.Warn("collect cycle", "error", err)
			}
		},
	})
	// Reddit may run on its own slower interval to stay under Reddit's
	// anonymous-RSS rate limit. When reddit.every is set, CollectOnce
	// skips Reddit (it has its own job below) so we never double-collect.
	if a.Cfg.Reddit.Enabled && a.Cfg.Reddit.Every > 0 {
		a.Scheduler.Add("reddit-collect", scheduler.Spec{
			Every: a.Cfg.Reddit.Every,
			Run: func(ctx context.Context) {
				if err := a.collectReddit(ctx); err != nil {
					a.Log.Warn("reddit collect", "error", err)
				}
			},
		})
	}
	// Briefing once per day at the configured local time.
	a.Scheduler.Add("briefing", scheduler.Spec{
		DailyAt: a.Cfg.Briefing.Schedule,
		Loc:     a.Cfg.Location(),
		Run: func(ctx context.Context) {
			if _, err := a.Briefer.Generate(ctx, briefing.SendOption(true)); err != nil {
				a.Log.Error("briefing", "error", err)
			}
		},
	})
	a.Scheduler.Start(ctx)

	if a.Telegram != nil {
		return a.Telegram.Listen(ctx, a.TelegramHandlers())
	}
	<-ctx.Done()
	return nil
}
