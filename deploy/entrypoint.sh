#!/bin/sh
# Supervisor for the radar container: runs `radar run` (scheduler + telegram)
# and `radar web` (bookmark dashboard) in parallel. Each child is its own
# process group; we wait on both and exit non-zero if either dies.
set -eu

CONFIG="${RADAR_CONFIG:-config/radar.yaml}"

echo "[entrypoint] starting radar run (scheduler + telegram)"
radar run -config "$CONFIG" &
RUN_PID=$!

echo "[entrypoint] starting radar web (bookmark dashboard on 127.0.0.1:8081)"
radar web -config "$CONFIG" &
WEB_PID=$!

# Trap signals and forward them to both children, then wait.
trap 'echo "[entrypoint] stopping"; kill -TERM "$RUN_PID" "$WEB_PID" 2>/dev/null || true; wait' TERM INT

# Wait on the first child to exit, then propagate. The other child will be
# killed by the trap on the next signal, or by the orchestrator (Docker)
# sending TERM to the whole container.
wait -n "$RUN_PID" "$WEB_PID"
EXIT=$?
echo "[entrypoint] one of the children exited with $EXIT, shutting down the other"
kill -TERM "$RUN_PID" "$WEB_PID" 2>/dev/null || true
wait "$RUN_PID" "$WEB_PID" 2>/dev/null || true
exit "$EXIT"
