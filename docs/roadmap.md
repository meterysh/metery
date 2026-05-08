# Metery — Roadmap

Stages are additive — each tier builds on the previous without rewriting
the ledger or breaking existing API contracts. Anchored to
[design.md](design.md).

## v0 — ledger primitives (current scope)

### Scope

**In:**

- Features (metered + boolean), entitlements, grants, usage events.
- **Manual grants only** — caller decides when to add credits.
- Hot path: `has_access`, `balance`, `ingest_event`.
- Boolean entitlements: row-existence-based access checks.
- Periodic reset (computed on read) + recurring grants (in-process worker).
- Idempotent event ingestion.
- Go service, Postgres / SQLite.

**Out (deferred):**

- Plans, subscriptions, Stripe sync.
- Static entitlements (typed config values).
- Time-bounded boolean (expiring access).
- Atomic check-and-deduct.
- Multi-tenant.
- Webhooks, real-time analytics.

### Implementation plan

- [x] Lock v0 API surface — proto + ConnectRPC bindings + REST transcoding via `google.api.http`. See `proto/metery/v1/`.
- [x] Validation declared in proto via `buf.validate` (protovalidate).
- [ ] Write Postgres migrations from design §7. SQLite parity migrations gated by driver.
- [ ] Initialize Go module (`go mod init github.com/meterysh/metery`).
- [ ] Implement `entitlement` package with balance computation + tests against a known ledger fixture.
- [ ] Wire up Connect handlers (and REST transcoder via `vanguard-go`) + minimal in-process recurrence worker.

## v1 — plans, subscriptions, billing sync

Adds, on top of v0:

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
  `rate_limit: 100`. New `type='static'` on features + a value column
  (or JSONB for richer types).
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
- **Outbound webhooks** — low-balance, exhausted, period-rollover events.
- **Adapters beyond Stripe** — Lago, Orb, Paddle, custom billing.
- **Historical analytics** — usage reports, retention, cohort views.
- **Real-time aggregation** — for high-volume features outgrowing
  Postgres-only ingestion.
- **Refunds / corrections** — first-class negative-amount events.
