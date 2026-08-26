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
| RSS         | native feed parse                        | none                         | ✅ stable      |
| GitHub      | REST API (repos + orgs)                  | `GITHUB_TOKEN`               | ✅ stable      |
| Reddit      | public RSS (`/r/<sub>/search`)           | none (best-effort, 429)      | ✅ best-effort |
| LinkedIn    | public company-page HTML                | none (JS-gated → 0 items)    | ⚠️ best-effort |
| X (Twitter) | twscrape GraphQL (Python sidecar)       | `X_AUTH_TOKEN` + `X_CT0`     | ✅ via session |

## Prerequisites

- Go 1.24+
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

# X sidecar only (needs venv with twscrape)
python3 -m venv .venv && . .venv/bin/activate
pip install twscrape
export X_AUTH_TOKEN=... X_CT0=... TWSCRAPE_DB=.venv/x_accounts.db
python3 xscraper/collect.py --accounts openai --limit 5
```

## Telegram commands (on @PersoRadarBot)

- `/briefing` — force a briefing now
- `/deepdive <id>` — LLM-structured analysis of item `<id>`
- `/save <id>` — save item `<id>` to the Obsidian vault
  (`Daily Radar/<date>/item-<id>.md`)

## Tests

```bash
go build ./...
go vet ./...
go test ./internal/...                 # unit + real network tests (skip w/ -short)
python3 xscraper/collect.py --accounts openai --limit 2   # manual X check
```

## Layout

```
cmd/radar            entrypoint
internal/
  collectors/        rss, github, reddit, linkedin, x  (each → model.Item)
  store/             PostgreSQL persistence + dedup
  ranking/           heuristic + LLM scorer
  briefing/          LLM synthesis ("why it matters")
  personalization/   user interest profile
  telegram/          delivery + command handling
  app/               wiring, handlers (/deepdive, /save)
xscraper/            Python twscrape sidecar for X
deploy/              Dockerfile + docker-compose.yml
config/radar.yaml    runtime config
docs/adr/            architecture decision records
```
