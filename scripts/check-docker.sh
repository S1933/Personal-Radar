#!/bin/bash
cd /home/pi/Projects/personal-radar
docker compose -f deploy/docker-compose.yml ps
echo "=== radar container status ==="
docker inspect -f '{{.State.Running}} {{.State.Health.Status}}' deploy-radar-1 2>/dev/null
echo "=== recent radar logs (warnings/errors only) ==="
docker logs deploy-radar-1 2>&1 | grep -E '"level":"(warn|error)"' | tail -15
