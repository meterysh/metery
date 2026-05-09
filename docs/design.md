# Metery — Design

Status: draft v0
Owner: vh
Last updated: 2026-05-07

## 1. Overview

Metery is a usage-billing / entitlements backend in the spirit of OpenMeter.
Integrated apps ask Metery two questions:

1. **Can this customer perform this action?** (entitlement check)
2. **Record that this customer performed this action.** (usage event)

Internally, Metery keeps a **per-feature credit ledger** per
`(customer, feature)` pair and derives a balance from grants minus usage.
Each capability of the integrated app (API calls, tokens, image
generation, …) is modelled as its own **feature** with its own balance,
grants, and reset cadence. Subscriptions top up balances via recurring
grants and/or period resets.

## 2. Goals (v0)

- Multiple **features** per system. Each feature has a `type`:
  - **metered** — measured in its own unit (`api_call`, `token`, `image`).
    "Credit" is the abstract term for one unit of a metered feature.
  - **boolean** — yes/no access to a capability (e.g. `priority_support`,
    `webhooks_enabled`). No balance, no grants, no usage events — just
    "does the customer have this entitlement or not."
- Per-customer **entitlements** scoped to a feature, with optional periodic
  reset (metered only).
- **Grants** that add credits to a metered entitlement, with priority,
  expiration, recurrence, and rollover at period boundary.
- Two read paths (uniform API, dispatched by feature type):
  - `has_access(customer, feature [, cost])` → bool
  - `value(customer, feature)` → metered: balance + period window;
    boolean: has_access.
- One write path (metered only):
  - `ingest_event(id, customer, feature, amount, occurred_at)`
- Stack: Go + Postgres (SQLite for tests / single-tenant dev).
- Idempotent event ingestion.

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
- Auth / authz of the management API.
- Webhooks / outbound notifications.
- Historical re-pricing or backfill from raw event streams.

See [roadmap.md](roadmap.md) for the v1 / v1+ tiers where these land.

## 4. Mental model

We are using the **metered-features** model: each capability of the
integrated app is its own feature. Most features are metered (own balance,
own ledger); some are boolean (yes/no access). Features are **not**
interchangeable — credits in `api_calls` cannot be spent on `tokens`. The
cost of a metered action is denominated in the feature's own unit
(1 API call = 1 `api_call`-credit; 1500 tokens = 1500 `token`-credits).

| Concept         | What it is                                                      |
|-----------------|-----------------------------------------------------------------|
| **Customer**     | The billable entity (user, account, org). Opaque string ID.     |
| **Feature**     | A named capability with a `type` (`metered` or `boolean`). Metered features have a unit (e.g. `api_call`, `token`). |
| **Entitlement** | A `(customer, feature)` access record. For metered features, also carries usage-period config. |
| **Grant**       | Credits added to one *metered* entitlement. Has priority, expiry, recurrence, rollover. (Not used for boolean.) |
| **Usage event** | Append-only record of consumption against one *metered* feature. (Not used for boolean.) |
| **Ledger**      | The set of grants + usage events for metered features. Balance is derived from it. |

**Worked example — a Pro subscriber:**

| Feature              | Type    | Unit     | Grant / Value                  | Cost per action            |
|----------------------|---------|----------|--------------------------------|----------------------------|
| `api_calls`          | metered | api_call | 10,000 / month, recurring      | one API call → 1           |
| `tokens`             | metered | token    | 1,000,000 / month, recurring   | one chat message → variable, sized by completion |
| `priority_support`   | boolean | —        | granted (entitlement exists)   | — (gate check, no spend)   |

The integrated app maps action → (feature, amount) before calling Metery.
Metery never knows what an "action" is — it sees only `(customer, feature,
amount)`.

