# Personal Radar

Personal intelligence agent: multi-source collectors → dedup → ranking →
daily Telegram briefing → feedback loop.

## Status

POC implemented (Phases 0-3, 6-11 of the plan):

- ✅ Go skeleton: config loader, structured JSON logger, scheduler (cron-like)
- ✅ PostgreSQL schema (10 tables) + migration runner with advisory lock
- ✅ RSS/Atom collector: ETag + Last-Modified conditional GET, per-feed isolation
- ✅ Reddit collector: OAuth client-credentials, hot/new/rising/top listings
- ✅ GitHub collector: releases + org new repos, read-only PAT
- ✅ Unified `Item` model (all collectors produce the same struct)
- ✅ Dedup level 1: unique (source, source_id) + content hash + canonical URL
- ✅ Ranking: deterministic heuristic scorer (relevance/importance/novelty/actionability)
- ✅ Personalization: topic/source/author weights adjusted by feedback
- ✅ Briefing: daily markdown (Telegram-compatible), trend clustering
- ✅ Telegram: commands /today /save /ignore /more /less /sources /status + 👍👎🔥📌
- ✅ Obsidian: /save writes notes to vault
- ⏳ Planned: X connector, LinkedIn adapters, LLM ranking stage (MiniMax M3), deep dive

See `docs/plans/2026-08-25-poc.md` for the full plan.

## Usage

```sh
make build            # build ./bin/radar
make test             # run unit tests
make docker-up        # docker compose: postgres + radar (needs .env)
```

Local development (standalone postgres on port 5499):

```sh
docker run -d --name radar-postgres -p 5499:5432 \
  -e POSTGRES_USER=radar -e POSTGRES_PASSWORD=radar -e POSTGRES_DB=radar \
  postgres:16-alpine
./scripts/run-local.sh migrate
./scripts/run-local.sh collect
./scripts/run-local.sh rank
./scripts/run-local.sh briefing
```

CLI:

```sh
radar migrate   # apply migrations
radar collect   # one collection cycle across enabled collectors
radar rank      # score pending items
radar briefing  # generate (and send) the daily briefing
radar run       # scheduler (20 min collect + daily 07:00 briefing) + telegram listener
```

## Configuration

- `config/radar.yaml` — sources, topics, briefing schedule, models
- Environment (see `config/env.example`): `TELEGRAM_BOT_TOKEN`,
  `TELEGRAM_CHAT_ID`, `REDDIT_CLIENT_ID`, `REDDIT_CLIENT_SECRET`,
  `GITHUB_TOKEN`, `OPENAI_API_KEY`, `RADAR_DB_*`

## Architecture

```
collectors (rss, reddit, github)  →  Item  →  store (PostgreSQL)
                                                ↓
                                   ranking (heuristic + personalization)
                                                ↓
                                   briefing (selection + trends)
                                                ↓
                                   telegram (delivery + feedback)
                                                ↓
                                   personalization (weights adjust)
```

Collector ≠ Intelligence: collectors only collect, the pipeline normalizes,
the ranking layer understands, personalization learns, Telegram interacts.
