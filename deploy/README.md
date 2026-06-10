# Freezer App — Raspberry Pi Zero W Deployment Guide

Headless deployment for Raspberry Pi Zero W (ARMv6, 32-bit). The Go backend runs as a single ~10MB binary with embedded SQLite. The frontend is served as static files. No Node.js, npm, or Go toolchain needed on the Pi.

**HTTPS** is provided by **Tailscale Funnel** — persistent URL, survives reboots and network changes, free, no domain required.

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│ Your Mac                                            │
│  deploy/build.sh → cross-compiles ARMv6 binary      │
│                  → builds frontend (Vite)            │
│                  → packages release.tar.gz           │
│  deploy/deploy.sh → SCP to Pi + remote install      │
└─────────────────┬───────────────────────────────────┘
                  │ SCP + SSH
┌─────────────────▼───────────────────────────────────┐
│ Raspberry Pi Zero W                                  │
│  /home/admin/freezer-app/                            │
│   ├── gobackend/freezer-server  (ARMv6 binary)      │
│   ├── frontend/dist/            (static files)       │
│   ├── prod-start.sh             (startup script)     │
│   ├── data/freezer.db           (SQLite database)    │
│   ├── .env                      (secrets)            │
│   └── deploy/                                          │
│        ├── tailscale-setup.sh   (one-time setup)     │
│        ├── freezer-app.service  (systemd)            │
│        └── tailscale-funnel.service                 │
│                                                       │
│  Service :3000  ← Go binary (API + static files)     │
│  Funnel  :443   ← Tailscale (persistent HTTPS URL)   │
│  tailscaled     ← auto-starts on boot (package)      │
└─────────────────────────────────────────────────────┘
```

---

## Prerequisites

### On Your Mac
- **Go 1.22+** (for cross-compilation)
- **Node.js 18+** / **npm** (for frontend build)
- **SSH access** to the Pi (key-based or password)

### On the Raspberry Pi Zero W
- **Raspbian/Raspberry Pi OS** (Lite is fine — no desktop needed)
- **SSH enabled** (`sudo raspi-config` → Interface Options → SSH → Enable)
- **Wi-Fi configured** (or Ethernet via USB adapter)
- **Tailscale account** (free — sign up at [tailscale.com](https://tailscale.com))
- **`file` utility** (for verifying binary architecture — included in most Pi images)

---

## One-Command Deploy (from Mac)

```bash
# Build, SCP, and remote install in one step
./deploy/deploy.sh pi@freezer-app.local

# Or with IP address
./deploy/deploy.sh pi@192.168.1.42
```

This will:
1. Cross-compile the Go binary for ARMv6 (`GOOS=linux GOARCH=arm GOARM=6`)
2. Build the frontend with Vite (`npm run build`)
3. Package everything into `release.tar.gz`
4. SCP to the Pi's home directory
5. SSH in and extract to `/home/admin/freezer-app/`
6. Set up `.env` from `.env.example` if not present

---

## Manual Step-by-Step Deployment

### 1. Build the Release (on Mac)

```bash
./deploy/build.sh
```

This creates `deploy/release.tar.gz` containing:
- `gobackend/freezer-server` — ARMv6 Go binary
- `frontend/dist/` — Vite production build
- `prod-start.sh` — startup script
- `.env.example` — environment template
- `deploy/tailscale-setup.sh` — one-time Tailscale Funnel setup

### 2. Copy to the Pi

```bash
# Transfer the release and env example
scp deploy/release.tar.gz pi@freezer-app.local:~
scp .env.example pi@freezer-app.local:~
```

### 3. SSH into the Pi and Install

```bash
ssh pi@freezer-app.local
```

Then on the Pi:

```bash
# Create app directory
mkdir -p ~/freezer-app
cd ~/freezer-app

# Extract
tar -xzf ~/release.tar.gz
chmod +x gobackend/freezer-server prod-start.sh deploy/tailscale-setup.sh
mkdir -p data

# Set up environment
if [ ! -f .env ]; then
  cp ~/.env.example .env
fi

nano .env  # Review configuration (PORT, logging, etc.)
```

### 4. Start the Server

```bash
cd ~/freezer-app
./prod-start.sh
```

Open `http://<pi-ip>:3000` on your local network. Sign in with your email and password. Done.

---

## Tailscale Funnel Setup (Persistent HTTPS)

The barcode scanner requires HTTPS (iOS `getUserMedia` restriction). Tailscale Funnel provides a **persistent HTTPS URL** that survives reboots, power outages, and network changes — unlike Cloudflare quick tunnels which give you a new random URL every time.

### One-Time Setup

On the Pi:

```bash
cd ~/freezer-app
./deploy/tailscale-setup.sh
```

This script will:
1. Install Tailscale (if not already present)
2. Start the `tailscaled` daemon (auto-starts on boot)
3. Prompt you to authenticate the Pi with your Tailscale account
4. Enable HTTPS certificates
5. Enable Funnel on port 3000

After running, your app will be available at:

```
https://<pi-hostname>.<your-tailnet>.ts.net
```

Example: `https://freezer-pi.your-tailnet.ts.net`

### How It Survives Everything

