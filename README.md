# Metery

Self-hosted usage-billing and entitlements backend.

## Quick start

```bash
export API_KEYS="mtr_your_secret_key"
export DATABASE_URL="sqlite://./metery.db"

go run ./cmd/metery serve --migrate
```

Integrate with your app to check customer access and ingest usage events.

## Environment variables

| Variable | Description | Default |
|---|---|---|
| `DATABASE_URL` | Database connection string (see below) | `file:metery.db` |
| `API_KEYS` | Comma-separated list of Bearer tokens for API authentication | required |
| `MIGRATE` | Run migrations on startup when `true` | — |
| `HOSTNAME` | Public base URL — injected into the served OpenAPI spec | `http://localhost:8080` |

## Database

Driver is auto-detected from `DATABASE_URL`:

| DSN | Database |
|---|---|
| `sqlite://./metery.db` or `file:metery.db` | SQLite (good for dev) |
| `postgres://user:pass@host/db` | PostgreSQL (production) |

SQLite serialises all writers — use PostgreSQL for any concurrent workload.

## CLI

```
metery serve [--migrate]  # start the server, optionally run migrations first
metery worker             # run the recurrence worker process
metery migrate            # run database migrations and exit
metery version            # print version
metery help               # print usage
```

## Authentication

### API (Bearer token)

All RPC endpoints require `Authorization: Bearer <key>`. 

Keys are provisioned at startup via the `API_KEYS` environment variable. A valid key has full administrative access across the platform.

```bash
export API_KEYS="mtr_secret1,mtr_secret2"
```

## Protocols

A single endpoint serves all protocols via [vanguard-go](https://github.com/connectrpc/vanguard-go) transcoding:

| Protocol | Transport |
|---|---|
| gRPC | HTTP/2 (h2c) |
| gRPC-Web | HTTP/1.1 or HTTP/2 |
| Connect | HTTP/1.1 or HTTP/2 |
| REST | HTTP/1.1 or HTTP/2 |

## Core flows

1. **Setup:** Define a **Meter** (how to aggregate events) and a **Feature** (billing capability wrapped around a meter).
2. **Provision:** Create a **Customer** and give them an **Entitlement** to the feature.
3. **Grant:** Issue **Grants** (credits) to the entitlement.
4. **Consume:** The app verifies access (`GetEntitlementValue`) and ingests raw usage (`IngestEvent`). 

Meters aggregate raw usage which is automatically deducted from active grants.

### Examples

```bash
# 1. Create a customer
curl -X POST http://localhost:8080/v1/customers \
  -H "Authorization: Bearer mtr_XXXXXX" \
  -H "Content-Type: application/json" \
  -d '{"key": "user_123", "name": "Acme Corp"}'

# 2. Define a meter & feature
curl -X POST http://localhost:8080/v1/meters \
  -H "Authorization: Bearer mtr_XXXXXX" \
  -H "Content-Type: application/json" \
  -d '{"slug": "api_calls", "name": "API calls", "aggregation": "count", "event_type": "api_call"}'

curl -X POST http://localhost:8080/v1/features \
  -H "Authorization: Bearer mtr_XXXXXX" \
  -H "Content-Type: application/json" \
  -d '{"slug": "api_calls", "name": "API calls", "meter_slug": "api_calls"}'

# 3. Provision entitlement & grant credits
curl -X POST http://localhost:8080/v1/customers/user_123/entitlements \
  -H "Authorization: Bearer mtr_XXXXXX" \
  -H "Content-Type: application/json" \
  -d '{"feature_id_or_slug": "api_calls"}'

curl -X POST http://localhost:8080/v1/customers/user_123/entitlements/api_calls/grants \
  -H "Authorization: Bearer mtr_XXXXXX" \
  -H "Content-Type: application/json" \
  -d '{"amount": "1000", "effective_at": "2026-05-01T00:00:00Z"}'

# 4. Check access & ingest usage
curl http://localhost:8080/v1/customers/user_123/entitlements/api_calls/value?cost=1 \
  -H "Authorization: Bearer mtr_XXXXXX"

curl -X POST http://localhost:8080/v1/events \
  -H "Authorization: Bearer mtr_XXXXXX" \
  -H "Content-Type: application/json" \
  -d '{"id": "req_abc123", "customer": "user_123", "type": "api_call", "time": "2026-05-08T10:00:00Z"}'
```

Connect and gRPC clients call the same endpoints under `/metery.v1.*Service/<RPC>` with appropriate protocol headers.

## Worker

Grants can be configured to recur periodically (e.g., monthly resets). The recurrence worker scans and emits new grants automatically.

Deploy as a long-lived process alongside the server:

```bash
metery worker
```

On request-based platforms (Cloud Run, etc.) where a persistent process isn't practical, trigger a single pass over HTTP instead:

```bash
curl -X POST http://localhost:8080/worker/run
```

## License

Apache 2.0
