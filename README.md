# Gold Price Service

Lightweight Go microservice that polls the BRS API for 18k gold prices and caches them in SQLite.

## Endpoints

- `GET /api/gold/18k` — Returns cached gold price
- `GET /health` — Healthcheck

## Response

```json
{
  "name": "طلای 18 عیار",
  "price": 42500000,
  "fetchedAt": "2026-02-26T12:00:00Z",
  "stale": false
}
```

## Run with Docker

```bash
cp .env.example .env
# Edit .env and set BRS_API_KEY
docker compose up --build -d
```

## Run locally

```bash
export BRS_API_KEY=your-key
export DB_PATH=./gold.db
go run .
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `BRS_API_KEY` | (required) | BrsApi.ir API key |
| `PORT` | `8080` | HTTP server port |
| `POLL_INTERVAL` | `60` | Poll interval in seconds |
| `DB_PATH` | `/data/gold.db` | SQLite database path |