| Event | What happens |
|---|---|
| **Power outage** | Pi boots, `freezer-app.service` restarts, `tailscaled` restarts (package auto-service), Funnel config persists, URL is the same |
| **Network change** (new Wi-Fi, new location) | Tailscale reconnects over any internet path, same URL keeps working |
| **Pi moves to a different house** | Plug in, Tailscale re-establishes, same URL, no config changes |
| **Reboot** | Everything auto-starts via systemd, same URL, zero manual intervention |

### Finding Your URL

At any time on the Pi:

```bash
tailscale status
# Look for the <hostname>.<tailnet>.ts.net entry
```

Or open [login.tailscale.com/admin/machines](https://login.tailscale.com/admin/machines) in a browser.

### CORS

The Go server allows CORS from `*.ts.net` domains (already configured in `main.go`). No additional setup needed.

---

## Auto-Start on Boot (systemd)

Install the systemd service file to start the app automatically after power loss:

```bash
sudo cp ~/freezer-app/deploy/freezer-app.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now freezer-app

# Check status
sudo systemctl status freezer-app
```

`tailscaled` is already configured as a systemd service by the Tailscale package — it auto-starts on boot. The Funnel configuration persists across reboots.

### Service Dependencies

- `freezer-app.service` — starts after network is online, restarts on failure (5s delay)
- `tailscaled.service` — installed by Tailscale package, auto-enabled

---

## Accessing Your App

| Method | URL | HTTPS? | Scanner works? |
|---|---|---|---|
| **Local network** | `http://<pi-ip>:3000` | No | No (iOS requires HTTPS) |
| **Tailscale Funnel** | `https://<hostname>.<tailnet>.ts.net` | Yes | Yes |
| **Tailscale direct** | `http://<pi-ip>:3000` (on tailnet) | No | No |

**For iPhone scanning**: use the Tailscale Funnel URL. It's the only one with HTTPS.

---

## Database

### Location
`/home/admin/freezer-app/data/freezer.db`

### Backup

```bash
# On the Pi
cp ~/freezer-app/data/freezer.db ~/freezer-app/data/freezer.db.backup

# To your Mac
scp pi@freezer-app.local:~/freezer-app/data/freezer.db ./freezer.db.backup
```

### Restore

```bash
# Stop the server first
sudo systemctl stop freezer-app

# Copy the DB file
cp ~/freezer.db.backup ~/freezer-app/data/freezer.db

# Start back up — GORM AutoMigrate will apply any pending schema changes
sudo systemctl start freezer-app
```

### Migrations
GORM `AutoMigrate` runs every time the server starts. It creates missing tables and adds missing columns. **Existing data is never dropped.** No manual migration scripts needed.

---

## Updates

To deploy an updated version:

```bash
# On your Mac: rebuild and redeploy
./deploy/deploy.sh pi@freezer-app.local

# The script handles everything. On the Pi side:
# - Old binary is overwritten
# - frontend/dist/ is updated
# - prod-start.sh is refreshed
# - .env is NOT overwritten (preserves your config)
# - data/ is NOT touched (preserves your database)
```

Then restart:

```bash
ssh pi@freezer-app.local
sudo systemctl restart freezer-app
```

---

## Troubleshooting

### Binary won't start: "Exec format error"
The binary was compiled for the wrong architecture. Verify:
```bash
file ~/freezer-app/gobackend/freezer-server
# Should output: ELF 32-bit LSB executable, ARM, EABI5 version 1 (SYSV)
```
If it says "arm64" or "x86-64", rebuild with `GOOS=linux GOARCH=arm GOARM=6`.

### Server starts but frontend doesn't load (404)
Check that `frontend/dist/` exists:
```bash
ls ~/freezer-app/frontend/dist/index.html
```
If missing, the build step was skipped. Run `./deploy/build.sh` on your Mac and redeploy.

### "Port 3000 already in use"
```bash
# Find what's using the port
sudo lsof -i :3000
# Kill it or adjust PORT in .env
```

### Scanner doesn't work on iPhone
- Use the **Tailscale Funnel URL** (`https://<hostname>.<tailnet>.ts.net`), not the local IP
- iOS Safari requires HTTPS for camera access
- Check Tailscale Funnel is active: `tailscale funnel status`
- Check the server is running: `sudo systemctl status freezer-app`

### Services won't start
```bash
# View full logs
sudo journalctl -u freezer-app -n 50 --no-pager

# Check .env exists
cat ~/freezer-app/.env
```

### Tailscale Funnel not working
```bash
# Check Tailscale is connected
tailscale status

# Re-enable Funnel if needed
tailscale funnel 3000

# Check tailscaled is running
sudo systemctl status tailscaled
```

---

## Pi Zero W Memory Notes

- Go server idles at ~15-20MB RAM with SQLite + GORM
- `tailscaled` adds ~15-20MB
- Total: ~30-40MB on a 512MB device — plenty of headroom
- No swap needed for normal operation

---

## Quick Reference

| Command | Where | Purpose |
|---|---|---|
| `./deploy/build.sh` | Mac | Cross-compile + package |
| `./deploy/deploy.sh pi@<host>` | Mac | Build + SCP + install |
| `./prod-start.sh` | Pi | Start server |
| `./deploy/tailscale-setup.sh` | Pi | One-time Tailscale Funnel setup |
| `tailscale funnel status` | Pi | Check Funnel status |
| `tailscale status` | Pi | Find your Funnel URL |
| `sudo systemctl restart freezer-app` | Pi | Restart server |
| `sudo journalctl -u freezer-app -f` | Pi | Follow server logs |