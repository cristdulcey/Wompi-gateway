# Wompi Webhook Gateway

A multi-company, multi-account Wompi webhook router and reliable delivery gateway built in Go (`Gin` + `PostgreSQL pgx/v5`).

When multiple products or companies (such as **Optimax** with `sub-*` references and **Mr. Waiters** with `mw-sub-*` references) share a single Wompi merchant account—or operate across multiple Wompi accounts—Wompi only allows configuring a single Events Webhook URL per account. This microservice acts as the central ingress point, verifies Wompi cryptographic signatures (`SHA256`), persists every raw event idempotently, routes events by reference prefix to the right downstream service, and guarantees delivery with exponential backoff, dead-letter tracking, and one-click manual retries.

---

## Key Features

- **Multi-Account Wompi Ingress**: Receive webhooks for any number of Wompi accounts (`prod` or `test`) via `/webhooks/wompi/:account_slug`.
- **Dynamic SHA256 Signature Verification**: Parses `signature.properties` dynamically from the Wompi payload using `json.UseNumber()` (preventing float distortion on `amount_in_cents`), concatenates property values + `timestamp` + `events_secret`, and verifies the `SHA256` digest in constant time against `X-Event-Checksum` and `signature.checksum`.
- **Longest-Prefix Reference Routing**: Matches `data.transaction.reference` against active `RouteRule`s ordered by longest prefix first (`sub-vip-` before `sub-`, `mw-sub-` distinct from `sub-`), then by priority, with optional catch-all fallback (`*` or `""`).
- **Exact Raw Payload Forwarding**: Forwards the exact byte-for-byte JSON payload and `X-Event-Checksum` header to the target company endpoint so downstream services can also run standard Wompi signature verification if desired.
- **Persistent Queue, Exponential Backoff & Manual Retry**:
  - Uses PostgreSQL `FOR UPDATE SKIP LOCKED` for horizontal scalability.
  - Retries failed deliveries (`30s`, `2m`, `10m`, `1h`, `6h` with jitter) up to `max_retries` before moving to `dead_letter`.
  - Records every HTTP attempt (`status_code`, `response_body`, `error_message`, `duration_ms`).
  - Supports instant manual retry (`POST /api/v1/deliveries/:id/retry`) and re-routing of unrouted events (`POST /api/v1/events/:id/route`).
- **Embedded Admin Console**: Built-in single-page dashboard at `GET /admin` to monitor stats, inspect raw payloads and HTTP attempts, trigger retries, and manage Companies, Wompi Accounts, and Prefix Routes.

---

## Architecture

```
cmd/server/main.go                  # Entrypoint, config, auto-migrate, worker & HTTP lifecycle
internal/
  config/config.go                  # Environment variable loader
  domain/
    entities.go                     # Core entities (Company, WompiAccount, RouteRule, WebhookEvent, etc.)
    repository.go                   # Persistence port interface
  wompi/
    verifier.go                     # Wompi JSON parser (UseNumber) & SHA256 signature verifier
  dispatcher/
    router.go                       # Longest-prefix + priority route matcher
    dispatcher.go                   # Concurrent delivery worker & exponential backoff engine
  repository/
    postgres/                       # PostgreSQL pgx/v5 adapter + embedded SQL migrations
    memory/                         # Thread-safe in-memory adapter for tests & local dev
  http/
    router.go                       # Gin router & embedded /admin UI
    handlers.go                     # Public webhook handler & Admin REST API v1
    middleware.go                   # Constant-time Admin API Key authentication
    ui/index.html                   # Embedded single-page Admin Console
```

---

## Quick Start

### 1. Run with Docker Compose

```bash
cp .env.example .env
docker compose up --build -d
```

Open the Admin Console at [http://localhost:8080/admin](http://localhost:8080/admin) and enter your `ADMIN_API_KEY`.

### 2. Example Configuration via REST API

```bash
export ADMIN_API_KEY="change-me-in-production"
export GATEWAY_URL="http://localhost:8080"

# 1. Create Companies
OPTIMAX_ID=$(curl -s -X POST "$GATEWAY_URL/api/v1/companies" \
  -H "X-API-Key: $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"Optimax","slug":"optimax"}' | jq -r '.id')

MRWAITERS_ID=$(curl -s -X POST "$GATEWAY_URL/api/v1/companies" \
  -H "X-API-Key: $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"Mr. Waiters","slug":"mr-waiters"}' | jq -r '.id')

# 2. Register Wompi Account
ACCOUNT_ID=$(curl -s -X POST "$GATEWAY_URL/api/v1/accounts" \
  -H "X-API-Key: $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Wompi Main Production",
    "slug": "main-prod",
    "environment": "prod",
    "events_secret": "prod_events_your_wompi_secret_here"
  }' | jq -r '.id')

# 3. Create Reference Prefix Route Rules
# Route "sub-" references to Optimax
curl -s -X POST "$GATEWAY_URL/api/v1/routes" \
  -H "X-API-Key: $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"wompi_account_id\": \"$ACCOUNT_ID\",
    \"company_id\": \"$OPTIMAX_ID\",
    \"name\": \"Optimax Subscriptions\",
    \"reference_prefix\": \"sub-\",
    \"target_url\": \"https://api.optimax.com/webhooks/wompi\",
    \"priority\": 10,
    \"max_retries\": 5
  }"

# Route "mw-sub-" references to Mr. Waiters
curl -s -X POST "$GATEWAY_URL/api/v1/routes" \
  -H "X-API-Key: $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"wompi_account_id\": \"$ACCOUNT_ID\",
    \"company_id\": \"$MRWAITERS_ID\",
    \"name\": \"Mr. Waiters Subscriptions\",
    \"reference_prefix\": \"mw-sub-\",
    \"target_url\": \"https://api.mrwaiters.com/webhooks/wompi\",
    \"priority\": 10,
    \"max_retries\": 5
  }"
```

Configure Wompi's Events URL for this account to:
```
https://your-gateway-domain.com/webhooks/wompi/main-prod
```

---

## Testing

```bash
go test -v -race ./...
```
