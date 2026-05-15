# Metery — Design

Status: draft v0
Owner: vh
Last updated: 2026-05-07

## 1. Overview

Metery is a usage-billing / entitlements backend.
Integrated apps ask Metery two questions:

1. **Can this customer perform this action?** (entitlement check)
2. **Record that this customer's raw event happened.** (event ingest)

Five core concepts:

- **Customer** — the billable / addressable entity. Caller creates first.
- **Meter** — defines how raw events are aggregated into a metric value
  (count, sum, avg, etc.) — server-side aggregation.
- **Feature** — a named billing capability backed by a meter (metered)
  or simply yes/no access (boolean).
- **Entitlement** — a `(customer, feature)` access record with optional
  usage-period config.
- **Grant** — credits added to a metered entitlement, with priority,
  expiration, recurrence, and rollover.

Raw usage events flow in through `IngestEvent`; the meter associated
with each metered feature aggregates them into usage that draws down
grant credits. Subscriptions (v1+) top balances up via recurring grants
and/or period resets.

## 2. Goals (v0)

- **Customers** are first-class — caller creates them before granting
  entitlements or ingesting events. Dual ID: server-generated `id`
  (ULID, lowercase Crockford base32) for our stable internal handle;
  caller-assigned `key` (opaque) for natural references. Matches the
  common usage-billing convention.
- **Meters** define server-side aggregation from raw events:
  `aggregation` (`count` / `sum` / `avg` / `min` / `max` / `unique_count`),
  `event_type` filter, optional `value_property` JSON path.
- **Features** are billing capabilities. `meter_slug` non-empty ⇒ metered
  (uses meter for usage); `meter_slug` empty ⇒ boolean (entitlement
  existence is the access bit).
- Per-customer **entitlements** scoped to a feature, with optional
  periodic reset (metered only).
- **Grants** that add credits to a metered entitlement, with priority,
  expiration, recurrence, and rollover at period boundary.
- Two read paths (uniform API, dispatched by feature kind):
  - `has_access(customer, feature [, cost])` → bool
  - `value(customer, feature)` → metered: balance + period window;
    boolean: has_access.
- One write path:
  - `ingest_event(id, customer, type, time, payload)` — raw event;
    server aggregates per the relevant meter. `time` is optional;
    server defaults to ingest time when absent.
- Stack: Go + Postgres (SQLite for tests / single-tenant dev). Aggregation
  is plain Postgres queries; no streaming infra in v0.
- Idempotent event ingestion (PK on caller-assigned `id`).

## 3. Non-goals (v0, revisit later)

- **Atomic check-and-deduct.** Eventual consistency; a customer can briefly
  go negative under concurrent spend. Acceptable per current requirements.
- **First-class `Plan` / `Subscription` concept.** v1+ vision — including
  Stripe (and other) sync. v0 only exposes ledger primitives (entitlements
  + grants); a billing adapter drives them externally.
- **Static entitlements** (typed config values like `seats: 5`,
  `rate_limit: 100`). Deferred — v0 covers metered + boolean only.
- **Time-bounded boolean entitlements** (e.g. trial access until a date).
  v0 boolean is just "row exists / doesn't"; lifecycle is managed by the
  caller via add/remove. Add `expires_at` later if needed.
- Real-time streaming aggregation (no Kafka / ClickHouse). Postgres only.
- Pricing / invoicing.
- Multi-tenant (single tenant assumed; can be added as a column later).
- Key management API (CreateApiKey / ListApiKeys / RevokeApiKey) +
  scopes / RBAC + rate limiting. v0 ships basic bearer-token auth
  (env-provisioned keys); management API and finer access controls
  land in v1+.
- Webhooks / outbound notifications.
- Historical re-pricing or backfill from raw event streams.

See [roadmap.md](roadmap.md) for the v1 / v1+ tiers where these land.

## 4. Mental model

Capabilities of the integrated app are exposed as **features**. Metered
features sit on top of **meters** which aggregate raw events into a
metric value. Boolean features are pure yes/no access checks.

