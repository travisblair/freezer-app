#!/usr/bin/env bash
# ── Raspberry Pi Hardware Watchdog Setup ────────────────────────────────
# Idempotent: safe to run multiple times.
# Enables the BCM2708 hardware watchdog and configures systemd to use it.
#
# What this does:
#   1. Loads the bcm2835_wdt kernel module (Pi's built-in hardware watchdog)
#   2. Makes it persistent across reboots via /etc/modules
#   3. Enables dtparam=watchdog=on in /boot/config.txt
#   4. Configures RuntimeWatchdogSec=60s in /etc/systemd/system.conf
#      → If the kernel hangs for 60s, the Pi hard-reboots itself
#   5. Adds panic=60 to kernel cmdline
#      → If a kernel panic occurs, the Pi auto-reboots after 60s
#
# After running this, reboot the Pi for kernel cmdline changes to apply.
# Verify with: wdctl
#
# Usage: sudo ./deploy/hardware-watchdog-setup.sh

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: Must run as root (sudo)."
  exit 1
fi

echo "=== Raspberry Pi Hardware Watchdog Setup ==="
echo ""

# ── 1. Ensure the hardware watchdog kernel module is loaded ────────────

MODULE="bcm2835_wdt"

if ! lsmod | grep -q "^${MODULE} "; then
  echo "[1/5] Loading kernel module: ${MODULE}..."
  modprobe ${MODULE}
  echo "       Loaded."
else
  echo "[1/5] Kernel module ${MODULE} is already loaded."
fi

# ── 2. Make it persistent across reboots ───────────────────────────────

if ! grep -q "^${MODULE}$" /etc/modules 2>/dev/null; then
  echo "[2/5] Adding ${MODULE} to /etc/modules for auto-load on boot..."
  echo "${MODULE}" >> /etc/modules
  echo "       Added."
else
  echo "[2/5] ${MODULE} already in /etc/modules."
fi

# ── 3. Enable watchdog in device tree ───────────────────────────────────

CONFIG_TXT="/boot/config.txt"
# Also check the new location on Bookworm+
if [ ! -f "$CONFIG_TXT" ]; then
  CONFIG_TXT="/boot/firmware/config.txt"
fi

if [ -f "$CONFIG_TXT" ]; then
  if ! grep -q '^dtparam=watchdog=on' "$CONFIG_TXT" 2>/dev/null; then
    echo "[3/5] Enabling watchdog in $CONFIG_TXT..."
    echo "dtparam=watchdog=on" >> "$CONFIG_TXT"
    echo "       Added."
  else
    echo "[3/5] dtparam=watchdog=on already in $CONFIG_TXT."
  fi
else
  echo "[3/5] WARNING: config.txt not found at /boot/config.txt or /boot/firmware/config.txt"
  echo "       Skipping dtparam step — watchdog may still work via module."
fi

# ── 4. Configure systemd RuntimeWatchdogSec ────────────────────────────

SYSTEMD_CONF="/etc/systemd/system.conf"
WATCHDOG_LINE="RuntimeWatchdogSec=60s"

if ! grep -q "^RuntimeWatchdogSec=" "$SYSTEMD_CONF" 2>/dev/null; then
  echo "[4/5] Configuring kernel watchdog in $SYSTEMD_CONF..."
  echo "$WATCHDOG_LINE" >> "$SYSTEMD_CONF"
  echo "       Added ($WATCHDOG_LINE)."
elif grep -q "^#RuntimeWatchdogSec=" "$SYSTEMD_CONF" 2>/dev/null; then
  echo "[4/5] Uncommenting RuntimeWatchdogSec in $SYSTEMD_CONF..."
  sed -i 's/^#RuntimeWatchdogSec=.*/RuntimeWatchdogSec=60s/' "$SYSTEMD_CONF"
  echo "       Uncommented and set to 60s."
else
  echo "[4/5] RuntimeWatchdogSec already configured in $SYSTEMD_CONF."
fi

# ── 5. Add panic=60 to kernel cmdline (auto-reboot after kernel panic) ─

CMDLINE="/boot/cmdline.txt"
if [ ! -f "$CMDLINE" ]; then
  CMDLINE="/boot/firmware/cmdline.txt"
fi

if [ -f "$CMDLINE" ]; then
  if ! grep -q 'panic=' "$CMDLINE" 2>/dev/null; then
    echo "[5/5] Adding panic=60 to kernel cmdline ($CMDLINE)..."
    # cmdline.txt must be a single line — append without newline
    sed -i 's/$/ panic=60/' "$CMDLINE"
    echo "       Added panic=60."
  else
    echo "[5/5] panic= already present in $CMDLINE."
  fi
else
  echo "[5/5] WARNING: cmdline.txt not found. Skipping panic= kernel param."
fi

# ── Done ────────────────────────────────────────────────────────────────

echo ""
echo "=== Hardware watchdog setup complete ==="
echo ""
echo "Verification:"
echo "  wdctl                       # Check watchdog device status"
echo "  cat /dev/watchdog           # Test: should reboot Pi after ~15s (Ctrl+C to cancel)"
echo "  sudo journalctl -u systemd  # Look for 'hardware watchdog' messages on boot"
echo ""
echo "REBOOT REQUIRED for kernel cmdline changes to apply."
echo "Run: sudo reboot"