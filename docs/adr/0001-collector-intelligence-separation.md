# ADR 0001: Collector / Intelligence separation + X via twscrape

- **Status:** Accepted
- **Date:** 2026-08-26

## Context

Personal Radar aggregates heterogeneous sources (RSS, GitHub, Reddit, LinkedIn,
X) into a single ranked briefing. Each source has wildly different access
mechanics:

- RSS / GitHub: clean, documented, key-or-anonymous APIs.
- Reddit / LinkedIn: usable without OAuth via public endpoints, but rate-limited
  or JS-gated (best-effort).
- X: the paid API tier (Basic ~$200/mo) is required for any programmatic read;
  the free/public web is behind a login wall.

We needed an architecture that (a) keeps source-specific mechanics isolated,
(b) lets new sources be added without touching the scoring/delivery pipeline,
and (c) accommodates X without paying for the API.

## Decision

### 1. Connector ≠ Intelligence

Every collector implements a single `Collector` interface returning
`[]model.Item` — a unified, source-agnostic struct (source, source_id, author,
title, url, content, published_at, topics, language, metadata). All downstream
stages (dedup, ranking, personalization, synthesis, delivery) operate **only** on
`model.Item` and know nothing about where an item came from.

Consequences:
- Adding a source = adding one package under `internal/collectors/*`.
- LLM stages (rank, synthesize, deepdive) are source-blind and reusable.
- Source outages (e.g. Reddit 429, LinkedIn 0 items) degrade gracefully — the
  pipeline keeps running on the others.

### 2. X access via twscrape sidecar (not the paid API)

X scraping uses [`twscrape`](https://github.com/vladkens/twscrape), a Python
library that drives X's internal GraphQL endpoints using an **authenticated X
session** (browser cookies `auth_token` + `ct0`). This bypasses the paid API
tier entirely.

The Go service does **not** embed Python. It shells out to a small sidecar
(`xscraper/collect.py`) as a subprocess, passing accounts/queries as CLI args
and reading normalized JSON on stdout. The session cookies are injected from
env (`X_AUTH_TOKEN`, `X_CT0`).

Consequences:
- If X changes its GraphQL schema, only the Python sidecar breaks — the Go
  service keeps running and reports a clean collector error.
- The X session is a credential equivalent to a password; it lives in `.env`
  (chmod 600, gitignored) and is rotated by re-exporting the cookies.
- ToS note: X discourages multi-account scraping. Single-session use is at the
  operator's discretion.

## Alternatives considered

| Option | Verdict |
|--------|---------|
| X paid API (Basic) | Rejected — ~$200/mo, still no user-timeline on Basic tier |
| X public web scrape (Go) | Rejected — login wall, no anonymous access |
| X unofficial Go library | Rejected — twscrape (Python) is the maintained reference impl |
| Embed twscrape in Go via CGO | Rejected — subprocess sidecar is simpler and isolates failures |

## Result

The POC runs in production on a Raspberry Pi with 5 sources + LLM ranking,
synthesis, deepdive, and Obsidian save. X delivers ~40 items/cycle via the
twscrape sidecar at zero API cost.