| Concept         | What it is                                                      |
|-----------------|-----------------------------------------------------------------|
| **Customer**    | First-class billable entity. Server-generated `id` (ULID, lowercase Crockford) + caller-assigned `key` (opaque, unique). What other resources reference. |
| **Meter**       | Server-side aggregation definition: `aggregation` (count/sum/…), `event_type` filter, `value_property` JSON path. Multiple features can wrap one meter. |
| **Feature**     | Billing capability. `meter_slug` set ⇒ metered (uses meter for usage); empty ⇒ boolean (entitlement existence is the access bit). |
| **Entitlement** | A `(customer, feature)` access record. For metered features, also carries usage-period config. |
| **Grant**       | Credits added to one *metered* entitlement. Has priority, expiry, recurrence, rollover. |
| **Usage event** | Append-only raw observation: `id`, `customer`, `type`, `time`, `payload`. Server aggregates via meter. |
| **Ledger**      | The set of grants + usage events for metered features. Balance is derived: grants minus aggregated usage. |

**Worked example — a Pro subscriber:**

| Meter           | Aggregation | event_type   | value_property |
|-----------------|-------------|--------------|----------------|
| `api_calls`     | count       | `api_call`   | —              |
| `tokens`        | sum         | `llm_call`   | `$.tokens`     |

| Feature              | meter_slug   | Grant / Value                  |
|----------------------|--------------|--------------------------------|
| `api_calls`          | `api_calls`  | 10,000 / month, recurring      |
| `tokens`             | `tokens`     | 1,000,000 / month, recurring   |
| `priority_support`   | empty (boolean) | granted (entitlement exists)|

The integrated app emits raw events with the appropriate `type` and
`payload`. The relevant meter aggregates them; the feature's grants
draw down. *Same event can drive multiple meters/features* — e.g. an
`llm_call` event with `tokens: 1500` could feed both a `tokens` meter
(SUM of `$.tokens`) and a separate `llm_calls_count` meter (COUNT).

**Invariant.** For any entitlement at time `T`:

```
balance(T) = Σ active_grant_amount(T)  −  Σ usage_in_current_period(T)
```

with grants consumed in priority order (lower priority number burns first),
ties broken by `effective_at`.

## 5. Grants & resets

**Grant fields:**

- `amount` — credits granted.
- `priority` — burn order; lower = consumed first. Default `100`.
- `effective_at` — when the grant becomes spendable.
- `expiration.duration` — how long after `effective_at` the grant remains
  valid. Unconsumed credits past expiry are forfeit.
- `recurrence.interval` + `recurrence.anchor` — auto-emit a new grant on
  schedule (e.g. monthly subscription top-up).
