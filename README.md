# Scriptorum — Book request manager

![Scriptorum logo](internal/httpapi/web/static/icon.svg)

[![Go 1.25+](https://img.shields.io/badge/go-1.25+-blue)](https://go.dev)
[![License: GPLv3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

Lightweight, self-hosted web app to manage eBook and audiobook requests. It searches Readarr and public sources, lets users request titles, and helps admins send those into Readarr with a simple dark UI.

This README is meant to be a quick start; deeper details live in `API.md` and the source.

---

## ✨ Key features

- Multi-source search (Readarr, Amazon public pages, Open Library).
- Request queue with approve/decline/delete and bulk actions.
- Dual Readarr instances (ebooks + audiobooks) with profiles and root folders.
- First-run setup wizard (server URL, admin user, Readarr, OAuth).
- Local auth plus optional OAuth/OIDC login with role-based access.
- Notifications via ntfy, email (SMTP), and Discord (incl. one-click approvals).
- Dark, Tailwind + HTMX-powered web UI.

All of these are implemented in this repo today.

---

## How it works

1. Users search for titles, pick a result, and submit a request (kind = ebook or audiobook).
2. Requests land on the admin queue where bulk or single approvals push books into the matching Readarr instance using pre-set profiles/root folders/tags.
3. Readarr handles the download/monitoring; Scriptorum tracks request status and shows it back to the requester.
4. Notifications (optional) ping admins or requesters via ntfy/email/Discord with one-click approval links.

The UI sits on top of a single SQLite database (`data/scriptorum.db`) and a YAML config (`data/scriptorum.yaml`).

---

## Quick pointers

- Docker: see "Docker quick start" below (recommended).
- Native: use `build.ps1` on Windows or `go build`.
- Config file: `data/scriptorum.yaml` (created on first run if absent).
- Default HTTP listen port: `:8491` (can be changed via `http.listen`).
- Health endpoints: `/healthz` and `/version`.

---

## Docker quick start (recommended)

1. Clone the repository:

```powershell
git clone https://github.com/your-username/scriptorum.git
cd scriptorum
```

2. Start with Docker Compose (builds the image if needed):

```powershell
docker compose up -d --build
```

3. Open the UI in your browser. The repo's `docker-compose.yml` maps host port `8491` → container port `8491` by default:

http://localhost:8491

Notes
- If you change `http.listen` in your config, update the compose port mapping.
- To view logs:

```powershell
docker compose logs -f scriptorum
```

- To stop and remove containers:

```powershell
docker compose down
```

---

## Run locally (development / advanced)

Use the included PowerShell helper on Windows (works on other platforms with Go installed).

Prerequisites
- Go 1.25+
- SQLite3 (optional; the binary uses the modernc SQLite driver)
- Node.js / npm (optional for rebuilding CSS)

Build and run using the helper script:

```powershell
# build the binary
./build.ps1 build

# run tests
./build.ps1 test

# run the app (builds first if needed)
./build.ps1 run
```

Or build and run directly with Go:

```powershell
go build -o ./bin/scriptorum ./cmd/scriptorum
./bin/scriptorum
```

By default the app will create `data/scriptorum.yaml` and `data/scriptorum.db` on first run if they don't exist. Override paths with environment variables (PowerShell example):

```powershell
$env:SCRIPTORUM_CONFIG_PATH = "C:\data\scriptorum.yaml"
$env:SCRIPTORUM_DB_PATH = "C:\data\scriptorum.db"
```

---

## Configuration basics

- **DON'T PANIC — the setup wizard writes this for you.** On first launch Scriptorum generates `data/scriptorum.yaml` with sensible defaults, so you only need to edit it if you want to tweak things later.
- **Everything works out of the box.** Skip straight to running the container if you just want the defaults; the wizard will prompt for anything essential.

- Example config: `scriptorum.example.yaml` (repo root). Copy it to `data/scriptorum.yaml` and edit.
- Key fields you’ll likely touch:
  - `http.listen` — HTTP listen address.
  - `db.path` — SQLite DB location.
  - `readarr.ebooks` / `readarr.audiobooks` — `base_url`, `api_key`, profile, root folder, tags.
  - `notifications` — ntfy/SMTP/Discord settings and which events to send.
  - `oauth` — OIDC issuer, client id/secret, scopes, username claim, allowlists.

After changing `data/scriptorum.yaml`, restart the app or container.

### Minimum you need to configure

- At least one admin account (created via setup wizard or `/users`).
- `readarr.ebooks` and/or `readarr.audiobooks` `base_url` + `api_key` (can skip one if you only run the other).
- Optional: notification providers (ntfy/SMTP/Discord) and OAuth if you prefer SSO.

---

## Admin toolkit

- `/requests` — queue with filters, bulk approve/decline, request history.
- `/users` — manage local accounts, roles, and password resets.
- `/settings` — Readarr targets, quality profiles, root folders, OAuth, and general settings.
- `/notifications` — configure/test ntfy, SMTP, Discord.
- `/approve/{token}` — one-click approvals from notification links.

All admin pages are HTMX-driven and require the `admin` role.

---

## Data & backups

- Config: `data/scriptorum.yaml` (override with `SCRIPTORUM_CONFIG_PATH`).
- Database: `data/scriptorum.db` (override with `SCRIPTORUM_DB_PATH`).
- Static assets and built CSS live under `internal/httpapi/web` and `assets/`.

Back up the YAML + SQLite files together. The database is small and safe to snapshot while the app is stopped.

---

## Docs & license

- API and advanced options: see `API.md`.
- License: GNU GPLv3 (see `LICENSE`).

---

**Made for the self-hosted media community.**