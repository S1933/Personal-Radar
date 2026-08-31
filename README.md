# Personal Radar

Multi-source AI-powered personal veille agent. Collects from RSS / GitHub /
Reddit / LinkedIn / X (Twitter), deduplicates, ranks with an LLM, synthesizes a
daily "why it matters" briefing, and delivers it to Telegram. Supports
`/deepdive <id>` (structured LLM analysis) and `/save` (to an Obsidian vault).

## Architecture

```
                          ┌─────────────────────────────────────────┐
   sources (RSS, GH,     │                 radar (Go)                │
   Reddit, LinkedIn,  ──▶│  collectors ─▶ ingest ─▶ dedup ─▶ rank    │
   X via twscrape)       │                          (LLM)  │         │
                          │                                  ▼         │
                          │                    personalization        │
                          │                          │                │
                          │                  briefing (LLM synth)     │
                          │                          │                │
                          │                  telegram (delivery)      │
                          │                          │                │
                          └──────────────────────────┼────────────────┘
                                                     ▼
                                            PostgreSQL (items, prefs)
                                            Obsidian vault (/save)

   X is special: the Go collector shells out to a Python sidecar
   (xscraper/collect.py) that uses twscrape with an authenticated X session.
```

**Key principle: Connector ≠ Intelligence.** Every collector emits a unified
`model.Item`; all scoring, filtering, synthesis and delivery live downstream.
See `docs/adr/0001-collector-intelligence-separation.md`.

## Sources

| Source      | Method                                   | Auth                         | Status        |
|-------------|------------------------------------------|------------------------------|---------------|
| RSS         | native feed parse (conditional GET)      | none                         | ✅ stable      |
| GitHub      | REST API (repos + orgs)                  | `GITHUB_TOKEN`               | ✅ stable      |
| Reddit      | public RSS (`/r/<sub>/search`)           | none (best-effort, 429)      | ✅ best-effort |
| LinkedIn    | public company-page HTML                | none (JS-gated → 0 items)    | ⚠️ best-effort |
| X (Twitter) | twscrape GraphQL (Python sidecar)       | `X_AUTH_TOKEN` + `X_CT0`     | ✅ via session |

## Prerequisites

- Go 1.25+
- Docker + Docker Compose v2
- Python 3.12+ (only for the X sidecar; bundled in the image)
- An X account session (cookies) for the X collector — see below

## Configuration

Copy `.env.example` to `.env` and fill in:

```
TELEGRAM_BOT_TOKEN=...        # @PersoRadarBot token
TELEGRAM_CHAT_ID=...          # your chat id
OPENAI_API_KEY=...            # OpenAI-compatible key (OpenCode Go endpoint)
GITHUB_TOKEN=...
REDDIT_CLIENT_ID=             # optional (public mode if empty)
REDDIT_CLIENT_SECRET=
X_AUTH_TOKEN=...              # X session cookie (auth_token)
X_CT0=...                     # X session cookie (ct0)

# Database — RADAR_DATABASE_URL wins when set; otherwise the per-field
# vars below compose the DSN at boot. Passwords with special chars
# are URL-encoded automatically.
RADAR_DB_HOST=db
RADAR_DB_PORT=5432
RADAR_DB_USER=radar
RADAR_DB_PASSWORD=...
RADAR_DB_NAME=radar
```

`config/radar.yaml` holds non-secret tuning: sources, schedules, model names,
Obsidian vault path.

### X session cookies

twscrape requires an authenticated X session. Export cookies from your browser:

1. Log in to x.com
2. DevTools (F12) → Application → Cookies → x.com
3. Copy `auth_token` and `ct0` into `.env`

These are equivalent to a password — never commit them.

## Run (Docker, production)

```bash
set -a && . ./.env && set +a
docker compose -f deploy/docker-compose.yml build --no-cache
docker compose -f deploy/docker-compose.yml up -d
docker logs -f deploy-radar-1
```

The stack runs collection every 20 min and a briefing at 07:00 (Europe/Paris).

## Run (local dev)

```bash
# Go service
go run ./cmd/radar run -config config/radar.yaml

# Cross-compile every platform the radar runs on
make build-all && ls dist/

# Local-CI (vet + short tests + build-all)
make ci

# X sidecar only (needs venv with twscrape)
python3 -m venv .venv && . .venv/bin/activate
pip install twscrape
export X_AUTH_TOKEN=... X_CT0=... TWSCRAPE_DB=.venv/x_accounts.db
python3 xscraper/collect.py --accounts openai --limit 5
```

## Telegram commands (on @PersoRadarBot)

- `/briefing` — force a briefing now (alias: `/today`, `/start`, `/help`)
- `/deepdive <id>` — LLM-structured analysis of item `<id>`
- `/save <id>` — save item `<id>` to the Obsidian vault
  (`Daily Radar/<date>/item-<id>.md`)
- ❤️ / 📌 on dashboard cards — like / pin in the radar (Trello-style)

## Tests & CI

```bash
go build ./...               # fast
go vet ./...
go test -short ./internal/...  # CI default
go test ./internal/...         # full suite (real network)
python3 xscraper/collect.py --accounts openai --limit 2   # manual X check
```

CI runs on every push and PR via `.github/workflows/ci.yml`:
the **test** job vets and runs `-short -race` on `ubuntu-latest`; the
**build** job cross-compiles `linux/amd64`, `linux/arm64`, `darwin/amd64`
and uploads the binaries as workflow artifacts. Concurrency cancels
in-flight runs of the same branch.

## Reliability features

- **Per-collector budget** (5 min) bounds any single source — a stuck
  X sidecar cannot freeze the cycle
- **Scheduler panic recovery** — a job that panics logs the stack and
  continues, the Docker restart loop stays silent
- **Conditional GET on RSS** — ETag / Last-Modified persisted in
  `feed_state`; the 9 feeds go from ~650 req/day to ~65 req/day
- **Cross-source dedup** — same story arriving via RSS, X and Reddit
  is merged via canonical URL + content hash (≈ 30% fewer items)
- **Source-quota cap** — no single source can crowd out the rest of
  the briefing
- **Run telemetry** — every collect and briefing run lands in the
  `runs` table for grep-friendly observability

## Security

- **CSRF** — every mutating web endpoint requires the `X-Radar-Request: 1`
  header (rejects cross-origin form posts)
- **Telegram bot gate** — only the configured `TELEGRAM_CHAT_ID` is
  allowed to issue commands; messages from other chats are logged and
  dropped
- **Secrets** — `.env` is gitignored (chmod 600), `RADAR_DB_PASSWORD`
  is URL-encoded at the DSN boundary, X cookies rotate by re-export


## Layout

```
cmd/radar            entrypoint
internal/
  collectors/        rss, github, reddit, linkedin, x  (each → model.Item)
  store/             PostgreSQL persistence + dedup (FindDuplicate)
  ranking/           BM25 heuristic + LLM scorer
  briefing/          LLM synthesis ("why it matters") + source quota
  ingestion/         cross-source dedup pipeline
  personalization/   user interest profile
  telegram/          delivery + command handling + bot gate
  scheduler/         jobs with panic recovery
  app/               wiring, handlers (/deepdive, /save)
  textutil/          shared rune-safe truncate + word boundary
  logging/           structured logger
  summary/           dashboard "summary" endpoint
xscraper/            Python twscrape sidecar for X
deploy/              docker-compose.yml
.github/workflows/   ci.yml (vet + test + cross-compile)
config/radar.yaml    runtime config
docs/adr/            architecture decision records
```