- `rollover.max_amount` — max credits that survive an entitlement reset.
- `rollover.type` — `"original"` (cap at original grant size) or
  `"remaining"` (cap at what's still unused).

**Entitlement reset:**

An entitlement may declare a `usage_period` (ISO-8601 duration, e.g. `P1M`)
and an `anchor` timestamp (typically the subscription start). At each
period boundary:

1. Usage counter for the new period starts at zero (we don't count events
   from before the boundary against the new balance).
2. Each active grant's surviving amount is computed via its rollover
   policy. Anything beyond `rollover.max_amount` is voided.
3. Recurring grants emit a new grant for the new period, governed by their
   own `recurrence` config (independent of the entitlement reset).

Resets can also be **triggered manually** via API (e.g. plan upgrade).

**Subscription mapping (forward-looking).**
A "Pro plan: 10k api_calls/month + 1M tokens/month" subscription becomes
**two entitlements** for the customer:

- `(customer, api_calls)` with `usage_period = P1M`, anchored to
  subscription start, plus a recurring grant of `amount = 10_000`,
  `recurrence = P1M`, `expiration = P1M`, `rollover.max_amount = 0`.
- `(customer, tokens)` with the same period config, plus a recurring grant
  of `amount = 1_000_000`, same recurrence/expiration/rollover.

Add-on packs (e.g. "buy 5k extra api_calls") become non-recurring grants
on the corresponding entitlement, with their own expiration and a
priority chosen so they burn before or after the monthly allowance —
caller's choice.

## 6. Core flows

The wire protocol is **ConnectRPC** (see `proto/metery/v1/`). Every RPC
has two equivalent HTTP entry points (transcoded at the server):

- **Connect URL** (canonical): `POST /metery.v1.<Service>/<Method>` —
  used by Connect / gRPC clients. Both binary and JSON payloads.
- **REST URL** (transcoded): conventional REST shape declared via
  `google.api.http` annotation in the proto.

Examples below use the REST shape since it's more readable; field names
in payloads match the proto.

### Endpoint reference

| Method | REST                                                                              | Connect RPC                                  |
|--------|-----------------------------------------------------------------------------------|----------------------------------------------|
| POST   | `/v1/customers`                                                                   | `CustomerService/CreateCustomer`             |
| GET    | `/v1/customers`                                                                   | `CustomerService/ListCustomers`              |
| GET    | `/v1/customers/{id_or_key}`                                                       | `CustomerService/GetCustomer`                |
| PATCH  | `/v1/customers/{id_or_key}`                                                       | `CustomerService/UpdateCustomer`             |
| DELETE | `/v1/customers/{id_or_key}`                                                       | `CustomerService/DeactivateCustomer`         |
| POST   | `/v1/meters`                                                                      | `MeterService/CreateMeter`                   |
| GET    | `/v1/meters`                                                                      | `MeterService/ListMeters`                    |
| GET    | `/v1/meters/{id_or_slug}`                                                         | `MeterService/GetMeter`                      |
| DELETE | `/v1/meters/{id_or_slug}`                                                         | `MeterService/ArchiveMeter`                  |
| POST   | `/v1/features`                                                                    | `FeatureService/CreateFeature`               |
| GET    | `/v1/features`                                                                    | `FeatureService/ListFeatures`                |
| GET    | `/v1/features/{id_or_slug}`                                                       | `FeatureService/GetFeature`                  |
| DELETE | `/v1/features/{id_or_slug}`                                                       | `FeatureService/ArchiveFeature`              |
| POST   | `/v1/customers/{customer_id_or_key}/entitlements`                                 | `EntitlementService/CreateEntitlement`       |
| GET    | `/v1/customers/{customer_id_or_key}/entitlements`                                 | `EntitlementService/ListEntitlements`        |
| GET    | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}`            | `EntitlementService/GetEntitlement`          |
| DELETE | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}`            | `EntitlementService/DeleteEntitlement`       |
| GET    | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}/value`      | `EntitlementService/GetEntitlementValue`     |
| POST   | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}/reset`      | `EntitlementService/ResetEntitlement`        |
| POST   | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}/grants`     | `GrantService/CreateGrant`                   |
| GET    | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}/grants`     | `GrantService/ListGrants`                    |
| DELETE | `/v1/grants/{id}`                                                                 | `GrantService/VoidGrant`                     |
| POST   | `/v1/events`                                                                      | `EventService/IngestEvent`                   |

### 6.0 v0 minimal happy path

End-to-end use of Metery, no subscriptions, no recurrence — *"customer
exists, give them 1000 api-call credits, check, ingest"*:

```
# 1. Create the customer (once, at signup)
POST /v1/customers
{ "key": "user_123", "name": "Acme Corp" }

# 2. Define the meter (once, admin)
POST /v1/meters
{ "slug": "api_calls", "name": "API calls",
  "aggregation": "count", "event_type": "api_call" }

# 3. Define the feature backed by that meter (once, admin)
POST /v1/features
{ "slug": "api_calls", "name": "API calls", "meter_slug": "api_calls" }
# server resolves slug → meter id for the DB FK (wire exposes meter_slug)

# 4. Provision an entitlement for the customer (once)
POST /v1/customers/user_123/entitlements
{ "feature_id_or_slug": "api_calls" }
# no usage_period ⇒ never resets

# 5. Grant credits manually (one-time, no recurrence/expiration)
POST /v1/customers/user_123/entitlements/api_calls/grants
{ "amount": "1000" }

# 6. Hot path: check + ingest on every action
GET  /v1/customers/user_123/entitlements/api_calls/value?cost=1

POST /v1/events
{ "id": "req_abc123", "customer": "user_123",
  "type": "api_call", "time": "..." }   # time optional; defaults to now
```

