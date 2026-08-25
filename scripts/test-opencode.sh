#!/bin/bash
# Test OpenCode Go API key loaded from .env
cd /home/pi/Projects/personal-radar
set -a; source .env; set +a
[ -z "$OPENAI_API_KEY" ] && { echo "NO KEY IN .env"; exit 1; }
echo "Key present: sk-...${OPENAI_API_KEY: -4}"
echo "BaseURL: ${OPENAI_BASE_URL:-https://api.opencode.ai/v1}"
echo "=== testing chat completion (deepseek-v4-flash) ==="
curl -s -m 30 -X POST "${OPENAI_BASE_URL:-https://api.opencode.ai/v1}/chat/completions" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"ping"}],"max_tokens":16}' \
  | head -c 800
echo
