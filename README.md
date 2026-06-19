# URL Shortener

A URL shortening service built in Go, with SQLite persistence, an optional Redis cache layer, rate limiting, and SSRF-aware validation.

**Live demo:** `http://13.50.106.60:18080` ([health check](http://13.50.106.60:18080/health))

---

## Overview

This service exposes two JSON endpoints:

- **Encode** — turn a long `http(s)://` URL into a short 6-character code.
- **Decode** — turn a short code back into the original URL.

Key properties:

- **Idempotent** — encoding the same URL twice returns the same short code.
- **Collision-resistant** — codes are generated with `crypto/rand` over a 62^6 (~56 billion) keyspace, with retry on collision.
- **SSRF-aware** — resolves the hostname and rejects URLs pointing at private/loopback IP ranges before saving.
- **Persistent** — backed by SQLite, so previously encoded URLs survive a restart.
- **Optional caching** — if `REDIS_URL` is set, decode reads go through Redis first; if Redis is unavailable at startup or at request time, the service degrades to SQLite-only and keeps working.

---

## Architecture

```
cmd/server/main.go        Entry point — config, wiring, graceful shutdown
internal/
  handler/                Gin HTTP handlers (encode, decode, health)
  service/shortener.go    Business logic — validation, idempotency, collision retry, cache-aside
  storage/sqlite.go       SQLite persistence
  cache/                  Redis cache (optional — Cache interface, nil-safe in the service)
  middleware/             Security headers, rate limiting, request ID, request logging
  migrate/                Minimal versioned SQL migration runner
  model/url.go            URL domain model
pkg/base62/                Cryptographically random Base62 code generator
migrations/001_init.sql    Initial schema
```

Request flow:

```
Client → Gin router → Security headers → Request ID → Rate limiter → Request logger
       → Handler → Service (validate / dedupe / cache-aside) → SQLite (+ Redis if configured)
```

This is a straightforward layered architecture — handler → service → storage — rather than something like hexagonal/ports-and-adapters. The business logic here (validate, generate, persist) is simple enough that the extra indirection of a hexagonal layout wouldn't pay for itself; layered keeps the same testability (everything is injected via interfaces) with less ceremony.

---

## Getting Started

### Prerequisites

- Go 1.25+ (for running locally without Docker)
- Docker and Docker Compose (recommended)

### Run with Docker Compose

```bash
git clone https://github.com/abdelrhman-elsbagh/go-url-shortener.git
cd go-url-shortener

docker compose up --build
```

This starts the app (port `18080` on the host, mapped to `8080` in the container) and a Redis instance. The app works without Redis too — see [Environment Variables](#environment-variables).

```bash
# detached
docker compose up --build -d

# logs
docker compose logs -f app

# stop
docker compose down
```

### Run locally without Docker

```bash
go mod download
go run ./cmd/server
```

By default this listens on `:8080` and writes the SQLite file to `./data/urls.db`. Without `REDIS_URL` set, the service runs SQLite-only — no Redis required to develop or test locally.

---

## Deploying to a fresh server (e.g. AWS EC2, Ubuntu)

This is the exact path used for the live demo, on a plain Ubuntu EC2 instance with nothing pre-installed.

### 1. SSH into the server

```bash
ssh -i your-key.pem ubuntu@your-server-ip
```

### 2. Install Docker

```bash
sudo apt update && sudo apt upgrade -y
sudo apt install -y ca-certificates curl gnupg

sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# optional: run docker without sudo
sudo usermod -aG docker $USER && newgrp docker
```

### 3. Clone and deploy

```bash
git clone https://github.com/abdelrahmantarek/go-url-shortener
cd go-url-shortener
docker compose up --build -d
```

### 4. Open the port in AWS

The app listens on host port `18080` (see `docker-compose.yml`). AWS Security Groups block all inbound traffic except SSH by default, so this port has to be opened explicitly:

1. EC2 Console → select the instance → **Security** tab → click the attached Security Group
2. **Inbound rules** → **Edit inbound rules** → **Add rule**
3. Type: `Custom TCP`, Port range: `18080`, Source: `0.0.0.0/0` (or a specific IP/CIDR to restrict access)
4. Save

### Verify

From the server itself:
```bash
curl http://localhost:18080/health
```

From your own machine, using the instance's public IP:
```bash
curl http://13.50.106.60:18080/health
```

Both should return:
```json
{"status":"ok","version":"1.0.0"}
```

---

## API Documentation

### POST /api/v1/encode

```bash
curl -X POST http://localhost:18080/api/v1/encode \
  -H "Content-Type: application/json" \
  -d '{"url": "https://www.example.com/very/long/path?query=1"}'
```

**200 OK**
```json
{
  "short_url": "http://localhost:18080/Xk92mP",
  "short_code": "Xk92mP",
  "original_url": "https://www.example.com/very/long/path?query=1",
  "created_at": "2026-06-19T10:30:00Z"
}
```

| Status | Code | Reason |
|--------|------|--------|
| 400 | `INVALID_URL` | Empty body, malformed URL, or disallowed scheme (`javascript:`, `data:`, anything non-http(s)) |
| 422 | `INVALID_URL` | URL resolves to a private/loopback IP (SSRF check) |
| 429 | `RATE_LIMIT_EXCEEDED` | Too many requests from this IP |
| 500 | `INTERNAL_ERROR` | Unexpected server-side failure |

### POST /api/v1/decode

```bash
curl -X POST http://localhost:18080/api/v1/decode \
  -H "Content-Type: application/json" \
  -d '{"short_code": "Xk92mP"}'
```

**200 OK**
```json
{
  "original_url": "https://www.example.com/very/long/path?query=1",
  "short_code": "Xk92mP",
  "created_at": "2026-06-19T10:30:00Z",
  "click_count": 42
}
```

| Status | Code | Reason |
|--------|------|--------|
| 400 | `INVALID_URL` | Empty or missing `short_code` |
| 404 | `URL_NOT_FOUND` | Code does not exist |
| 410 | `URL_EXPIRED` | URL has passed its expiry, if one was set |

### GET /health

```bash
curl http://localhost:18080/health
```

```json
{ "status": "ok", "version": "1.0.0" }
```

### Error envelope

Every error response uses the same shape:

```json
{
  "error": {
    "code": "INVALID_URL",
    "message": "invalid URL",
    "details": "only http/https allowed, got \"ftp\""
  }
}
```

---

## Security Considerations

### Identified attack vectors and mitigations

1. **SSRF (Server-Side Request Forgery)** — a malicious URL could point at an internal service (`http://169.254.169.254/`, `http://localhost:6379`, etc.) and trick the service into fetching or exposing internal resources on decode/redirect. Mitigated by resolving the hostname via DNS at encode time and rejecting any result that falls inside `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, `::1/128`, `fc00::/7`, or `fe80::/10`.

2. **Scheme injection** — `javascript:` or `data:` URIs could be stored and later rendered somewhere unsafe by a client. Only `http` and `https` schemes are accepted; `javascript:` and `data:` are rejected before parsing.

3. **Brute-force / enumeration** — an attacker could hammer `/encode` or scan `/decode` for valid codes. A per-IP token-bucket rate limiter (default 10 req/s, burst 20) returns `429` with a `Retry-After` header. Codes are drawn from a 62^6 (~56 billion) keyspace via `crypto/rand`, so guessing a valid code by brute force is impractical at this scale.

4. **Oversized payloads / DoS** — request bodies are capped at 1 MB, and URLs longer than 2048 characters are rejected before any DB work happens.

5. **Clickjacking / MIME sniffing / content injection** — standard security headers are set on every response: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none'`, `Referrer-Policy: strict-origin-when-cross-origin`.

6. **Spoofed client IP for rate limiting** — the rate limiter trusts `X-Forwarded-For`/`X-Real-IP` if present, falling back to the socket address. Behind a trusted reverse proxy this is correct; exposed directly to the internet, an attacker could forge these headers to bypass per-IP limits. Worth tightening (only trust these headers from a known proxy CIDR) before a public-facing deployment.

### Not implemented (acknowledged trade-off)

URLs are stored as plain text in SQLite (and in Redis, if enabled). For this assignment the original URLs aren't sensitive, so this is an acceptable trade-off. In a deployment where the underlying URLs might carry sensitive query parameters (session tokens, personal identifiers), `original_url` should be encrypted at rest with AES-GCM before being persisted, decrypting only at read time.

---

## Scalability Approach

### Current implementation

Single SQLite file, single process. Reads can optionally go through Redis first (cache-aside: check Redis, fall back to SQLite on a miss, populate Redis on the way back). This is enough for low-to-moderate traffic and is intentionally simple — the assignment doesn't call for a scaled-out service, so the goal here was a correct, well-understood baseline rather than premature infrastructure.

### The collision problem

Short codes are 6 random Base62 characters (`crypto/rand`), giving a keyspace of 62^6 ≈ 56.8 billion. Before saving, the service checks whether the generated code already exists and retries (up to 5 times) on a collision. At 1 million stored URLs, the chance of any single attempt colliding is roughly 1 in 56,000 — and a failed attempt just costs one retry, not a wasted request.

As the dataset grows well beyond that, the random-and-check approach starts doing more retries, and at very large scale that's the first thing to change:

- **Counter-based generation**: feed a monotonically increasing ID (e.g., a database sequence, or a Redis `INCR`) directly into `base62.Encode`, which deterministically maps a number to a Base62 string. This eliminates collisions entirely — no check, no retry — at the cost of codes becoming sequential/guessable, which would need to be weighed against the brute-force mitigation above.
- **Pre-generated code pool**: generate and store a large batch of unused codes ahead of time, and hand them out atomically; avoids doing crypto/rand + a DB lookup on the hot path.

### If this had to scale further

- **Swap SQLite for PostgreSQL** — the schema doesn't need to change; only the `database/sql` driver does. SQLite is a single-writer store, which is the main reason it wouldn't hold up under heavy concurrent writes.
- **Cache is already pluggable** — the `Cache` interface means a Redis cluster, or a different cache entirely, can be swapped in without touching the service logic.
- **Separate read/write paths** — `/encode` is write-heavy but low-volume relative to `/decode`, which is read-heavy. A read replica (or the Redis cache doing most of the work, as it already does) keeps decode latency low without scaling writes unnecessarily.
- **Stateless app tier** — the only per-process state is the in-memory rate limiter's bucket map. If Redis is configured, the Redis-backed rate limiter is used instead, which makes the whole app tier horizontally scalable behind a load balancer.

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|--------------|
| `APP_ENV` | `development` | `development` or `production` — affects log format (text vs JSON) and Gin's mode |
| `APP_PORT` | `8080` | HTTP listen port |
| `APP_BASE_URL` | `http://localhost:{APP_PORT}` | Prefix used to build `short_url` in encode responses |
| `DB_PATH` | `./data/urls.db` | SQLite file path |
| `RATE_LIMIT_RPS` | `10` | Allowed requests per second per IP |
| `RATE_LIMIT_BURST` | `20` | Burst allowance for the rate limiter |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `REDIS_URL` | _(unset)_ | Optional. If set (e.g. `redis:6379`), enables the Redis cache and Redis-backed rate limiter. If unreachable at startup, the service logs a warning and runs without it. |

---

## Running Tests

```bash
# all tests
go test ./...

# verbose
go test ./... -v

# coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html

# a single package
go test ./internal/service/... -v
```

Tests cover: Base62 generation (length, charset, uniqueness, known values), SQLite storage (CRUD, not-found, duplicates, expiry), the service layer (valid encode, idempotency, bad scheme, localhost/private-IP rejection, oversized URL, expired decode, cache hit/miss/degradation), and handler-level integration tests for both endpoints, security headers, request ID propagation, and rate limiting.