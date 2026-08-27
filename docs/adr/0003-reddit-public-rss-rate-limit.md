# ADR 0003: Reddit collector — anonymous RSS rate-limited on Pi IP

- **Status:** Accepted (throttle + identified UA in place; full recovery needs OAuth)
- **Date:** 2026-08-27

## Context

Personal Radar watches 17 subreddits via the native Reddit RSS endpoint
(`https://www.reddit.com/r/<sub>/.rss`) using the public (no-credential)
adapter, because no Reddit OAuth app was configured (`REDDIT_CLIENT_ID` /
`REDDIT_CLIENT_SECRET` empty in `.env`).

## Decision

Keep the public RSS adapter (user choice "B") with two mitigations:

1. **Dedicated 60m poll interval** (`reddit.every: 60m`) — Reddit runs on its
   own scheduler job, decoupled from the 20m global collect cycle, so we never
   pile Reddit traffic on top of RSS/X/GitHub.
2. **2.5s throttle between subreddits** (`redditThrottle` in `public.go`) plus
   an **identified User-Agent** (`by /u/jenue1933; contact: …@gmail.com`) to
   stay within Reddit's acceptable-use policy.

A watchdog cron (`reddit_ban_check.sh`, every 30m) alerts on Telegram if the
ban ever lifts.

## Observed reality (measured 2026-08-27)

| Test | Result |
|---|---|
| 1 sub, isolated | HTTP 200 ✅ |
| 17 subs, 2.5s/sub | 15–17 × HTTP 429 ❌ |
| 17 subs, 5s/sub | 13 × HTTP 429 ❌ |

Reddit's anonymous RSS edge enforces a **per-IP quota of ~1–2 requests per
window** from this Pi IP. A single request succeeds; any burst of N subs is
throttled after the first 1–2. **The throttle cannot fix this** — the quota is
already exhausted at 1–2 requests, far below 17 subs. The limit is structural,
not a transient temporary ban (an isolated request still returns 200).

## Consequences

- With 17 subreddits, only ~1–2 subs per 60m cycle actually return items; the
  rest are dropped with a `429` warning (best-effort, non-fatal — other
  collectors are unaffected).
- If the Pi IP quota ever relaxes, the collector resumes automatically.
- **Full recovery requires OAuth**: a Reddit *script* app yields a
  `REDDIT_CLIENT_ID` + `REDDIT_CLIENT_SECRET` and switches the collector to
  `oauth.reddit.com` (quota ~60 req/min, separate from the anonymous edge).
  The code already supports this — `reddit.NewCollector` uses OAuth when
  credentials are present, falling back to public only when they are empty.
  Filling the two env vars in `.env` is the only change needed (no rebuild).

## Alternatives considered

- Reduce to 1–2 subreddits: rejected — defeats the purpose of watching 17.
- Switch to OAuth now: user deferred (choice "B"); documented here as the
  definitive fix when desired.
