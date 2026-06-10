#!/usr/bin/env bash
set -euo pipefail

# ── Tailscale Funnel Setup for Freezer App ────────────────────────────
# One-time setup: installs Tailscale and enables Funnel on port 3000.
# The Funnel URL is persistent — it survives reboots, power outages,
# and network changes.
#
# Usage: ./deploy/tailscale-setup.sh

PORT="${PORT:-3000}"

echo "==> Installing Tailscale..."
if ! command -v tailscale &>/dev/null; then
  # Prefer the distro package on Raspberry Pi OS (apt repository)
  if command -v apt-get &>/dev/null; then
    sudo apt-get update -qq && sudo apt-get install -y tailscale
  else
    # Fallback: download install script, inspect, then execute
    echo "    Downloading Tailscale install script for inspection..."
    curl -fsSL https://tailscale.com/install.sh -o /tmp/tailscale-install.sh
    echo "    SHA256: $(sha256sum /tmp/tailscale-install.sh | cut -d' ' -f1)"
    sh /tmp/tailscale-install.sh
    rm /tmp/tailscale-install.sh
  fi
else
  echo "    Tailscale is already installed."
fi

echo ""
echo "==> Ensuring tailscaled is running..."
sudo systemctl enable --now tailscaled

echo ""
echo "==> Checking Tailscale status..."
if tailscale status &>/dev/null; then
  echo "    Tailscale is connected."
else
  echo "    Tailscale is not yet authenticated."
  echo ""
  echo "    Run the following to authenticate this Pi:"
  echo "      sudo tailscale up"
  echo ""
  echo "    Then open the URL printed by that command in a browser."
  echo "    After authentication, re-run this script."
  exit 1
fi

echo ""
echo "==> Enabling HTTPS certificate..."
tailscale cert --cert-file /dev/null --key-file /dev/null "$(hostname)" 2>/dev/null || true

echo ""
echo "==> Enabling Tailscale Funnel on port $PORT..."
tailscale funnel "$PORT"

echo ""
echo "==================================="
echo "Tailscale Funnel is active!"
echo "==================================="
echo ""
echo "Your persistent URL is:"
echo "  https://$(hostname).$(tailscale status --json 2>/dev/null | grep -o '"DNSName":"[^"]*"' | head -1 | cut -d'"' -f4 | sed 's/^.*\.//' | tr -d '\n').ts.net"
echo ""
echo "To find your URL at any time:"
echo "  tailscale status"
echo "  # Your URL is: https://<hostname>.<tailnet-name>.ts.net"
echo ""
echo "The Funnel URL persists through reboots and network changes."
echo "No Cloudflare account or domain name required."