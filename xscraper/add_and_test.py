#!/usr/bin/env python3
"""Add X session cookies from env and verify the account is active."""
import asyncio
import json
import os
import sys

from twscrape import API


async def main():
    db = os.environ.get("TWSCRAPE_DB", "x_accounts.db")
    auth = os.environ.get("X_AUTH_TOKEN")
    ct0 = os.environ.get("X_CT0")
    if not auth or not ct0:
        print(json.dumps({"error": "missing X_AUTH_TOKEN or X_CT0 env"}))
        return

    api = API(db)
    # add_account_cookies(name, cookies: str | dict)
    cookie = f"auth_token={auth}; ct0={ct0}"
    try:
        await api.pool.add_account_cookies("radar_session", cookie)
        print(json.dumps({"ok": True, "msg": "cookie added"}))
    except Exception as e:  # noqa: BLE001
        print(json.dumps({"error": "add_cookie", "detail": str(e)}))
        return

    # verify by trying a single user lookup
    try:
        user = await api.user_by_login("openai")
        if user is None:
            print(json.dumps({"error": "user_not_found", "hint": "session likely invalid/expired"}))
            return
        print(json.dumps({"ok": True, "user": user.username, "id": str(user.id)}))
        tweets = api.user_tweets(user.id, limit=3)
        count = 0
        async for t in tweets:
            count += 1
        print(json.dumps({"ok": True, "tweets_fetched": count}))
    except Exception as e:  # noqa: BLE001
        print(json.dumps({"error": "verify", "detail": str(e)}))


if __name__ == "__main__":
    asyncio.run(main())
