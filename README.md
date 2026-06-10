# 🧊 Freezer Inventory

Local web app for managing your freezer. Scan barcodes with your phone, manually add items, track counts, search, and export. Runs on your home Wi-Fi — no cloud, no accounts, no subscriptions.

## Quick Start

```bash
# 1. Create .env from example
cp .env.example .env

# 2. Start the server
./prod-start.sh
```

The first time you run, there are no users yet. Generate password hashes and insert them into the database:

```bash
# Generate a hash (type password, copy the output)
./gobackend/freezer-server --hash-password

# Insert users
sqlite3 data/freezer.db "INSERT INTO users (email, password_hash) VALUES ('you@email.com', 'PASTE_HASH_HERE');"
# Add additional users the same way:
# sqlite3 data/freezer.db "INSERT INTO users (email, password_hash) VALUES ('other@email.com', 'PASTE_HASH_HERE');"
```

Open `http://localhost:3000` in your browser. Sign in with your email and password. Done.

## Features

- **Barcode scanning** — use your phone's camera (requires HTTPS via Tailscale Funnel)
- **Manual entry** — add items with or without barcodes
- **Multi-barcode** — one item can have many barcodes (e.g., family pack + single)
- **Duplicate detection** — when barcode exists, offers to increment the existing item
- **Unknown barcode flow** — scan something new → create new item or link to existing
- **Search + filter** — search by name, toggle deleted items
- **Edit items** — rename, adjust quantities
- **Soft delete / restore** — count=0 hides item, restoreable
- **Hard delete** — permanent removal
- **Bulk delete** — select all, delete selected
- **CSV export** — download full inventory
- **Offline banner** — shows when server is unreachable
- **Auto-login** — stays logged in after browser close (HttpOnly cookie)

## Tech Stack

| Layer | Tech |
|---|---|
| Backend | Go 1.26 (single binary, zero deps at runtime) |
| Database | SQLite (pure Go, cross-compiles to ARM for Raspberry Pi) |
| ORM | GORM with auto-migration |
| Frontend | SolidJS + Pico CSS |
| Auth | bcrypt email/password, HttpOnly session cookie |
| Tests | Go integration + Playwright E2E |

## Requirements

- **Go 1.22+** (for stdlib routing)
- **Node.js 18+** (for frontend build and E2E tests)
- **npm** (for Vite and Playwright)

## Raspberry Pi Zero W

The server deploys to a Pi Zero W (ARMv6) behind Tailscale Funnel for persistent HTTPS. See `deploy/README.md` for full deployment instructions.

Cross-compile from your Mac:

```bash
cd gobackend
GOOS=linux GOARCH=arm GOARM=6 go build -o freezer-server .
```

## Running Tests

```bash
# Go backend
cd gobackend && go test ./...

# Playwright E2E (from frontend/)
npx playwright test
```

## Project Structure

```
├── gobackend/       # Go server (single binary)
├── frontend/        # SolidJS app
│   ├── src/         # Components, hooks, store, API client
│   └── e2e/         # Playwright tests
├── deploy/          # Deployment scripts, systemd units
├── prod-start.sh    # Builds + starts everything
├── .env.example     # Template (safe to commit)
└── data/            # SQLite DB lives here (git-ignored)
```

## Notes

- GORM handles all database migrations. Add fields to models, `AutoMigrate` does the rest.
- Never write raw SQL — use GORM's query builder.
- Auth uses bcrypt with cost 8 (tuned for Pi Zero W). Session tokens are 256-bit crypto/rand.
- The `__Host-` cookie prefix is intentional — browsers enforce `secure` and `path=/`.
- No dependencies are committed. Run `npm install` in `frontend/` after cloning.
