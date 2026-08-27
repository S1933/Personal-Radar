#!/usr/bin/env bash
# Check if Reddit anonymous-RSS ban has lifted on this Pi IP.
# Exits 0 + prints "OK" when reachable, exits 1 (silent) when still 429.
UA="PersonalRadar/0.1 (by /u/jenue1933; contact: jeanphilippenuel@gmail.com; +https://github.com/S1933/personal-radar)"
code=$(curl -s -m 20 -A "$UA" -o /dev/null -w "%{http_code}" "https://www.reddit.com/r/golang/.rss?limit=5" 2>/dev/null)
if [ "$code" = "200" ]; then
  echo "Reddit RSS ban lifted (HTTP 200) — Reddit collection should resume."
  exit 0
fi
# silent otherwise (watchdog pattern)
exit 1
