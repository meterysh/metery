-- +goose Up

CREATE TABLE customers (
  id              TEXT PRIMARY KEY,
  key             TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  metadata        TEXT,
  created_at      DATETIME NOT NULL DEFAULT current_timestamp,
  deactivated_at  DATETIME
);

CREATE TABLE meters (
  id              TEXT PRIMARY KEY,
  slug            TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  aggregation     TEXT NOT NULL CHECK (aggregation IN ('count','sum','avg','min','max','unique_count')),
  event_type      TEXT NOT NULL,
  value_property  TEXT,
  created_at      DATETIME NOT NULL DEFAULT current_timestamp,
  archived_at     DATETIME
);
CREATE INDEX meters_by_event_type ON meters (event_type) WHERE archived_at IS NULL;

CREATE TABLE features (
  id              TEXT PRIMARY KEY,
  slug            TEXT NOT NULL UNIQUE,
  name            TEXT NOT NULL,
  meter_id        TEXT REFERENCES meters(id),
  created_at      DATETIME NOT NULL DEFAULT current_timestamp,
  archived_at     DATETIME
);

CREATE TABLE entitlements (
  id                       TEXT PRIMARY KEY,
  customer_id              TEXT NOT NULL REFERENCES customers(id),
  feature_id               TEXT NOT NULL REFERENCES features(id),
  usage_period_duration    TEXT,
  usage_period_anchor      DATETIME,
  created_at               DATETIME NOT NULL DEFAULT current_timestamp,
  deleted_at               DATETIME
);
CREATE UNIQUE INDEX entitlements_active_uniq ON entitlements (customer_id, feature_id) WHERE deleted_at IS NULL;

CREATE TABLE grants (
  id                  TEXT PRIMARY KEY,
  entitlement_id      TEXT NOT NULL REFERENCES entitlements(id),
  amount              INTEGER NOT NULL CHECK (amount > 0),
  priority            INTEGER NOT NULL DEFAULT 100,
  effective_at        DATETIME NOT NULL,
  expires_at          DATETIME,
  recurrence_interval TEXT,
  recurrence_anchor   DATETIME,
  rollover_max        INTEGER,
  rollover_type       TEXT,
  parent_grant_id     TEXT REFERENCES grants(id),
  metadata            TEXT,
  created_at          DATETIME NOT NULL DEFAULT current_timestamp,
  voided_at           DATETIME
);
CREATE INDEX grants_by_entitlement_active ON grants (entitlement_id, priority, effective_at) WHERE voided_at IS NULL;

CREATE TABLE usage_events (
  id              TEXT PRIMARY KEY,
  customer        TEXT NOT NULL,
  type            TEXT NOT NULL,
  time            DATETIME NOT NULL,
  payload         TEXT,
  created_at      DATETIME NOT NULL DEFAULT current_timestamp,
  processed_at    DATETIME
);
CREATE INDEX usage_events_lookup ON usage_events (customer, type, time);

CREATE TABLE balance_snapshots (
  entitlement_id  TEXT NOT NULL REFERENCES entitlements(id),
  as_of           DATETIME NOT NULL,
  balance         INTEGER NOT NULL,
  per_grant_state TEXT NOT NULL,
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