For a **boolean** feature the flow skips the meter and grant:

```
POST /v1/features
{ "slug": "priority_support", "name": "Priority support" }
# meter_slug omitted ⇒ boolean feature

POST /v1/customers/user_123/entitlements
{ "feature_id_or_slug": "priority_support" }
# row exists ⇒ has access

GET  /v1/customers/user_123/entitlements/priority_support/value
→ { "has_access": true }

# Revoke:
DELETE /v1/customers/user_123/entitlements/priority_support
```

(Path params: `customer_id_or_key` and `feature_id_or_slug` are bound
from the URL and omitted from request body when present in the path.)

> **Note on JSON int64.** protojson serialises `int64` as a JSON string
> (`"1000"` not `1000`) to avoid JavaScript precision loss. Caller
> libraries handle this transparently.

Everything below (recurrence, expiration, rollover, periodic reset) is
optional and only needed when you graduate to subscription-style billing.

### 6.1 Define a meter and feature (one-time setup, admin)

Meter (server-side aggregation):

```
POST /v1/meters
{
  "slug":           "tokens",
  "name":           "LLM tokens",
  "aggregation":    "sum",
  "event_type":     "llm_call",
  "value_property": "$.tokens"
}
→ { "id": "<ulid>", "slug": "tokens", ... }
```

Feature (billing wrapper):

```
# Metered — backed by a meter (caller passes slug; server resolves to id)
POST /v1/features
{ "slug": "tokens", "name": "Tokens", "meter_slug": "tokens" }

# Boolean — meter_slug omitted
POST /v1/features
{ "slug": "priority_support", "name": "Priority support" }
```

### 6.2 Provision an entitlement (per customer, per feature)

```
POST /v1/customers/user_123/entitlements
{
  "feature_id_or_slug": "tokens",
  "usage_period": { "duration": "P1M", "anchor": "2026-05-01T00:00:00Z" }
}
```

### 6.3 Grant credits

```
POST /v1/customers/user_123/entitlements/tokens/grants
{
  "amount":       "1000000",
  "priority":     100,
  "effective_at": "2026-05-01T00:00:00Z",
  "expiration":   { "duration": "P1M" },
  "recurrence":   { "interval": "P1M", "anchor": "2026-05-01T00:00:00Z" },
  "rollover":     { "max_amount": "0", "type": "remaining" }
}
```

### 6.4 Check access (hot path, called by integrated app's cost center)

Metered — pass `cost` as a query param; full ledger snapshot comes back
(REST response is unwrapped via `response_body: "value"`):

```
GET /v1/customers/user_123/entitlements/tokens/value?cost=1500
→ {
  "has_access":   true,
  "balance":      "987200",
  "usage":        "12800",
  "overage":      "0",
  "usage_period": { "from": "2026-05-01T00:00:00Z", "to": "2026-06-01T00:00:00Z" },
  "last_reset":   "2026-05-01T00:00:00Z"
}
```

Boolean — `cost` is ignored; result is row existence:

```
GET /v1/customers/user_123/entitlements/priority_support/value
→ { "has_access": true }
```

The caller discriminates on **field presence**: metered responses
include `balance` (and friends); boolean responses include only
`has_access`. If the entitlement does not exist for the customer, the
response is `{ "has_access": false }`.

> **Note.** Connect / gRPC clients receive the wrapped form
> `{ "value": { ... } }` — `response_body` only affects the REST entry
> point.

### 6.5 Ingest a usage event (hot path)

```
POST /v1/events
{
  "id":          "req_abc123",
  "customer":    "user_123",
  "type":        "llm_call",
  "time":        "2026-05-08T10:11:12Z",
  "payload":     { "tokens": 1500, "model": "gpt-4" }
}
→ 200 OK
{}
```

