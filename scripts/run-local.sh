#!/bin/bash
# Local end-to-end test against the radar-postgres container.
set -e
cd /home/pi/Projects/personal-radar
export RADAR_DB_HOST=localhost RADAR_DB_PORT=5499 RADAR_DB_USER=radar RADAR_DB_PASSWORD=radar RADAR_DB_NAME=radar
export GITHUB_TOKEN="$(cat /home/pi/ghtoken.txt | head -1 | tr -d '[:space:]')"

CMD="$1"
shift || true
case "$CMD" in
  migrate) ./bin/radar migrate -config config/radar.yaml ;;
  collect) ./bin/radar collect -config config/radar.yaml ;;
  rank)    ./bin/radar rank -config config/radar.yaml ;;
  briefing) ./bin/radar briefing -config config/radar.yaml ;;
  tables)  docker exec radar-postgres psql -U radar -d radar -c '\dt' ;;
  counts)  docker exec radar-postgres psql -U radar -d radar -c 'SELECT source, count(*) FROM items GROUP BY source; SELECT count(*) AS scored FROM scores; SELECT action, count(*) FROM feedback GROUP BY action;' ;;
  *) echo "usage: run-local.sh {migrate|collect|rank|briefing|tables|counts}" ;;
esac
