#!/bin/bash
# Verify radar data + send a test briefing to Telegram.
cd /home/pi/Projects/personal-radar
export RADAR_DB_HOST=localhost RADAR_DB_PORT=5499 RADAR_DB_USER=radar RADAR_DB_PASSWORD=radar RADAR_DB_NAME=radar
echo "=== items per source (in docker postgres) ==="
docker exec deploy-postgres-1 psql -U radar -d radar -t -c "SELECT source, count(*) FROM items GROUP BY source ORDER BY 2 DESC;" 2>/dev/null || echo "psql direct failed"
echo "=== force a fresh collect + rank + briefing via local binary ==="
/home/pi/Projects/personal-radar/scripts/run-local.sh collect 2>&1 | grep -E "collected|error" | head
/home/pi/Projects/personal-radar/scripts/run-local.sh rank 2>&1 | grep -E "ranked|error" | head
echo "=== send test briefing to Telegram ==="
node -e '
const token = process.env.TG_TOKEN;
const chat = process.env.TG_CHAT;
const msg = encodeURIComponent("☀️ *DAILY RADAR* (test)\n\nRadar démarré et connecté à @PersoRadarBot.\nLa collecte tourne toutes les 20 min, le briefing quotidien à 07:00.\n\nRéponds /start ou 👍/👎/🔥/📌 pour tester.");
fetch(`https://api.telegram.org/bot${token}/sendMessage?chat_id=${chat}&parse_mode=Markdown&text=${msg}`).then(r=>r.json()).then(d=>console.log(JSON.stringify(d).slice(0,120))).catch(e=>console.error(e));
' 2>/dev/null || echo "node unavailable"
