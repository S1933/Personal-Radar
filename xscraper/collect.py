#!/usr/bin/env python3
"""X/Twitter collector using twscrape (GraphQL, account-session based).

Reads accounts from the twscrape SQLite DB (TWSCRAPE_DB env, default
x_accounts.db). Requires at least one active account added via:

    twscrape add_cookie <name>        # paste auth_token + ct0 from x.com

Then collects recent tweets for the given accounts / queries and prints
a JSON array of normalized items to stdout.

Usage:
    collect.py --accounts openai anthropicai --queries "coding agent"
    collect.py --accounts openai --limit 10
"""
import argparse
import asyncio
import json
import os
import sys

from twscrape import API, gather


async def collect(accounts, queries, limit):
    db = os.environ.get("TWSCRAPE_DB", "x_accounts.db")
    api = API(db)

    try:
        infos = await api.pool.accounts_info()
    except Exception:  # noqa: BLE001
        infos = []
    active = sum(1 for a in infos if getattr(a, "active", False) or getattr(a, "status", "") == "active")
    if active == 0:
        print(json.dumps({"error": "no_active_accounts",
                          "hint": "run: twscrape add_cookie <name>"}),
              file=sys.stderr)
        return []

    out = []
    seen = set()

    for handle in accounts:
        handle = handle.lstrip("@")
        try:
            user = await api.user_by_login(handle)
        except Exception as e:  # noqa: BLE001
            print(json.dumps({"error": "user_by_login", "handle": handle,
                              "detail": str(e)}), file=sys.stderr)
            continue
        if user is None:
            print(json.dumps({"error": "user_not_found", "handle": handle}),
                  file=sys.stderr)
            continue
        tweets = await gather(api.user_tweets(user.id, limit=limit))
        for t in tweets:
            out.append(_item(t, handle))

    for q in queries:
        try:
            tweets = await gather(api.search(q, limit=limit))
        except Exception as e:  # noqa: BLE001
            print(json.dumps({"error": "search", "query": q,
                              "detail": str(e)}), file=sys.stderr)
            continue
        for t in tweets:
            out.append(_item(t, t.author))

    # dedupe by tweet id
    uniq = []
    for it in out:
        if it["source_id"] in seen:
            continue
        seen.add(it["source_id"])
        uniq.append(it)
    return uniq


def _item(t, handle):
    text = t.rawContent or t.text or ""
    author = handle or (t.author.username if t.author else "unknown")
    return {
        "source": "x",
        "source_id": "x:" + str(t.id),
        "author": author,
        "title": (text.split("\n", 1)[0])[:160],
        "url": f"https://twitter.com/{author}/status/{t.id}",
        "content": text,
        "published_at": t.date.isoformat() if t.date else None,
        "collected_at": None,
        "topics": ["software-engineering", "open-source"],
        "language": t.lang or "en",
        "metadata": {"mode": "twscrape", "likes": t.likeCount,
                     "retweets": t.retweetCount},
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--accounts", nargs="*", default=[])
    ap.add_argument("--queries", nargs="*", default=[])
    ap.add_argument("--limit", type=int, default=10)
    args = ap.parse_args()

    items = asyncio.run(collect(args.accounts, args.queries, args.limit))
    print(json.dumps(items, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
