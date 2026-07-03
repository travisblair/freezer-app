#!/usr/bin/env bash
# ── WiFi Watchdog ──────────────────────────────────────────────────────
# Pings the gateway every run. 3 consecutive failures = reboot.
#
# Install: sudo cp deploy/wifi-watchdog.* /etc/systemd/system/
#           sudo systemctl daemon-reload
#           sudo systemctl enable --now wifi-watchdog.timer
#
# Counter file: /tmp/wifi-fail-count
set -euo pipefail

GATEWAY="192.168.50.1"
MAX_FAILS=3
COUNTER_FILE="/tmp/wifi-fail-count"

# Ping gateway 3 times, 2s timeout each. All must fail to count as failure.
if ping -c 3 -W 2 "$GATEWAY" > /dev/null 2>&1; then
    echo 0 > "$COUNTER_FILE"
    exit 0
fi

# Failure — increment counter
if [ -f "$COUNTER_FILE" ]; then
    count=$(cat "$COUNTER_FILE")
else
    count=0
fi
count=$((count + 1))
echo "$count" > "$COUNTER_FILE"

logger -t wifi-watchdog "Gateway $GATEWAY unreachable (failure $count/$MAX_FAILS)"

if [ "$count" -ge "$MAX_FAILS" ]; then
    logger -t wifi-watchdog "Rebooting due to sustained WiFi loss"
    echo 0 > "$COUNTER_FILE"
    /sbin/reboot
fi
