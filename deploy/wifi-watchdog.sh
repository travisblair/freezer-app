#!/usr/bin/env bash
# ── WiFi Watchdog ──────────────────────────────────────────────────────
# Pings the gateway every run. If unreachable for consecutive checks:
#   Failure 3: restart wpa_supplicant + NetworkManager (soft recovery)
#   Failure 5: reboot (hard recovery)
#
# Install: sudo cp deploy/wifi-watchdog.* /etc/systemd/system/
#           sudo systemctl daemon-reload
#           sudo systemctl enable --now wifi-watchdog.timer
#
# Counter file: /tmp/wifi-fail-count
set -euo pipefail

GATEWAY="192.168.50.1"
MAX_FAILS=5
SOFT_RECOVERY_AT=3
COUNTER_FILE="/tmp/wifi-fail-count"

# Ping gateway 3 times, 2s timeout each. All must fail to count as failure.
if ping -c 3 -W 2 "$GATEWAY" > /dev/null 2>&1; then
    # Success — reset counter
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

# ── Soft recovery at failure 3: restart networking stack ────────────
if [ "$count" -eq "$SOFT_RECOVERY_AT" ]; then
    logger -t wifi-watchdog "Attempting soft recovery: restarting wpa_supplicant + NetworkManager"
    systemctl restart wpa_supplicant 2>/dev/null || true
    systemctl restart NetworkManager 2>/dev/null || true
    # Give the stack time to reassociate
    sleep 30

    # Test again — if recovered, reset counter and bail out
    if ping -c 3 -W 2 "$GATEWAY" > /dev/null 2>&1; then
        logger -t wifi-watchdog "Soft recovery successful — WiFi restored"
        echo 0 > "$COUNTER_FILE"
        exit 0
    fi

    # Still down — let counter keep climbing toward reboot
    logger -t wifi-watchdog "Soft recovery failed — continuing toward hard reboot"
fi

# ── Hard recovery: reboot ────────────────────────────────────────────
if [ "$count" -ge "$MAX_FAILS" ]; then
    logger -t wifi-watchdog "Rebooting due to sustained WiFi loss"
    echo 0 > "$COUNTER_FILE"
    /sbin/reboot
fi
