#!/usr/bin/env bash
# Build and start the DB-free signaling relay, run the WebRTC interop harness
# against it, then tear everything down. Exit code is the harness result.
#
#   ./run.sh
#
# Requires: Go (to build the relay), Node + npm install already run.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
PORT="${PORT:-8081}"
BIN="$(mktemp -d)/signaling-dev"

cleanup() {
  [[ -n "${SIG_PID:-}" ]] && kill "$SIG_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "[run] building signaling-dev"
( cd "$ROOT/backend" && go build -o "$BIN" ./cmd/signaling-dev )

echo "[run] starting signaling relay on :$PORT"
HTTP_ADDR=":$PORT" "$BIN" >/tmp/signaling-dev.log 2>&1 &
SIG_PID=$!

# Wait for health.
for _ in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.1
done

echo "[run] running negotiation + data-channel harness"
SIGNALING_URL="ws://127.0.0.1:$PORT" node "$HERE/src/harness.js"

echo "[run] running media-flow harness"
SIGNALING_URL="ws://127.0.0.1:$PORT" node "$HERE/src/media-harness.js"
