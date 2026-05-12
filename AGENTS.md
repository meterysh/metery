# Metery — Agent guide

Metery is a usage-billing / entitlements backend (OpenMeter-style). v0
exposes ledger primitives (customers, meters, features, entitlements,
grants, raw usage events) over ConnectRPC.

## Read first

- [docs/design.md](docs/design.md) — architecture, mental model, schema, hot-path flows.
- [docs/roadmap.md](docs/roadmap.md) — v0 / v1 / v1+ tiers + implementation checklist.
- [proto/metery/v1/](proto/metery/v1/) — authoritative API surface.

## Core concepts

- **Customer** — billable / addressable entity. Dual ID:
  server-generated `id` (ULID) for stable internal handle;
  caller-assigned `key` (opaque, unique) for natural references.
  Created first; everything else references customers.
- **Meter** — server-side aggregation definition: `aggregation`
  (`count` / `sum` / `avg` / `min` / `max` / `unique_count`),
  `event_type` filter, optional `value_property` JSON path. Dual ID:
  server `id` (ULID) + caller-assigned `slug` (URL-safe).
  Multiple features can share one meter.
- **Feature** — billing capability. Dual ID: server `id` (ULID) +
  caller-assigned `slug`. `meter_slug` set ⇒ metered (uses meter for
  usage); `meter_slug` empty ⇒ boolean (entitlement existence is the
  access bit). No `type` field — `meter_slug` presence is the
  discriminator.
- **Entitlement** — `(customer, feature)` access record + optional
  usage-period config. References `customer_key` and `feature_slug`
  (both immutable).
- **Grant** — credits added to a metered entitlement.
- **Usage event** — append-only raw observation with `id`, `customer`,
  `type`, `time`, `payload`. Server aggregates via meter.

## Terminology (non-negotiable)

- **Customer**, not "user" / "subject" / "account". The acting, entitled,
  billable entity.
- **Customer.id** is a ULID (our handle); **Customer.key** is the
  caller's identifier — opaque, max 256 chars, but **cannot itself
  parse as a ULID** (rejected at create time to keep the namespace
  unambiguous).
- **Meter.slug** / **Feature.slug** is the caller-assigned identifier
  for those resources — URL-safe, format-constrained
  (`^[a-z][a-z0-9_]*$`, max 64 chars). `slug` (not `key`) because the
  format expectation is semantic: meter / feature identifiers appear
  in event streams and URL paths, and the slug pattern signals
  "format this carefully."
- **IngestEvent**, not `RecordEvent` (industry verb for usage-billing).
- Events use **`customer`** and **`type`** field names. Other resources
  use `customer_key` / `customer_id_or_key` and `feature_slug` /
  `feature_id_or_slug`.

## Resource ID convention

All three "named" resources are dual-ID — server-generated ULID `id`
plus a caller-friendly identifier (`key` for Customer; `slug` for
Meter / Feature). Direct-op paths and sub-resource paths accept the
flexible `id_or_key` / `id_or_slug` form (server format-detects ULID:
26 chars, Crockford lowercase alphabet).

| Resource | Server `id` | Caller's | Direct ops URL | Direct-op request field |
|---|---|---|---|---|
| Customer | ULID | `key` | `/v1/customers/{id_or_key}` | `id_or_key` |
| Meter | ULID | `slug` | `/v1/meters/{id_or_slug}` | `id_or_slug` |
| Feature | ULID | `slug` | `/v1/features/{id_or_slug}` | `id_or_slug` |

Sub-resource paths use `customer_id_or_key` and `feature_id_or_slug`.

**Exception**: `CreateFeatureRequest.meter_slug` accepts caller-friendly
slug only (no `id_or_slug` flex). Rationale: admin scripting CreateFeature
already has the meter's slug; the flexibility isn't earning its place
on a setup-time write.

## Cross-reference convention

