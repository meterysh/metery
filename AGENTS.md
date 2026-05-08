# Metery — Agent guide

Metery is a usage-billing / entitlements backend (OpenMeter-style). v0
exposes ledger primitives (features, entitlements, grants, usage events)
over ConnectRPC.

## Read first

- [docs/design.md](docs/design.md) — architecture, mental model, schema, hot-path flows.
- [docs/roadmap.md](docs/roadmap.md) — v0 / v1 / v1+ tiers + implementation checklist.
- [proto/metery/v1/](proto/metery/v1/) — authoritative API surface.

## Terminology (non-negotiable)

- **Customer**, not "user" / "subject" / "account". The acting, entitled,
  billable entity. Aligns with Stripe/Lago/Orb and current OpenMeter.
- **Feature.type**: `"metered"` (has a credit ledger) or `"boolean"`
  (yes/no access). Static is v1+.
- **Credit**: one unit of a feature. Unit is per-feature
  (`api_call`, `token`, …); credits don't transfer between features.
- **IngestEvent**, not `RecordEvent` (industry verb for usage-billing).

## Proto conventions

- ConnectRPC is canonical; REST is transcoded via `google.api.http`
  annotations. Both wire formats target the same handler.
- Single-field responses use `response_body: "<field>"` so the REST
  output is unwrapped (Connect/gRPC clients still get the wrapped form).
- REST path params (`{customer_id}`, `{feature_id}`) come from the URL —
  omit them from the JSON body in examples.
- **Validation lives in the proto** via `buf.validate`:
  - `required = true` for presence — do **not** use `string.min_len = 1`.
  - `string = {in: [...]}` for closed-set discriminators. We deliberately
    chose strings + `in` over proto enums for clean JSON output.
  - `string.uuid = true` for UUID fields.
- ISO-8601 durations are plain strings (`"P1M"`); we don't use
  `google.protobuf.Duration` because billing periods are calendar-aware.
  Field names are semantic (`duration`, `interval`); format lives in the
  comment.
- `int64` is JSON-serialised as a string (`"1000"` not `1000`) — that's
  protojson's default to preserve precision.
- **Action verbs return `{}`**: `Reset`, `Archive`, `Void`, `Delete`,
  `IngestEvent`. REST surfaces these as `200 OK {}` (not 204 — keeps
  REST and Connect responses shape-identical, lets REST clients call
  `response.json()` unconditionally). Caller refetches via `Get*` /
  `Value` for post-action state. `Create*` is the only mutation that
  returns a body — server-assigned UUIDs aren't recoverable otherwise.
- **Idempotency**: `IngestEvent.id` is both event identifier and dedup
  key. Replays are silent no-ops (still `200 OK {}`) — duplicate
  visibility is server-side metrics, not response body.
- **Pagination**: opaque cursor, server-controlled. Request:
  `limit` + `cursor`. Response: `<resource>` array + `next_cursor`
  (empty ⇒ end of results). Same semantics as AIP-158 with shorter
  names; aligns better with REST/usage-billing convention than
  `page_size` / `page_token`.

## Build / generate

```
just proto         # buf lint + buf generate
buf lint           # must pass before commit
buf generate       # writes to gen/go/ (committed; consumers don't need buf)
```

## Out of scope (unless explicitly requested)

- Plans, Subscriptions, Stripe sync — v1 (see roadmap).
- Static entitlements, time-bounded boolean — v1.
- Atomic check-and-deduct, multi-tenant, webhooks — v1+.

## Stack

- Go + Postgres (SQLite for tests / single-tenant dev).
- Module path: `github.com/meterysh/metery` (placeholder; change in
  proto `option go_package` if you rename the org).