Events are **raw observations**. The relevant meter (configured for the
feature whose entitlement is being checked) aggregates them into usage:
e.g. a `tokens` meter with `aggregation: sum` and `value_property: $.tokens`
adds `1500` to the customer's token usage.

The caller-assigned `id` is **both** the event identifier and the
idempotency key — replaying the same `id` is silently deduped at the
unique-PK level (also `200 OK {}`). UUIDs / ULIDs are conventional, but
any unique string ≤ 256 chars works.

The `customer` field accepts either `Customer.id` (ULID) or
`Customer.key` — server resolves and stores the key.

Server-side metrics track dedupe rate; if a caller needs visibility
into their replay frequency, that surfaces via dashboards rather than
the response body.

### 6.6 Manual reset (admin)

```
POST /v1/customers/user_123/entitlements/api_calls/reset
{ "at": "2026-05-08T00:00:00Z" }
→ {}
```

(Caller refetches `GET .../value` if they want to see the post-reset
balance.)

### Action verb response convention

`Reset`, `Archive`, `Void`, `Delete`, and `IngestEvent` return an empty
message (`200 OK {}` for REST; empty proto message for Connect/gRPC).
Use the matching `Get*` / `Value` endpoint to read state afterwards.

`Create*` is the only mutation that returns a body — the new resource —
because the server-assigned ULID isn't recoverable otherwise.

> Why 200 + `{}` not 204: keeps the REST and Connect surfaces shape-
> identical, lets clients call `response.json()` unconditionally, and
> matches the default transcoder behavior for empty messages.

### 6.7 Authentication

Every Metery endpoint (REST and Connect/gRPC) requires a bearer token
in the standard `Authorization` header:

```
Authorization: Bearer mtr_<random-32+-bytes-base64url>
```

**v0 model — env-provisioned keys.** Keys are configured at boot via
the `API_KEYS` env var, a comma-separated list of accepted
tokens:

```
API_KEYS=mtr_AbCdEf...,mtr_GhIjKl...
```

The server compares the incoming token (constant-time) against the
configured set. No DB lookup, no per-key audit trail, no scopes.
Every valid key has full admin access — appropriate for the v0
single-tenant assumption.

**Token shape.** `mtr_` prefix (so they're spottable in logs and
configs) plus a random 32+ byte suffix encoded as URL-safe base64. The
prefix is convention only; the server treats keys as opaque strings.

**Failure modes.**
- Missing `Authorization` header → `UNAUTHENTICATED` (HTTP 401).
- Invalid / unknown token → `UNAUTHENTICATED`. We deliberately do
  *not* distinguish "missing" from "invalid" to avoid token-existence
  enumeration.

**Implementation.** Auth runs as Connect/HTTP middleware before any
handler. Failures short-circuit with the standard error envelope;
successful auth threads no per-request identity into handlers in v0
(every valid call is full-admin).

**Forward path (v1).** DB-backed `api_keys` table (hashed storage,
named keys, revocation), plus a management API. v0 env keys migrate
over with a one-shot import. Scopes / RBAC / per-key rate limiting
are v1+. See [roadmap.md](roadmap.md).

## 7. Data model (Postgres)