**Entities expose the caller-friendly identifier only — never the
ULID FK.** ULIDs live on the resource itself (Customer / Meter / Feature
each carry their own `id`). When entity A references entity B, A
carries B's `key` (Customer) or B's `slug` (Meter / Feature), both
immutable.

| Reference | Field on entity |
|---|---|
| Feature → Meter | `meter_slug` |
| Entitlement → Customer | `customer_key` |
| Entitlement → Feature | `feature_slug` |
| UsageEvent → Customer | `customer` (terse) |

If a caller needs a referenced resource's ULID, they `Get*` it by
key/slug. DB-level FK strategy is an implementation detail (decided
at migration time).

## Customer reference flexibility

For customer references in sub-resource paths, we accept either form:

| Where | Field name | Accepts |
|---|---|---|
| Sub-resource paths | `customer_id_or_key` | ULID **or** key |
| Event entity / IngestEvent | `customer` (terse) | ULID **or** key on ingest; stored as key |

Server format-detects ULID inputs (26 chars, Crockford lowercase) and
routes accordingly. No fallback dance needed — caller-assigned keys
that would parse as ULIDs are rejected at create time, so detection
is unambiguous.

## Caller-friendly references on create

When a request references another resource by its server-generated ID,
**accept the caller-friendly identifier and resolve to the ULID
server-side**. Examples:

- `CreateFeatureRequest.meter_slug` (slug only) — server looks up Meter
  by slug and stores the resolved reference.
- `CreateEntitlementRequest.feature_id_or_slug` — server resolves to
  the feature record.
- Sub-resource customer paths: `customer_id_or_key` accepts either form.

## Proto conventions

- ConnectRPC is canonical; REST is transcoded via `google.api.http`
  annotations. Both wire formats target the same handler.
- Single-field responses use `response_body: "<field>"` so the REST
  output is unwrapped (Connect/gRPC clients still get the wrapped form).
- REST path params (`{id_or_key}`, `{id_or_slug}`, `{customer_id_or_key}`,
  `{feature_id_or_slug}`) come from the URL — omit them from the JSON
  body in examples.
- **Validation lives in the proto** via `buf.validate`:
  - `required = true` for presence — do **not** use `string.min_len = 1`.
  - `string = {in: [...]}` for closed-set discriminators. We deliberately
    chose strings + `in` over proto enums for clean JSON output.
  - ULID fields use `string.pattern = "^[0-9a-hjkmnp-tv-z]{26}$"`
    (26 chars, lowercase Crockford base32 alphabet — skips i/l/o/u).
    No built-in protovalidate `ulid` rule; pattern does the job.
  - **Always pair `in` / `pattern` with `required = true` on
    proto3 scalar fields.** Protovalidate honors implicit field
    presence: rules like `in` and `pattern` are *skipped* when the
    value is the default (`""`/`0`). Without `required`, an empty
    string silently passes `in` and `pattern` checks.
    Exception: `Feature.meter_slug` deliberately uses the slug `pattern`
    *without* `required` — empty string is the legitimate "boolean
    feature" sentinel; the pattern rule fires only when meter_slug is set.
  - **Mark `required = true` on always-populated message fields** (not
    just request inputs). Proto3 generates message fields as Go
    pointers, so the language can't express the invariant — the
    validation rule documents it and `protovalidate.Validate` enforces
    it on receive. Applies to:
    - `created_at` on every managed entity (Customer, Meter, Feature,
      Entitlement, Grant), `effective_at` on Grant, `time` on UsageEvent.
      UsageEvent is a stream observation, not a managed entity — its
      server bookkeeping (`created_at`, `processed_at`) stays in storage.
    - Single-resource response wrappers: `CreateFeatureResponse.feature`,
      `GetEntitlementValueResponse.value`, etc.

    Do *not* mark fields that are state-conditional: `archived_at`,
    `deleted_at`, `voided_at`, `expires_at`, `deactivated_at`.
    List/empty responses don't need it.
