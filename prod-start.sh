#!/usr/bin/env bash
set -euo pipefail

# ── Freezer App Production Start ──────────────────────────────────────
# Builds the frontend, compiles the Go backend, and starts the server.
# Usage: ./prod-start.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load .env if present (space-safe: parses KEY=value lines without bash word-splitting)
if [ -f "$SCRIPT_DIR/.env" ]; then
  while IFS='=' read -r key value; do
    # Skip blank lines and comments
    [ -z "$key" ] && continue
    [[ "$key" =~ ^[[:space:]]*# ]] && continue
    # Trim leading/trailing whitespace from key
    key=$(echo "$key" | xargs)
    # Export the variable
    export "$key"="$value"
  done < "$SCRIPT_DIR/.env"
fi

echo "==> Building frontend..."
cd "$SCRIPT_DIR/frontend"
if [ -d dist ]; then
  echo "    dist/ already exists, skipping build. (Use deploy/build.sh on Mac for Pi builds)"
else
  npm run build
fi

echo "==> Building Go backend..."
cd "$SCRIPT_DIR/gobackend"
if [ -f freezer-server ]; then
  echo "    Binary already exists, skipping compilation."
else
  go build -o freezer-server .
fi

echo "==> Starting server on port ${PORT:-3000}..."
cd "$SCRIPT_DIR"
export PORT="${PORT:-3000}"

if [[ "$(uname -s)" == "Darwin" ]]; then
  # macOS: keep the shell wrapper so caffeinate can prevent sleep
  exec caffeinate -i ./gobackend/freezer-server
else
  # Linux/Pi: exec replaces this shell so the Go binary is PID 1.
  # Required for systemd Type=notify — the Go binary sends WATCHDOG=1.
  exec ./gobackend/freezer-server
fi
