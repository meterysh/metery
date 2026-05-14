# Metery — Roadmap

Stages are additive — each tier builds on the previous without rewriting
the ledger or breaking existing API contracts. Anchored to
[design.md](design.md).

## v0 — ledger primitives (current scope)

### Scope

**In:**

- Customers, meters, features (metered + boolean), entitlements,
  grants, raw usage events.
- Dual-ID for the named resources — server-generated ULID `id`
  (lowercase Crockford, stored as TEXT) + caller-assigned identifier
  (`Customer.key`, `Meter.slug`, `Feature.slug`).
- Terse event envelope: `id`, `customer`, `type`, `time`, `payload`.
- **Manual grants only** — caller decides when to add credits.
- Hot path: `has_access`, `balance`, `ingest_event`.
- Boolean entitlements: row-existence-based access checks
  (`meter_slug` empty on Feature is the discriminator).
- Periodic reset (computed on read) + recurring grants (in-process worker).
- Idempotent event ingestion (PK on caller-assigned `id`).
- Keyset pagination by ULID `id` across all `List*` endpoints.
- Bearer-token authentication, keys env-provisioned via `API_KEYS`.
  Every valid token = full admin (single-tenant).
- Go service, Postgres / SQLite.

**Out (deferred):**

- Plans, subscriptions, Stripe sync.
- Static entitlements (typed config values).
- Time-bounded boolean (expiring access).
- Atomic check-and-deduct.
- Multi-tenant.
- Webhooks, real-time analytics.
- Key management API + DB-backed keys + scopes / RBAC + rate limiting.

### Implementation plan

- [x] Lock v0 API surface — proto + ConnectRPC bindings + REST transcoding via `google.api.http`. See `proto/metery/v1/`.
- [x] Validation declared in proto via `buf.validate` (protovalidate).
- [x] Initialize Go module (`go mod init github.com/meterysh/metery`) + Justfile.
- [x] Write Postgres migrations from design §7. SQLite parity migrations gated by driver.
- [x] Implement `entitlement` package with balance computation.
- [x] **Ledger fixture suite** — declarative test scenarios (grants + events ⇒ expected balance) exercising priority ordering, period rollover, rollover policies, expiration. Must pass *before* wiring any handlers.
- [x] `store` package — repository interface + Postgres impl + SQLite impl. v0 tests run against SQLite only; Postgres tests deferred to v1.
- [x] Auth middleware — bearer header parsing, `API_KEYS` env loading, constant-time compare, `UNAUTHENTICATED` for missing/invalid.
- [x] Wire up Connect handlers (and REST transcoder via `vanguard-go`) + handler-level smoke test from §6.0.
- [x] Minimal in-process recurrence worker — emits child grants on schedule; idempotent via `(parent_grant_id, effective_at)` unique constraint.

## v1 — plans, subscriptions, billing sync

Adds, on top of v0:

- **Postgres-specific test suite** — same store/repository contract,
  run against Postgres via `testcontainers-go`. Covers JSONB
  aggregation (`payload->>meter.value_property`), `timestamptz`
  semantics, partial-index behavior — the divergences SQLite-only
  testing in v0 doesn't catch.
- **API key management** — `api_keys` table (hashed storage, named,
  revocable), `CreateApiKey` / `ListApiKeys` / `RevokeApiKey` RPCs.
  v0 env keys import as a one-shot migration. Still admin-only auth;
  scopes deferred to v1+.
- **`Plan`** entity — a template binding features → grant configurations
  (amounts, recurrence, expiration, rollover).
- **`Subscription`** entity — links customer → plan with start/end dates;
  subscribing materialises the plan into entitlements + grants atomically;
  unsubscribing voids the recurring grants.
- Plan-change handling — replace recurring grants on upgrade/downgrade,
  optional proration.
- **Stripe adapter** — Stripe stays source-of-truth for "what plan is
  this customer on"; Metery is a downstream materialised view kept in
  sync via webhooks (likely shape; final call deferred to v1 design doc).
- **Static entitlements** — typed config values like `seats: 5`,
  `rate_limit: 100`. New `value` column on `entitlements` (scalar
  or JSONB for richer types); `Feature.meter_slug` empty + an
  additional "is_static" hint, or a third Feature mode. Final
  discriminator shape deferred to v1 design doc.
- **Time-bounded boolean** — `expires_at` on entitlements so trial /
  subscription-window access can be modeled without external lifecycle
  glue.

v0 ledger primitives keep working unchanged; subscription APIs are sugar
that calls into the same grant primitives under the hood.

## v1+ — candidates

Unordered, scope per-feature:

- **Atomic check-and-deduct** for race-free spending under concurrent
  load (row-locked counter or `SELECT ... FOR UPDATE`).
- **Multi-tenant** deployment — namespace column on every row.
- **API key scopes / RBAC** — read-only vs admin keys, per-resource
  scopes, per-key rate limiting.
- **Outbound webhooks** — low-balance, exhausted, period-rollover events.
- **Adapters beyond Stripe** — Lago, Orb, Paddle, custom billing.
- **Historical analytics** — usage reports, retention, cohort views.
- **Real-time aggregation** — for high-volume features outgrowing
  Postgres-only ingestion.
- **Refunds / corrections** — first-class negative-amount events.
