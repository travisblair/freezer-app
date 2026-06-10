#!/usr/bin/env bash
set -euo pipefail

# ── Freezer App Deploy Script (SCP to Pi Zero W) ─────────────────────
# Builds the release tarball (if not already built), copies it to the
# Pi, and runs the remote install steps.
#
# Usage: ./deploy/deploy.sh <pi-address>
#   e.g. ./deploy/deploy.sh pi@freezer-app.local
#   e.g. ./deploy/deploy.sh pi@192.168.1.42

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELEASE_FILE="$SCRIPT_DIR/release.tar.gz"

if [ $# -lt 1 ]; then
  echo "Usage: $0 <pi-address>"
  echo "  e.g. $0 pi@freezer-app.local"
  echo "  e.g. $0 pi@192.168.1.42"
  exit 1
fi

PI_ADDR="$1"

# ── Step 1: Build ─────────────────────────────────────────────────────
if [ ! -f "$RELEASE_FILE" ]; then
  echo "Release tarball not found. Building..."
  "$SCRIPT_DIR/build.sh"
fi

# ── Step 2: Copy to Pi ────────────────────────────────────────────────
echo "==> Copying release to $PI_ADDR..."
scp "$RELEASE_FILE" "$PI_ADDR:~"
scp "$SCRIPT_DIR/.env.example" "$PI_ADDR:~/.env.example" 2>/dev/null || true

# ── Step 3: Remote install ────────────────────────────────────────────
echo ""
echo "==> Installing on Pi..."
ssh "$PI_ADDR" << 'ENDSSH'
set -euo pipefail

APP_DIR="/home/admin/freezer-app"

# Create app directory structure
mkdir -p "$APP_DIR"
cd "$APP_DIR"

# Extract the release — creates gobackend/freezer-server and frontend/dist/
if [ -f ~/release.tar.gz ]; then
  echo "    Extracting release..."
  tar -xzf ~/release.tar.gz
  rm ~/release.tar.gz
fi

# Make the binary executable
chmod +x gobackend/freezer-server

# Make prod-start.sh executable
if [ -f prod-start.sh ]; then
  chmod +x prod-start.sh
fi

# Create data directory for SQLite
mkdir -p data

# Set up .env from example if not already present
if [ ! -f .env ] && [ -f .env.example ]; then
   echo "    No .env found. Creating from .env.example..."
   cp .env.example .env
   chmod 600 .env
   echo ""
   echo "==================================="
   echo "!! IMPORTANT: Add users to the DB !! "
   echo "==================================="
   echo "Generate password hashes:"
   echo "  ./freezer-server --hash-password"
   echo "Then insert users:"
   echo "  sqlite3 data/freezer.db \"INSERT INTO users (email, password_hash) VALUES ('you@email.com', 'HASH');\""
   echo ""
fi

echo ""
echo "==================================="
echo "Deployment complete!"
echo "==================================="
echo ""
echo "Next steps on the Pi:"
echo "  1. Edit .env:      nano ~/freezer-app/.env"
echo "  2. Add users:"
echo "     ./freezer-server --hash-password"
echo "     sqlite3 data/freezer.db \"INSERT INTO users (email, password_hash) VALUES ('you@email.com', 'HASH');\""
echo "  3. Start server:   cd ~/freezer-app && ./prod-start.sh"
echo "  4. Set up Tailscale: ~/freezer-app/deploy/tailscale-setup.sh"
echo ""
echo "Tailscale Funnel gives you a persistent HTTPS URL (survives reboots)."
echo ""
echo "For auto-start on boot:"
echo "  sudo cp ~/freezer-app/deploy/freezer-app.service /etc/systemd/system/"
echo "  sudo systemctl daemon-reload"
echo "  sudo systemctl enable --now freezer-app"
echo ""
echo "For crash protection (hardware watchdog + systemd watchdog):"
echo "  sudo ~/freezer-app/deploy/hardware-watchdog-setup.sh"
echo "  sudo reboot"
echo ""
echo "(tailscaled is already auto-started by the Tailscale package.)"
ENDSSH