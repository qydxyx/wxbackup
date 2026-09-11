# Docker (`wxbackup`)

Second publish target after the fnOS FPK. The image is the Go HTTP server from `cmd/server`. It does **not** ship a WeChat protocol client.

`network_mode: host` shares the NAS network namespace so later LAN backup/restore can bind the host's 8011/24011. The viewer itself listens on `WXBACKUP_PORT` (default 20365).

## Layout

```text
deploy/docker/
  Dockerfile           multi-stage: golang 1.22 → alpine runtime
  docker-compose.yml   host network, ./data:/data, healthcheck
  README.md
```

Build context is the **repository root** so `COPY` sees `cmd/server` and `go.mod`.

## Run

From the repository root (needs `cmd/server` on the tree, e.g. after merging the server PRs):

```bash
docker compose -f deploy/docker/docker-compose.yml up --build
```

Or:

```bash
docker build -f deploy/docker/Dockerfile -t wxbackup .
docker run --network host \
  -e WXBACKUP_PORT=20365 \
  -e WXBACKUP_DATA=/data \
  -v "$(pwd)/deploy/docker/data:/data" \
  wxbackup
```

Open `http://127.0.0.1:20365/health`. Compose healthcheck hits the same path.

Host networking is a Linux (NAS) feature. Docker Desktop on macOS/Windows does not expose container ports on the host this way; use a Linux VM or a NAS.

## Environment

| Variable | Default | Role |
|----------|---------|------|
| `WXBACKUP_PORT` | `20365` | App HTTP listen port |
| `WXBACKUP_DATA` | `/data` | Canonical store / backup data directory |
| `WXBACKUP_SIDECAR_DIR` | unset | Optional path to an official WeChat Backup export directory |

Sidecar is a **directory watch** of files the official client already wrote. Leave it unset for viewer-only. To enable it, uncomment the volume and env in `docker-compose.yml`:

```yaml
environment:
  WXBACKUP_SIDECAR_DIR: /sidecar
volumes:
  - ./sidecar:/sidecar:ro
```

## Volumes

| Mount | Purpose |
|-------|---------|
| `./data:/data` | Persistent `WXBACKUP_DATA` (SQLite, media). Relative to this compose file. |

The image user is `wxbackup` (uid 65532). If the bind mount is not writable, either `chown -R 65532 deploy/docker/data` or set `user: "1000:1000"` (or your NAS uid) on the service.

## Ports

| Port | Role | Configurable? |
|------|------|----------------|
| 20365 | App HTTP (`WXBACKUP_PORT`) | yes |
| 8011 | WeChat LAN discovery | **no** (host net) |
| 24011 | WeChat LAN transfer | **no** (host net) |

Keep 8011/24011 free on the host if you need phone LAN backup later. The viewer and `/health` still work if those ports are taken.

Do not publish 20365 with `ports:` while using `network_mode: host`; the process already binds the host interface.

## Healthcheck

`GET /health` returns JSON `{"status":"ok","version":"..."}`. Both the image `HEALTHCHECK` and compose `healthcheck` call:

```text
wget -q -O /dev/null http://127.0.0.1:20365/health
```

`wget` is in the alpine runtime. The image healthcheck uses `$WXBACKUP_PORT`. If you change `WXBACKUP_PORT` in compose, update the compose `healthcheck` URL to match.

## Image notes

- Builder: `golang:1.22-alpine`, `CGO_ENABLED=0`, `./cmd/server`.
- Runtime: `alpine:3.20` plus `ca-certificates` and `wget` (not distroless, so the healthcheck has a client).
- ffmpeg / Chromium are **not** in the image. Media conversion should use a host binary in a later PR if needed.
- No WeChat protocol implementation is copied into the image.

Pass a release string with `--build-arg VERSION=1.0.0` (compose: `VERSION=1.0.0 docker compose ...`).
