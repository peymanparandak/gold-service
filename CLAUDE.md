# CLAUDE.md

## What is this?

**gold-price-service** is a standalone Go microservice that polls real-time gold prices from BrsApi.ir, caches them in SQLite, and exposes them via a simple HTTP API. It is consumed by the [Zarsaz](https://zarsazapp.ir) gold trading platform.

This service is fully decoupled from the main Zarsaz project — own repo, own CI/CD, own Dokploy deployment with its own domain.

## Commands

```bash
go run main.go                   # Run locally (needs BRS_API_KEY in env)
docker build -t gold-service .   # Build Docker image
docker compose up -d             # Run with Docker Compose (local dev)
```

## Tech Stack

Go, SQLite, net/http (stdlib). No frameworks — intentionally minimal.

## How it works

1. **Poller**: Every `POLL_INTERVAL` seconds (default 60), fetches gold prices from BrsApi.ir using `BRS_API_KEY`
2. **Cache**: Stores latest prices in SQLite at `DB_PATH` (default `/data/gold.db`)
3. **API**: Serves cached prices over HTTP — never calls BrsApi.ir on request

## API Contract

The main Zarsaz app calls this service at `GOLD_SERVICE_URL`. The only endpoint consumed:

### `GET /api/gold/18k`

Returns the cached 18-karat gold price.

**Response:**
```json
{
  "name": "string",
  "price": 0,
  "fetchedAt": "2025-01-01T12:00:00Z",
  "stale": false
}
```

- `price` — price in Rials
- `stale` — `true` if the cached value is older than expected (poller may be failing)

### `GET /health`

Healthcheck endpoint. Returns 200 if the service is running.

## Environment Variables

| Variable        | Required | Default         | Description                            |
|-----------------|----------|-----------------|----------------------------------------|
| `BRS_API_KEY`   | Yes      | —               | API key for BrsApi.ir                  |
| `PORT`          | No       | `8080`          | HTTP server port                       |
| `POLL_INTERVAL` | No       | `60`            | Seconds between price fetches          |
| `DB_PATH`       | No       | `/data/gold.db` | SQLite database file path              |

## Deployment

- **CI/CD**: GitHub Actions (`.github/workflows/build.yml`) builds and pushes to `ghcr.io/peymanparandak/gold-price-service`
- **Hosting**: Dokploy — triggered via `DOKPLOY_GOLD_WEBHOOK_URL` secret after successful build
- **Compose**: `docker-compose.github.yml` is the production compose used by Dokploy

## GitHub Secrets needed

| Secret                     | Description                              |
|----------------------------|------------------------------------------|
| `DOKPLOY_GOLD_WEBHOOK_URL` | Dokploy webhook to trigger redeployment |

`GITHUB_TOKEN` is automatic (used for GHCR login).
