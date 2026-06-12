#!/bin/bash
# Capture system state for post-crash analysis.
# Runs every 2 min via cron, writes to SD-card-persisted log.
# After a crash, check ~/freezer-app/data/system-capture.log for the
# last few captures — they show the pre-crash state.

OUTFILE="$HOME/freezer-app/data/system-capture.log"
MAX_LINES=2000

{
    echo "=== $(date -Iseconds) ==="
    echo "--- uptime ---"
    uptime
    echo "--- dmesg (last 20) ---"
    dmesg | tail -20
    echo "--- journal (err+, last 30) ---"
    journalctl --no-pager -p err -n 30 2>/dev/null || echo "(journal unavailable)"
    echo "--- wifi ---"
    cat /proc/net/wireless 2>/dev/null || echo "(wireless info unavailable)"
    echo
} >> "$OUTFILE"

# Rotate: keep last MAX_LINES
tail -n "$MAX_LINES" "$OUTFILE" > "$OUTFILE.tmp" && mv "$OUTFILE.tmp" "$OUTFILE"
