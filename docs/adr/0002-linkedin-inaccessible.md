# ADR 0002: LinkedIn collector — inaccessible, désactivé

- **Status:** Accepted (superseded by X + RSS coverage)
- **Date:** 2026-08-27

## Context

Personal Radar must watch company pages (OpenAI, Anthropic) on LinkedIn.
The official LinkedIn API (`r_organization_social`) only exposes posts of
pages the authenticated member **administers** — it cannot read third-party
pages we merely follow. A session-cookie approach (same pattern as X/twscrape)
was therefore attempted with a real `li_at` + `JSESSIONID` session.

## Decision

**Disable the LinkedIn collector.** All three scraping strategies failed
against LinkedIn's current anti-bot posture:

| Strategy | Result | Detail |
|---|---|---|
| Static HTML scrape (company page) | ❌ | Posts are rendered client-side via JS/XHR; the server HTML contains only UI chrome + engagement counters (`urn:li:ugcPost:…`), never the post body. |
| Voyager internal API (`/voyager/api/...`) | ❌ | HTTP 400/403. Org-lookup by vanity returns 400; feed endpoint returns 403 CSRF even with correct `csrf-Token` header. Endpoint shape has changed/locked. |
| Playwright headless (chromium) | ❌ | Session valid (no login wall), but the feed never renders — 0 `<article>`/post nodes after scroll + networkidle. LinkedIn detects headless and withholds the feed. |

The session cookies were removed from `.env` immediately after testing
(security: no long-lived credential at rest).

## Consequences

- LinkedIn is **not** a source in the POC.
- The same orgs are already covered: **X** mirrors OpenAI/Anthropic posts
  (via twscrape session), and the **RSS** set includes Le Monde IA, Korben,
  Cloudflare, HN — sufficient AI/tech signal.
- If LinkedIn becomes a hard requirement later, viable options are:
  1. Official API with an **admin** relationship on the target page (rare).
  2. Residential proxy + full browser fingerprint (heavy, ToS-violating,
     high maintenance). Out of scope for this POC.

## Alternatives considered

- Keep the collector enabled as best-effort: rejected — it returns 0 items
  permanently and adds a dead code path + a stale credential at rest.
- Store session cookies for periodic refresh: rejected — ToS risk + no
  payoff given X/RSS coverage.