```sql
-- IDs are 26-char lowercase ULIDs stored as TEXT — readable in psql /
-- dumps / logs at a ~10-byte-per-row cost vs binary. Migrate to bytea
-- later if storage pressure justifies.
CREATE TABLE customers (
  id              TEXT PRIMARY KEY,                     -- server-generated ULID
  key             TEXT NOT NULL UNIQUE,                 -- caller-assigned, immutable
  name            TEXT NOT NULL,
  metadata        JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  deactivated_at  TIMESTAMPTZ                           -- soft delete
);

CREATE TABLE meters (
  id              TEXT PRIMARY KEY,                     -- server-generated ULID
  slug            TEXT NOT NULL UNIQUE,                 -- caller-assigned, immutable
  name            TEXT NOT NULL,
  aggregation     TEXT NOT NULL CHECK (aggregation IN
                    ('count','sum','avg','min','max','unique_count')),
  event_type      TEXT NOT NULL,
  value_property  TEXT,                                 -- JSON path; null for count
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at     TIMESTAMPTZ
);
CREATE INDEX meters_by_event_type ON meters (event_type) WHERE archived_at IS NULL;

CREATE TABLE features (
  id              TEXT PRIMARY KEY,                     -- server-generated ULID
  slug            TEXT NOT NULL UNIQUE,                 -- caller-assigned, immutable
  name            TEXT NOT NULL,
  meter_id        TEXT REFERENCES meters(id),           -- NULL ⇒ boolean feature
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at     TIMESTAMPTZ
);

CREATE TABLE entitlements (
  id                       TEXT PRIMARY KEY,
  customer_id              TEXT NOT NULL REFERENCES customers(id),
  feature_id               TEXT NOT NULL REFERENCES features(id),
  usage_period_duration    TEXT,                        -- "P1M"; NULL = no reset
  usage_period_anchor      TIMESTAMPTZ,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at               TIMESTAMPTZ
);
CREATE UNIQUE INDEX entitlements_active_uniq
  ON entitlements (customer_id, feature_id) WHERE deleted_at IS NULL;

CREATE TABLE grants (
  id                  TEXT PRIMARY KEY,
  entitlement_id      TEXT NOT NULL REFERENCES entitlements(id),
  amount              BIGINT NOT NULL CHECK (amount > 0),
  priority            INT    NOT NULL DEFAULT 100,
  effective_at        TIMESTAMPTZ NOT NULL,
  expires_at          TIMESTAMPTZ,                      -- effective_at + expiration.duration
  recurrence_interval TEXT,                             -- "P1M" or NULL
  recurrence_anchor   TIMESTAMPTZ,
  rollover_max        BIGINT,                           -- NULL = no rollover
  rollover_type       TEXT,                             -- 'original' | 'remaining'
  parent_grant_id     TEXT REFERENCES grants(id),       -- recurring chain; internal — not exposed on the wire
  metadata            JSONB,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  voided_at           TIMESTAMPTZ                       -- soft-void
);
CREATE INDEX grants_by_entitlement_active
  ON grants (entitlement_id, priority, effective_at)
  WHERE voided_at IS NULL;

-- Server bookkeeping columns (`created_at`, `processed_at`) are
-- internal — not exposed on the wire.
CREATE TABLE usage_events (
  id              TEXT PRIMARY KEY,                     -- caller-assigned; PK ⇒ dedup
  customer        TEXT NOT NULL,                        -- resolved Customer.key
  type            TEXT NOT NULL,                        -- matches Meter.event_type
  time            TIMESTAMPTZ NOT NULL,                 -- caller may omit on ingest; API fills in now()
  payload         JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),   -- internal: row insert time
  processed_at    TIMESTAMPTZ                           -- internal: NULL until async pipeline rolls it up (v1+)
);
CREATE INDEX usage_events_lookup
  ON usage_events (customer, type, time);

-- Optional cache; rebuildable from the ledger.
CREATE TABLE balance_snapshots (
  entitlement_id  TEXT NOT NULL REFERENCES entitlements(id),
  as_of           TIMESTAMPTZ NOT NULL,                 -- typically a period boundary
  balance         BIGINT NOT NULL,
  per_grant_state JSONB NOT NULL,                       -- [{grant_id, remaining}, ...]
  PRIMARY KEY (entitlement_id, as_of)
);
```

**Notes**

- `usage_events` is append-only — no updates, no deletes. Corrections are
  separate negative-amount entries (deferred; v0 only positive amounts).
- `grants` are append-only except for `voided_at` (soft-void).
- `balance_snapshots` is purely a derived cache; if empty, balance is
  recomputed from the full ledger via meter aggregation.
- **Boolean features** (meter_id NULL) use only the `entitlements`
  table. `usage_period_duration` must be NULL; no rows in `grants` /
  `usage_events` / `balance_snapshots`. Validation enforced at the
  application layer.