- ISO-8601 durations are plain strings (`"P1M"`); we don't use
  `google.protobuf.Duration` because billing periods are calendar-aware.
  Field names are semantic (`duration`, `interval`); format lives in the
  comment.
- `int64` is JSON-serialised as a string (`"1000"` not `1000`) — that's
  protojson's default to preserve precision.
- **Action verbs return `{}`**: `Reset`, `Archive`, `Void`, `Delete`,
  `Deactivate`, `IngestEvent`. REST surfaces these as `200 OK {}` (not
  204 — keeps REST and Connect responses shape-identical, lets REST
  clients call `response.json()` unconditionally). Caller refetches via
  `Get*` / `Value` for post-action state. `Create*` is the only
  mutation that returns a body — server-assigned ULIDs aren't
  recoverable otherwise.
- **Idempotency**: `IngestEvent.id` is both event identifier and dedup
  key. Replays are silent no-ops (still `200 OK {}`) — duplicate
  visibility is server-side metrics, not response body.
- **Pagination**: keyset by ULID. Request:
  `optional int32 limit` (server default when absent) +
  `optional string after` (validated by the ULID pattern) —
  the last `id` from the previous page; absent ⇒ first page. Response:
  just the `repeated <resource>` field — no `next_cursor`. Caller
  paginates by passing the last returned row's `id` as the next
  `after`. Works because ULID is lexically sortable; on the DB
  side this is `WHERE id > $after ORDER BY id LIMIT $limit`. End of
  results ⇒ response array shorter than `limit` (or empty).

## Aggregation model

Events are **raw observations** — no pre-aggregation by caller. Server
aggregates via the meter associated with the metered feature:
- `count` — `COUNT(*)` of events matching `type = meter.event_type`
  for the customer in current period.
- `sum` / `avg` / `min` / `max` — applies aggregation to
  `payload->>meter.value_property` (cast to numeric).
- `unique_count` — `COUNT(DISTINCT payload->>meter.value_property)`.

No streaming infra in v0. Plain Postgres queries with appropriate
indexes; consider materialized rollups when read traffic justifies.

## Build / generate

```
make proto         # buf lint + buf generate
buf lint           # must pass before commit
buf generate       # writes to gen/go/ (committed; consumers don't need buf)
```

## Out of scope (unless explicitly requested)

- Plans, Subscriptions, Stripe sync — v1 (see roadmap).
- Static entitlements, time-bounded boolean — v1.
- Atomic check-and-deduct, multi-tenant, webhooks — v1+.
- Streaming infra (Kafka / ClickHouse) — v1+.

## Auth

- Every endpoint requires `Authorization: Bearer <token>`.
- **v0**: tokens are env-provisioned via `API_KEYS=mtr_xxx,mtr_yyy`
  (comma-separated). Constant-time compare; no DB lookup.
- Every valid token has full-admin access (single-tenant assumption).
- Missing / invalid token ⇒ `UNAUTHENTICATED` (no distinction, avoids
  enumeration).
- DB-backed keys + management API + scopes / RBAC / rate limiting are
  v1+ (see roadmap).

## Stack

- Go + Postgres (SQLite for tests / single-tenant dev).
- Module path: `github.com/meterysh/metery` (placeholder; change in
  proto `option go_package` if you rename the org).
- **IDs**: generate **ULIDs** via `github.com/oklog/ulid/v2`
  (`ulid.Make().String()`), then lowercase. 26 chars, Crockford base32,
  lexically sortable (time-prefix) for good B-tree locality. Stored
  as `TEXT` in Postgres (readability over the ~10-byte saving of
  binary) — easy to spot in `psql`, dumps, log lines. Caller-assigned
  IDs (`UsageEvent.id`, `Customer.key`, `Meter.slug`, `Feature.slug`)
  are exempt — caller picks any format up to 256 chars (or the slug
  pattern `^[a-z][a-z0-9_]*$` for `Meter.slug` / `Feature.slug`),
  but values that *would* parse as a valid ULID are rejected at
  create time so the format-detect routing stays unambiguous.