> **Note (deferred capability).** Nothing in this model prevents adding a
> generic `credits` wallet feature later (universal currency that the
> app's cost center can fall back to when a quota feature is exhausted).
> No schema change required — it would just be another feature row. Out
> of scope for v0; flagged here so we don't design ourselves out of it.

**Invariant.** For any entitlement at time `T`:

```
balance(T) = Σ active_grant_amount(T)  −  Σ usage_in_current_period(T)
```

with grants consumed in priority order (lower priority number burns first),
ties broken by `effective_at`.

## 5. OpenMeter parity: grants & resets

This is the part the user asked about explicitly. Metery v0 mirrors
OpenMeter's grant/reset model so that we can later integrate or migrate.

**Grant fields (OpenMeter-aligned):**

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

| Method | REST                                                                          | Connect RPC                                  |
|--------|-------------------------------------------------------------------------------|----------------------------------------------|
| POST   | `/v1/features`                                                                | `FeatureService/CreateFeature`               |
| GET    | `/v1/features`                                                                | `FeatureService/ListFeatures`                |
| GET    | `/v1/features/{id}`                                                           | `FeatureService/GetFeature`                  |
| DELETE | `/v1/features/{id}`                                                           | `FeatureService/ArchiveFeature`              |
| POST   | `/v1/customers/{customer_id}/entitlements`                                      | `EntitlementService/CreateEntitlement`       |
| GET    | `/v1/customers/{customer_id}/entitlements`                                      | `EntitlementService/ListEntitlements`        |
| GET    | `/v1/customers/{customer_id}/entitlements/{feature_id}`                         | `EntitlementService/GetEntitlement`          |
| DELETE | `/v1/customers/{customer_id}/entitlements/{feature_id}`                         | `EntitlementService/DeleteEntitlement`       |
| GET    | `/v1/customers/{customer_id}/entitlements/{feature_id}/value`                   | `EntitlementService/GetEntitlementValue`     |
| POST   | `/v1/customers/{customer_id}/entitlements/{feature_id}/reset`                   | `EntitlementService/ResetEntitlement`        |
| POST   | `/v1/customers/{customer_id}/entitlements/{feature_id}/grants`                  | `GrantService/CreateGrant`                   |
| GET    | `/v1/customers/{customer_id}/entitlements/{feature_id}/grants`                  | `GrantService/ListGrants`                    |
| DELETE | `/v1/grants/{id}`                                                             | `GrantService/VoidGrant`                     |
| POST   | `/v1/events`                                                                  | `EventService/IngestEvent`                   |

### 6.0 v0 minimal happy path

The simplest end-to-end use of Metery, no subscriptions, no recurrence —
just "give user 1000 credits, check, spend":

```
# 1. Define the feature once
POST /v1/features
{ "id": "api_calls", "type": "metered",
  "name": "API calls", "unit": "api_call" }

# 2. Provision an entitlement for the customer (one-time)
POST /v1/customers/user_123/entitlements
{ "feature_id": "api_calls" }
# no usage_period ⇒ never resets

# 3. Grant credits manually (one-time, no recurrence/expiration)
POST /v1/customers/user_123/entitlements/api_calls/grants
{ "amount": "1000" }

# 4. Hot path: check + ingest on every action
GET  /v1/customers/user_123/entitlements/api_calls/value?cost=1

POST /v1/events
{ "id": "...", "customer_id": "user_123", "feature_id": "api_calls",
  "amount": "1", "occurred_at": "..." }
```

For a **boolean** feature the flow is even shorter:

```
POST /v1/features
{ "id": "priority_support", "type": "boolean", "name": "Priority support" }

POST /v1/customers/user_123/entitlements
{ "customer_id": "user_123", "feature_id": "priority_support" }
# row exists ⇒ has access

GET  /v1/customers/user_123/entitlements/priority_support/value
→ { "value": { "type": "boolean", "has_access": true } }

# Revoke:
DELETE /v1/customers/user_123/entitlements/priority_support
```

(Path params: `customer_id` and `feature_id` are bound from the URL and
omitted from the request body when present in the path.)

> **Note on JSON int64.** protojson serialises `int64` as a JSON string
> (`"1000"` not `1000`) to avoid JavaScript precision loss. Caller
> libraries handle this transparently.

Everything below (recurrence, expiration, rollover, periodic reset) is
optional and only needed when you graduate to subscription-style billing.

### 6.1 Define a feature (one-time setup, admin)

Metered:

```
POST /v1/features
{ "id": "api_calls", "type": "metered",
  "name": "API calls", "unit": "api_call" }
```

Boolean:

```
POST /v1/features
{ "id": "priority_support", "type": "boolean",
  "name": "Priority support" }
```

### 6.2 Provision an entitlement (per customer, per feature)

```
POST /v1/customers/user_123/entitlements
{
  "feature_id":   "api_calls",
  "usage_period": { "duration": "P1M", "anchor": "2026-05-01T00:00:00Z" }
}
```

### 6.3 Grant credits

```
POST /v1/customers/user_123/entitlements/api_calls/grants
{
  "amount":       "10000",
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
GET /v1/customers/user_123/entitlements/api_calls/value?cost=1
→ {
  "type":         "metered",
  "has_access":   true,
  "balance":      "9742",
  "usage":        "258",
  "overage":      "0",
  "usage_period": { "from": "2026-05-01T00:00:00Z", "to": "2026-06-01T00:00:00Z" },
  "last_reset":   "2026-05-01T00:00:00Z"
}
```

Boolean — `cost` is ignored; result is row existence:

```
GET /v1/customers/user_123/entitlements/priority_support/value
→ { "type": "boolean", "has_access": true }
```

If the entitlement does not exist for the customer, the response is
`{ "type": "<feature.type>", "has_access": false }`. Caller does not
need to know the feature's type ahead of time.

> **Note.** Connect / gRPC clients receive the wrapped form
> `{ "value": { ... } }` — `response_body` only affects the REST entry
> point.

### 6.5 Ingest a usage event (hot path)

```
POST /v1/events
{
  "id":          "req_abc123",
  "customer_id": "user_123",
  "feature_id":  "api_calls",
  "amount":      "1",
  "occurred_at": "2026-05-08T10:11:12Z"
}
→ 200 OK
{}
```

The caller-assigned `id` is **both** the event identifier and the
idempotency key — replaying the same `id` is silently deduped at the
unique-PK level and is also a successful no-op (also `200 OK {}`).
UUIDs are conventional, but any unique string ≤ 256 chars works
(CloudEvents compatibility).

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
because the server-assigned UUID isn't recoverable otherwise.

> Why 200 + `{}` not 204: keeps the REST and Connect surfaces shape-
> identical, lets clients call `response.json()` unconditionally, and
> matches the default transcoder behavior for empty messages.

## 7. Data model (Postgres)

```sql
CREATE TABLE features (
  id            TEXT PRIMARY KEY,        -- "api_calls", "tokens", ...
  type          TEXT NOT NULL CHECK (type IN ('metered', 'boolean')),
  name  TEXT NOT NULL,
  unit          TEXT,                    -- required for metered, NULL for boolean
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at   TIMESTAMPTZ,
  CHECK ((type = 'metered' AND unit IS NOT NULL) OR (type = 'boolean' AND unit IS NULL))
);

CREATE TABLE entitlements (
  id                 UUID PRIMARY KEY,
  customer_id         TEXT NOT NULL,
  feature_id         TEXT NOT NULL REFERENCES features(id),
  usage_period_duration   TEXT,                 -- "P1M"; NULL = no reset
  usage_period_anchor TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at         TIMESTAMPTZ
);
CREATE UNIQUE INDEX entitlements_active_uniq
  ON entitlements (customer_id, feature_id) WHERE deleted_at IS NULL;

CREATE TABLE grants (
  id                  UUID PRIMARY KEY,
  entitlement_id      UUID NOT NULL REFERENCES entitlements(id),
  amount              NUMERIC NOT NULL CHECK (amount > 0),
  priority            INT    NOT NULL DEFAULT 100,
  effective_at        TIMESTAMPTZ NOT NULL,
  expires_at          TIMESTAMPTZ,         -- effective_at + expiration.duration
  recurrence_interval      TEXT,                -- "P1M" or NULL
  recurrence_anchor   TIMESTAMPTZ,
  rollover_max        NUMERIC,             -- NULL = no rollover
  rollover_type       TEXT,                -- 'original' | 'remaining'
  parent_grant_id     UUID REFERENCES grants(id), -- recurring chain
  metadata            JSONB,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  voided_at           TIMESTAMPTZ          -- set on rollover void / manual revoke
);
CREATE INDEX grants_by_entitlement_active
  ON grants (entitlement_id, priority, effective_at)
  WHERE voided_at IS NULL;

CREATE TABLE usage_events (
  id              TEXT PRIMARY KEY,                       -- caller-assigned; PK ⇒ dedup
  customer_id     TEXT NOT NULL,
  feature_id      TEXT NOT NULL,
  amount          NUMERIC NOT NULL CHECK (amount > 0),
  occurred_at     TIMESTAMPTZ NOT NULL,
  metadata        JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX usage_events_lookup
  ON usage_events (customer_id, feature_id, occurred_at);

-- Optional cache; rebuildable from the ledger.
CREATE TABLE balance_snapshots (
  entitlement_id  UUID NOT NULL REFERENCES entitlements(id),
  as_of           TIMESTAMPTZ NOT NULL,    -- typically a period boundary
  balance         NUMERIC NOT NULL,
  per_grant_state JSONB NOT NULL,          -- [{grant_id, remaining}, ...]
  PRIMARY KEY (entitlement_id, as_of)
);
```

**Notes**

- `usage_events` is append-only — no updates, no deletes. Corrections are
  separate negative-amount entries (deferred; v0 only positive amounts).
- `grants` are append-only except for `voided_at` (soft-void).
- `balance_snapshots` is purely a derived cache; if it's empty, balance is
  recomputed from the full ledger.
- **Boolean features** use only the `entitlements` table. `usage_period_duration`
  must be NULL; no rows in `grants` / `usage_events` / `balance_snapshots`.
  Validation enforced at the application layer; can be hardened with
  partial constraints later.
- SQLite parity: replace `UUID` with `TEXT`, `NUMERIC` with `REAL` or
  store as integer micro-credits, `JSONB` with `TEXT`. Keep schema
  otherwise identical via migrations gated by driver.

## 8. Balance computation

Given `(entitlement, T)`:

1. Determine current usage period `[period_start, period_end)`:
   - If `usage_period_duration` is null → `[entitlement.created_at, +∞)`.
   - Else: align `T` against `usage_period_anchor` stepping by
     `usage_period_duration`.
2. Load active grants: `voided_at IS NULL AND effective_at <= T AND
   (expires_at IS NULL OR expires_at > T)`.
3. Initial per-grant remaining = `amount`. If a snapshot exists at
   `period_start`, seed `per_grant_state` from it.
4. Replay usage events in `[period_start, T]` ordered by `occurred_at`,
   deducting from grants in `(priority asc, effective_at asc)`.
5. `balance = Σ per_grant_remaining`.

For v0, recompute on every read. Add snapshots once read traffic justifies
it — a snapshot at every period boundary bounds the replay window to one
period of usage events.

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
- **Recurring grant emission**, however, *does* need a scheduler because
  new grants must materialize as rows so they show up in queries. v0
  approach: a periodic worker scans `grants WHERE recurrence_interval IS NOT
  NULL` and emits the next child grant when due. Idempotent via
  `(parent_grant_id, effective_at)` uniqueness.

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

1. **Credit unit precision.** Integer or fractional amounts? Most features
   (`api_calls`) are naturally integer; `tokens`-style features are also
   integer (1 token = 1 credit). Suggest: store `amount` as `BIGINT` and
   require integer-valued credits; if we ever need fractional pricing,
   scale at the feature level (define unit as `millitoken`,
   `micro-credit`, etc).
2. **Variable-cost actions.** A chat completion isn't priced until it
   completes (1500 tokens vs 200 tokens). Two patterns to choose from:
   (a) check access with a worst-case `cost` upfront, then record actual
   usage afterwards; (b) check `has_access` for `cost=1` (any), allow,
   then record actual. Default: (a) — check `cost = max_expected`.
3. **Negative amounts / refunds.** Do we need to model refunds or
   corrections in v0, or rely on compensating positive grants?
4. **Customer namespace.** Do customers belong to a namespace/tenant, or is
   "customer string" globally unique to the deployment?
5. **Subscriptions: v1+ scope, deferred from v0.** Long-term goal is a
   first-class `Plan` + `Subscription` concept in Metery, with sync
   adapters to Stripe (and others). v0 stays at the primitive level —
   manual grants only. The integrated app or Stripe webhook code drives
   Metery via the grant API for now. Open question for v1: does Metery
   own subscription state directly, or does it stay in Stripe with
   Metery as a downstream materialized view? Likely the latter, but
   we'll decide when we get there.
6. ~~**Boolean / static entitlements.**~~ **Resolved.** Boolean is in v0
   (`type` column on `features`). Static is deferred to v1+ (see §3
   Non-goals).
7. **Hot-path latency target?** Drives whether we need balance snapshots
   from day one.

## 12. Roadmap & implementation plan

Tier breakdown (v0 / v1 / v1+) and the v0 implementation checklist live
in [roadmap.md](roadmap.md).