- SQLite parity: ULID IDs are already `TEXT`, so no conversion needed
  there. Swap `BIGINT` for `INTEGER` and `JSONB` for `TEXT`. Keep schema
  otherwise identical via migrations gated by driver.

## 8. Balance computation

Given `(entitlement, T)`:

1. Resolve the meter from the feature: `meter = features.meter_id → meters`.
   Boolean features skip everything below — has_access is the row's
   existence (and `deleted_at IS NULL`).
2. Determine current usage period `[period_start, period_end)`:
   - If `usage_period_duration` is null → `[entitlement.created_at, +∞)`.
   - Else: align `T` against `usage_period_anchor` stepping by
     `usage_period_duration`.
3. **Aggregate usage from raw events** via the meter:
   ```sql
   -- For aggregation = sum:
   SELECT COALESCE(SUM((payload->>meter.value_property)::numeric), 0)
   FROM usage_events
   WHERE customer    = $1
     AND type        = meter.event_type
     AND time        >= period_start
     AND time        <  T;
   -- For aggregation = count: SELECT COUNT(*) ...
   ```
4. Load active grants: `voided_at IS NULL AND effective_at <= T AND
   (expires_at IS NULL OR expires_at > T)`.
5. Initial per-grant remaining = `amount`. If a snapshot exists at
   `period_start`, seed `per_grant_state` from it.
6. Apply aggregated usage in priority order
   `(priority asc, effective_at asc)`, deducting from each grant's
   remaining.
7. `balance = Σ per_grant_remaining`; `usage = aggregate result`;
   `overage = max(0, -balance)`.

Snapshots at period boundaries are written after each balance computation
and used to seed subsequent reads, bounding the event replay window to the
current period only.

## 9. Consistency & concurrency

- **Writes (events).** Single insert with unique idempotency key. No locks.
- **Reads (balance / has_access).** Read-only computation against the
  ledger. May briefly disagree with concurrent in-flight events.
- **No deduct-on-check** in v0 — explicitly deferred. When we add it, the
  natural shape is a row-locked insert into a per-entitlement counter
  table, or `SELECT ... FOR UPDATE` on the entitlement row before the
  event insert.
- **Period rollover** is computed on read; we do not need a scheduled job
  to "advance" entitlements.
- **Recurring grant emission**, however, *does* need a background worker
  because new grants must materialize as rows so they show up in queries.
  v0 approach: a periodic worker scans `grants WHERE recurrence_interval IS
  NOT NULL` and emits the next child grant when due. Idempotent via
  `(parent_grant_id, effective_at)` uniqueness.
- **Recurrence catchup on restart**: the worker ticks every minute and
  emits one child grant per tick. After a downtime spanning N missed
  periods, it takes N minutes to fully catch up. During that window,
  balance reads for affected entitlements are understated — the missing
  grants haven't materialised yet. Acceptable for v0. A startup sweep
  (emit all overdue children in a single pass before the ticker starts)
  would close this gap if needed.

## 10. Architecture (one paragraph)

A single Go service exposing a REST API, talking to Postgres. Three
internal packages: `entitlement` (domain types + balance computation),
`store` (Postgres / SQLite repository), `api` (HTTP handlers). A
background `recurrence` worker emits child grants on schedule. No queues,
no caches in v0.

```
┌──────────┐   HTTP    ┌──────────────────────────────┐    SQL    ┌──────────┐
│  client  │ ────────▶ │  metery (Go)                 │ ────────▶ │ postgres │
└──────────┘           │  api → entitlement → store   │           └──────────┘
                       │  recurrence worker (in-proc) │
                       └──────────────────────────────┘
```

## 11. Open questions

1. **Hot-path latency target?** `balance_snapshots` are implemented:
   each read seeds from the most recent period-boundary snapshot and only
   replays events within the current period. Pick a concrete p99 target
   before adding further optimisations (covering indexes, materialized
   rollups).
