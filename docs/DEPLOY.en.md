# DeepSeek_Web_To_API Deployment Guide

Language: [中文](DEPLOY.md) | [English](DEPLOY.en.md)

This guide covers all deployment methods for the current Go-based codebase.

Doc map: [Index](./README.md) | [Architecture](./ARCHITECTURE.en.md) | [API](../API.en.md) | [Testing](./TESTING.md)

---

## Table of Contents

- [Recommended deployment priority](#recommended-deployment-priority)
- [Prerequisites](#0-prerequisites)
- [1. Download Release Binaries](#1-download-release-binaries)
- [2. Docker / GHCR Deployment](#2-docker--ghcr-deployment)
- [3. Local Run from Source](#3-local-run-from-source)
- [4. Reverse Proxy (Nginx)](#4-reverse-proxy-nginx)
- [5. Linux systemd Service](#5-linux-systemd-service)
- [6. Post-Deploy Checks](#6-post-deploy-checks)
- [7. Pre-Release Local Regression](#7-pre-release-local-regression)

---

## Recommended deployment priority

Recommended order when choosing a deployment method:

1. **Download and run release binaries**: the easiest path for most users because the artifacts are already built.
2. **Docker / GHCR image deployment**: suitable for containerized, orchestrated, or cloud environments.
3. **Run from source / build locally**: suitable for development, debugging, or when you need to modify the code yourself.

---

## 0. Prerequisites

| Dependency | Minimum Version | Notes |
| --- | --- | --- |
| Go | 1.26+ | Build backend |
| Node.js | `20.19+` or `22.12+` | Only needed to build WebUI locally |
| npm | Bundled with Node.js | Install WebUI dependencies |

Config source (choose one):

- **File**: `config.json` (recommended for local/Docker)
- **Environment variable**: `DEEPSEEK_WEB_TO_API_CONFIG_JSON` (for read-only or cloud-injected runtime config; supports raw JSON or Base64)

Unified recommendation (best practice):

```bash
cp config.example.json config.json
# Edit config.json
```

The template is paired with `config.example.json.Annotation`. The annotation
file is not loaded at runtime; it documents each deployment key's meaning,
type, default, bounds, and optional environment override.

Use `config.json` as the single source of truth:
- Local run: read `config.json` directly
- Docker / cloud platforms: generate `DEEPSEEK_WEB_TO_API_CONFIG_JSON` (Base64) from `config.json` and inject it

---

## 1. Download Release Binaries

Built-in GitHub Actions workflow: `.github/workflows/release-artifacts.yml`

- **Trigger**: by default only on Release `published`; you can also run it manually via `workflow_dispatch` and pass `release_tag` to rerun / backfill
- **Outputs**: multi-platform binary archives, Linux Docker image export tarballs, and `sha256sums.txt`
- **Container publishing**: GHCR only (`ghcr.io/meow-calculations/deepseek-web-to-api`)

| Platform | Architecture | Format |
| --- | --- | --- |
| Linux | amd64, arm64, armv7 | `.tar.gz` |
| macOS | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

Each archive includes:

- `deepseek-web-to-api` executable (`deepseek-web-to-api.exe` on Windows)
- `static/admin/` (built WebUI assets)
- `config.example.json`, `.env.example`
- `README.MD`, `README.en.md`, `LICENSE`

### Usage

```bash
# 1. Download the archive for your platform
# 2. Extract
tar -xzf deepseek-web-to-api_<tag>_linux_amd64.tar.gz
cd deepseek-web-to-api_<tag>_linux_amd64

# 3. Configure
cp config.example.json config.json
# Edit config.json

# 4. Start
./deepseek-web-to-api
```

### Maintainer Release Flow

1. Create and publish a GitHub Release (with tag, for example `vX.Y.Z`)
2. Wait for the `Release Artifacts` workflow to complete
3. Download the matching archive from Release Assets

---

## 2. Docker / GHCR Deployment

### 2.1 Basic Steps

```bash
# Pull prebuilt image
docker pull ghcr.io/meow-calculations/deepseek-web-to-api:latest

# Copy deployment overrides and the single runtime config
cp .env.example .env
cp config.example.json config.json

# Edit config.json and set at least:
#   admin.key
#   admin.jwt_secret
#   keys / accounts
# Keep .env for deployment-layer overrides only. Optionally set the host port:
#   DEEPSEEK_WEB_TO_API_HOST_PORT=6011

# Start
docker-compose up -d

# View logs
docker-compose logs -f
```

The default `docker-compose.yml` directly uses `ghcr.io/meow-calculations/deepseek-web-to-api:latest` and maps host port `6011` to container port `5001`. If you want `5001` exposed directly, set `DEEPSEEK_WEB_TO_API_HOST_PORT=5001` (or adjust the `ports` mapping).
The compose template sets `DEEPSEEK_WEB_TO_API_CONFIG_PATH=/data/config.json` and mounts `./config.json:/data/config.json` by default to avoid persistence failures on a read-only `/app`. If `DEEPSEEK_WEB_TO_API_CONFIG_PATH` is not set and the runtime directory is `/app`, the program prefers `/data/config.json` when `/data` exists and otherwise falls back to `/app/config.json` for legacy container upgrades.

If you want a pinned version instead of `latest`, you can also pull a specific tag directly:

```bash
docker pull ghcr.io/meow-calculations/deepseek-web-to-api:v3.0.0
```

### 2.2 Update

```bash
docker-compose up -d --build
```

### 2.3 Docker Architecture

The `Dockerfile` now provides two image paths:

1. **Default local/dev path (`runtime-from-source`)**: a three-stage build (WebUI build + Go build + runtime).
2. **Release path (`runtime-from-dist`)**: the release workflow first creates tag-named release archives, then copies the Linux bundles to `dist/docker-input/linux_amd64.tar.gz` / `linux_arm64.tar.gz`; Docker consumes those prepared inputs directly, without rerunning `npm build`/`go build`.

The release path keeps Docker images aligned with release archives and reduces duplicate build work.

Container entry command: `/usr/local/bin/deepseek-web-to-api`, default exposed port: `5001`.

### 2.4 Development Mode

```bash
docker-compose -f docker-compose.dev.yml up
```

Development features:
- Source code mounted (live changes)
- `LOG_LEVEL=DEBUG`
- No auto-restart

### 2.5 Health Check

Docker Compose includes a built-in health check:

```yaml
healthcheck:
  test: ["CMD", "/usr/local/bin/busybox", "wget", "-qO-", "http://localhost:${PORT:-5001}/healthz"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 10s
```

### 2.6 Docker Troubleshooting

If container logs look normal but the admin panel is unreachable, check these first:

1. **Port alignment**: when `PORT` is not `5001`, use the same port in your URL (for example `http://localhost:8080/admin`).
2. **WebUI assets in dev compose**: `docker-compose.dev.yml` runs `go run` in a dev image and does not auto-install Node.js inside the container; if `static/admin` is missing in your repo, `/admin` will return 404. Build once on host: `./scripts/build-webui.sh`.

### 2.7 Zeabur One-Click (Dockerfile)

This repo includes a `zeabur.yaml` template for one-click deployment on Zeabur:

Zeabur template URL: `https://zeabur.com/templates/L4CFHP`

Notes:

- **Port**: DeepSeek_Web_To_API listens on `5001` by default; the template sets `PORT=5001`.
- **Persistent config**: the template mounts `/data` and sets `DEEPSEEK_WEB_TO_API_CONFIG_PATH=/data/config.json`. After importing config in Admin UI, it will be written and persisted to this path.
- **`open /app/config.json: permission denied`**: this means the instance is trying to persist runtime tokens to a read-only path (commonly `/app` inside the image).
  Recommended handling:
  1. Set a writable path explicitly: `DEEPSEEK_WEB_TO_API_CONFIG_PATH=/data/config.json` (and mount a persistent volume at `/data`);
  2. If you bootstrap with `DEEPSEEK_WEB_TO_API_CONFIG_JSON` and do not need runtime writeback, keep env-backed mode (`DEEPSEEK_WEB_TO_API_ENV_WRITEBACK` disabled);
  3. In current versions, login/session tests continue even if persistence fails; Admin API returns a warning that token persistence failed and token is memory-only until restart.
- **Build version**: Zeabur / regular `docker build` does not require `BUILD_VERSION` by default. The image prefers that build arg when provided, and automatically falls back to the repo-root `VERSION` file when it is absent.
- **First login**: after deployment, open `/admin` and login with `admin.key` from `/data/config.json` (recommended: rotate to a strong secret after first login).

---

## 3. Local Run from Source

### 3.1 Basic Steps

```bash
# Clone
git clone https://github.com/Meow-Calculations/DeepSeek_Web_To_API.git
cd deepseek-web-to-api

# Copy and edit config
cp config.example.json config.json
# Open config.json and fill in:
#   - keys: your API access keys
#   - accounts: DeepSeek accounts (email or mobile + password)

# Start
go run ./cmd/DeepSeek_Web_To_API
```

Default local access URL: `http://127.0.0.1:5001`; the server actually binds to `0.0.0.0:5001` (override with `PORT`).

### 3.2 WebUI Build

On first local startup, if `static/admin/` is missing, DeepSeek_Web_To_API will automatically attempt to build the WebUI (requires Node.js/npm; when dependencies are missing it runs `npm ci` first, then `npm run build -- --outDir static/admin --emptyOutDir`).

Manual build:

```bash
./scripts/build-webui.sh
```

Or step by step:

```bash
cd webui
npm ci
npm run build
# Output goes to static/admin/
```

Control auto-build via environment variable:

```bash
# Disable auto-build
DEEPSEEK_WEB_TO_API_AUTO_BUILD_WEBUI=false go run ./cmd/DeepSeek_Web_To_API

# Force enable auto-build
DEEPSEEK_WEB_TO_API_AUTO_BUILD_WEBUI=true go run ./cmd/DeepSeek_Web_To_API
```

Protocol response cache defaults to `cache.response.dir` in `config.json` (the template value is `data/response_cache`); files are gzip-compressed, retained for 4 hours, and capped at 16GB total. The memory layer lives for 5 minutes and is capped at 3.8GB. Prefer changing `cache.response.dir` when you want the cache on a dedicated disk or container volume; keep `DEEPSEEK_WEB_TO_API_RESPONSE_CACHE_DIR` only as a deployment-layer override.
Chat history defaults to the SQLite file configured by `storage.chat_history_sqlite_path`. Full request details, context, and response text are gzip-compressed into `detail_blob` before write. Legacy uncompressed `detail_json` rows are migrated in small batches at service startup, then DeepSeek_Web_To_API attempts `VACUUM` to reclaim file space; the first startup after upgrade may therefore take longer than usual. If `VACUUM` fails because disk space is tight or SQLite is busy, new writes still stay compressed, and the database file shrinks after a later successful compact.
Runtime config writeback, chat-history SQLite, response cache, raw samples, and testsuite artifacts may contain accounts, tokens, request bodies, or response bodies. The program now treats them as sensitive runtime data and writes private directories/files with `0700` / `0600` permissions by default. Do not mount these paths to publicly readable shared volumes.

### 3.3 Compile to Binary

```bash
go build -o deepseek-web-to-api ./cmd/DeepSeek_Web_To_API
./deepseek-web-to-api
```

---

## 4. Reverse Proxy (Nginx)

When deploying behind Nginx, **you must disable buffering** for SSE streaming to work:

```nginx
location / {
    proxy_pass http://127.0.0.1:5001;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_buffering off;
    proxy_cache off;
    chunked_transfer_encoding on;
    tcp_nodelay on;
}
```

For HTTPS, add SSL at the Nginx layer:

```nginx
server {
    listen 443 ssl;
    server_name api.example.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:5001;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding on;
        tcp_nodelay on;
    }
}
```

---

## 5. Linux systemd Service

### 6.1 Installation

```bash
# Copy compiled binary and related files to target directory
sudo mkdir -p /opt/deepseek-web-to-api
sudo cp deepseek-web-to-api config.json /opt/deepseek-web-to-api/
sudo mkdir -p /opt/deepseek-web-to-api/static
sudo cp -r static/admin /opt/deepseek-web-to-api/static/admin
```

### 6.2 Create systemd Service File

```ini
# /etc/systemd/system/deepseek-web-to-api.service

[Unit]
Description=DeepSeek_Web_To_API (Go)
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/deepseek-web-to-api
Environment=PORT=5001
Environment=DEEPSEEK_WEB_TO_API_CONFIG_PATH=/opt/deepseek-web-to-api/config.json
ExecStart=/opt/deepseek-web-to-api/deepseek-web-to-api
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### 6.3 Common Commands

```bash
# Reload service config
sudo systemctl daemon-reload

# Enable on boot
sudo systemctl enable deepseek-web-to-api

# Start
sudo systemctl start deepseek-web-to-api

# Check status
sudo systemctl status deepseek-web-to-api

# View logs
sudo journalctl -u deepseek-web-to-api -f

# Restart
sudo systemctl restart deepseek-web-to-api

# Stop
sudo systemctl stop deepseek-web-to-api
```

---

## 6. Post-Deploy Checks

After deployment (any method), verify in order:

```bash
# 1. Liveness probe
curl -s http://127.0.0.1:5001/healthz
# Expected: {"status":"ok"}

# 2. Readiness probe
curl -s http://127.0.0.1:5001/readyz
# Expected: {"status":"ready"}

# 3. Model list
curl -s http://127.0.0.1:5001/v1/models
# Expected: {"object":"list","data":[...]} (including `*-nothinking` variants)

# 4. Admin panel (if WebUI is built)
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:5001/admin
# Expected: 200

# 5. Test API call
curl http://127.0.0.1:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}]}'
```

For security regression, see [security-audit-2026-05-02.md](security-audit-2026-05-02.md) and rerun at least `gosec ./...`, `govulncheck ./...`, frontend `npm audit`, and secret scanning.

---

## 7. Pre-Release Local Regression

Run the full live testsuite before release (real account tests):

```bash
./tests/scripts/run-live.sh
```

With custom flags:

```bash
go run ./cmd/DeepSeek_Web_To_API-tests \
  --config config.json \
  --admin-key admin \
  --out artifacts/testsuite \
  --timeout 120 \
  --retries 2
```

The testsuite automatically performs:

- ✅ Preflight checks (syntax/build/unit tests)
- ✅ Isolated config copy startup (no mutation to your original `config.json`)
- ✅ Live scenario verification (OpenAI/Claude/Admin/concurrency/toolcall/streaming)
- ✅ Full request/response artifact logging for debugging

For detailed testsuite documentation, see [TESTING.md](TESTING.md). The fixed local PR gates are listed in [TESTING.md](TESTING.md#pr-门禁--pr-gates).
