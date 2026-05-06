# DeepSeek_Web_To_API

Language: [中文](README.MD) | [English](README.en.md)

DeepSeek_Web_To_API is a self-hosted Go gateway that exposes DeepSeek Web sessions through OpenAI-, Claude-, and Gemini-compatible APIs. It also includes a React/Vite admin console for accounts, keys, proxies, response cache, chat history, and runtime metrics.

This repository is a Go backend plus React admin WebUI project, documented according to the current source tree and deployment model.

## Highlights

- OpenAI-compatible routes: `/v1/models`, `/v1/chat/completions`, `/v1/responses`, `/v1/files`, `/v1/embeddings`.
- Claude-compatible routes: `/anthropic/v1/messages`, `/v1/messages`, `/messages`, and `count_tokens`.
- Gemini-compatible routes: `/v1beta/models/{model}:generateContent`, `streamGenerateContent`, and `/v1/models/{model}:*`.
- Managed account pool with token refresh, queueing, per-account concurrency, and optional target-account routing.
- Direct-token mode when the caller token is not configured as a managed API key.
- Protocol response cache: memory TTL 5 minutes, memory cap 3.8 GB, gzip disk TTL 4 hours, disk cap 16 GB.
- SQLite chat history with gzip-compressed detail blobs and a default 20,000-record retention limit.
- Admin console served from `/admin`.

## Quick Start

```bash
cp config.example.json config.json
npm ci --prefix webui
npm run build --prefix webui
go run ./cmd/DeepSeek_Web_To_API
```

Then open `http://127.0.0.1:5001/admin`.

Docker Compose:

```bash
cp config.example.json config.json
cp .env.example .env
docker compose up -d
```

## Required Configuration

Edit `config.json` first:

- `keys` / `api_keys`: API keys used by client applications.
- `accounts`: DeepSeek Web accounts.
- `admin.key` or `admin.password_hash`: admin login credential.
- `admin.jwt_secret`: JWT signing secret.
- `server.bind_addr`: use `127.0.0.1` when Caddy/Nginx terminates public traffic.

Use `.env` only for deployment-layer overrides such as Docker host port, config path, or platform-injected config JSON.

## Documentation

- [Chinese Documentation Index](docs/README.md)
- [API Reference](API.en.md)
- [Configuration](docs/configuration.md)
- [Deployment](docs/deployment.md)
- [Storage and Cache](docs/storage-cache.md)
- [Security](docs/security.md)

## Local Gates

```bash
./scripts/lint.sh
./tests/scripts/check-refactor-line-gate.sh
./tests/scripts/run-unit-all.sh
npm run build --prefix webui
```

## License

This project is released under the [MIT License](LICENSE). Anyone may use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the software, subject to inclusion of the copyright and license notice. The software is provided "AS IS", without warranty of any kind.

## Disclaimer

This project is intended for learning, research, personal experiments, and internal validation. Users are responsible for complying with applicable service terms, platform rules, and laws.
