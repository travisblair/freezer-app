#!/usr/bin/env bash
set -euo pipefail

# ── Freezer App Build Script (cross-compile for Pi Zero W) ────────────
# Builds the Go backend for ARMv6 (Pi Zero W) and the frontend static
# files, then packages everything into deploy/release.tar.gz.
#
# Usage: ./deploy/build.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RELEASE_FILE="$SCRIPT_DIR/release.tar.gz"

echo "==> Building Go backend for Pi Zero W (linux/arm/ARMv6)..."
cd "$PROJECT_DIR/gobackend"
GOOS=linux GOARCH=arm GOARM=6 go build -o freezer-server .

# Verify the binary architecture
echo "    Binary type: $(file freezer-server | cut -d: -f2-)"

echo ""
echo "==> Building frontend (SolidJS + Vite)..."
cd "$PROJECT_DIR/frontend"
npm run build

echo ""
echo "==> Packaging release tarball..."
cd "$PROJECT_DIR"
tar -czf "$RELEASE_FILE" \
  gobackend/freezer-server \
  frontend/dist \
  prod-start.sh \
  .env.example \
  deploy/tailscale-setup.sh \
  deploy/hardware-watchdog-setup.sh \
  deploy/freezer-app.service \
  deploy/tailscale-funnel.service

echo ""
echo "==================================="
echo "Release built: $RELEASE_FILE"
echo "Size: $(du -h "$RELEASE_FILE" | cut -f1)"
echo "==================================="
echo ""
echo "Next: ./deploy/deploy.sh pi@<pi-hostname>"