2. **Subscriptions: v1+ scope, deferred from v0.** Long-term goal is a
   first-class `Plan` + `Subscription` concept in Metery, with sync
   adapters to Stripe (and others). v0 stays at the primitive level —
   manual grants only. The integrated app or Stripe webhook code drives
   Metery via the grant API for now. Open question for v1: does Metery
   own subscription state directly, or does it stay in Stripe with
   Metery as a downstream materialized view? Likely the latter, but
   we'll decide when we get there.

### Resolved (recorded for context)

- **Credit unit precision.** Store amounts as `BIGINT`; integer-valued
  credits only. If fractional pricing is ever needed, scale at the
  feature level (`millitokens`, `micro-credits`, etc.) — not in the
  ledger.
- **Variable-cost actions.** Check `cost = max_expected` upfront, record
  actual after the operation. Pattern (a).
- **Refunds / corrections.** v0 supports positive grants only.
  Compensating positive grants cover the limited cases we have. First-
  class negative-amount events deferred to v1+ (see roadmap).
- **Customer namespace.** v0 is single-tenant — `Customer.key` is
  globally unique within the deployment. Multi-tenant deferred to v1+.
- **Boolean / static entitlements.** Boolean is in v0; `meter_slug`
  empty on Feature is the discriminator (no `type` field). Static
  entitlements deferred to v1+.

## 12. Testing

The balance ledger is the system's brain; the rest is plumbing. The
testing strategy follows from that.

**Principle 1: balance computation is tested first, against fixtures,
before any handlers exist.** Each fixture is a declarative scenario —
a set of grants and events plus an expected balance at `T`. The
`entitlement` package's compute function takes the inputs and is
asserted against the expected output. This forces us to nail priority
ordering, period rollover (anchor + duration math), rollover policy
(`original` vs `remaining`), and expiration before we wrap it in any
SQL. If the fixtures pass, everything above is plumbing.

**Principle 2: v0 runs tests against SQLite only.** Both drivers exist
in the store layer (SQLite for tests/dev, Postgres for prod), but the
test suite runs only against SQLite in-memory. Trade-off: instant
test feedback, zero contributor setup, no Docker — at the cost of
not catching Postgres-specific divergence (JSONB operators vs SQLite
`json_extract`, `timestamptz` semantics, partial-index planner
behavior). The biggest divergence risk is the aggregation query
(`payload->>meter.value_property`); pre-prod manual validation against
real Postgres is the mitigation in v0. A Postgres test suite (via
`testcontainers-go`) is a v1 candidate once we have real users and
real billing data running through it. See roadmap.md.

**Principle 3: three layers, three roles, no overlap:**

| Layer | What it covers | What it doesn't |
|---|---|---|
| Unit | Pure domain logic in `entitlement`: balance math, period rollover, priority ordering. In-memory fixtures. | Anything that touches I/O |
| Integration (store) | Repository round-trips, idempotency (same event id twice ⇒ one row), unique constraints, query correctness. SQLite in-memory (Postgres tests deferred to v1). | API surface, auth |
| Handler | ConnectRPC + REST end-to-end via `httptest`. Auth middleware (bearer header, env keys). Validation surface (protovalidate rejects bad input). Happy path from §6.0. | Performance, load |

**What's explicitly out of scope for v0 testing:**

- Postgres-specific test suite (JSONB aggregation, `timestamptz`
  semantics, partial-index planner behavior). Deferred to v1; v0
  ships with manual pre-prod validation as the safety net.
- Load / concurrency above a few writers — no latency target set yet.
- Real billing-data correctness at scale — needs production fixtures.
- Multi-region / multi-tenant behaviors — single-tenant assumption.

**Tooling:** Go's stdlib `testing` is the baseline. `testify/require`
for assertions if it earns its weight; otherwise stdlib `t.Fatal` is
fine. No mock frameworks; prefer real components or hand-written
fakes.

## 13. Roadmap & implementation plan

Tier breakdown (v0 / v1 / v1+) and the v0 implementation checklist live
in [roadmap.md](roadmap.md).
