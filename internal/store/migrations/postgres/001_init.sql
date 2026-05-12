-- +goose Up

CREATE TABLE customers (
  id              TEXT PRIMARY KEY,
  key             TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  metadata        JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  deactivated_at  TIMESTAMPTZ
);

CREATE TABLE meters (
  id              TEXT PRIMARY KEY,
  slug            TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  aggregation     TEXT NOT NULL CHECK (aggregation IN ('count','sum','avg','min','max','unique_count')),
  event_type      TEXT NOT NULL,
  value_property  TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at     TIMESTAMPTZ
);
CREATE INDEX meters_by_event_type ON meters (event_type) WHERE archived_at IS NULL;

CREATE TABLE features (
  id              TEXT PRIMARY KEY,
  slug            TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  meter_id        TEXT REFERENCES meters(id),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at     TIMESTAMPTZ
);

CREATE TABLE entitlements (
  id                       TEXT PRIMARY KEY,
  customer_id              TEXT NOT NULL REFERENCES customers(id),
  feature_id               TEXT NOT NULL REFERENCES features(id),
  usage_period_duration    TEXT,
  usage_period_anchor      TIMESTAMPTZ,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at               TIMESTAMPTZ
);
CREATE UNIQUE INDEX entitlements_active_uniq ON entitlements (customer_id, feature_id) WHERE deleted_at IS NULL;

CREATE TABLE grants (
  id                  TEXT PRIMARY KEY,
  entitlement_id      TEXT NOT NULL REFERENCES entitlements(id),
  amount              BIGINT NOT NULL CHECK (amount > 0),
  priority            INT    NOT NULL DEFAULT 100,
  effective_at        TIMESTAMPTZ NOT NULL,
  expires_at          TIMESTAMPTZ,
  recurrence_interval TEXT,
  recurrence_anchor   TIMESTAMPTZ,
  rollover_max        BIGINT,
  rollover_type       TEXT,
  parent_grant_id     TEXT REFERENCES grants(id),
  metadata            JSONB,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  voided_at           TIMESTAMPTZ
);
CREATE INDEX grants_by_entitlement_active ON grants (entitlement_id, priority, effective_at) WHERE voided_at IS NULL;

CREATE TABLE usage_events (
  id              TEXT PRIMARY KEY,
  customer        TEXT NOT NULL,
  type            TEXT NOT NULL,
  time            TIMESTAMPTZ NOT NULL,
  payload         JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at    TIMESTAMPTZ
);
CREATE INDEX usage_events_lookup ON usage_events (customer, type, time);

CREATE TABLE balance_snapshots (
  entitlement_id  TEXT NOT NULL REFERENCES entitlements(id),
  as_of           TIMESTAMPTZ NOT NULL,
  balance         BIGINT NOT NULL,
  per_grant_state JSONB NOT NULL,
  PRIMARY KEY (entitlement_id, as_of)
);

CREATE UNIQUE INDEX grants_recurrence_uniq ON grants (parent_grant_id, effective_at) WHERE parent_grant_id IS NOT NULL;

-- +goose Down
DROP TABLE balance_snapshots;
DROP TABLE usage_events;
DROP TABLE grants;
DROP TABLE entitlements;
DROP TABLE features;
DROP TABLE meters;
DROP TABLE customers;
