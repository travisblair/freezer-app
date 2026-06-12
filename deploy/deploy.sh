#!/usr/bin/env bash
set -euo pipefail

# ── Freezer App Deploy Script ─────────────────────────────────────────
# Builds, uploads, and installs on the Pi with a single command.
# Usage: ./deploy/deploy.sh [pi-address]
#   Default: admin@freezer-app.local

PI_ADDR="${1:-admin@freezer-app.local}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RELEASE_FILE="$SCRIPT_DIR/release.tar.gz"

echo "==> Building..."
"$SCRIPT_DIR/build.sh"

echo ""
echo "==> Uploading to $PI_ADDR..."
scp "$RELEASE_FILE" "$PI_ADDR:~"

echo ""
echo "==> Installing on Pi..."
ssh -t "$PI_ADDR" "
  set -e
  cd ~/freezer-app
  tar -xzf ~/release.tar.gz
  rm ~/release.tar.gz
  chmod +x gobackend/freezer-server prod-start.sh
  sudo cp deploy/freezer-app.service /etc/systemd/system/
  sudo systemctl daemon-reload
  sudo systemctl restart freezer-app
  sleep 2
  systemctl status freezer-app --no-pager -n 3
"

echo ""
echo "==> Done."
