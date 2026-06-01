-- +goose Up

CREATE TABLE plans (
  id                  TEXT PRIMARY KEY,
  slug                TEXT NOT NULL UNIQUE,
  name                TEXT NOT NULL,
  entries             JSONB NOT NULL DEFAULT '[]'::jsonb,
  recurrence_interval TEXT,
  recurrence_anchor   TIMESTAMPTZ,
  metadata            JSONB,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at         TIMESTAMPTZ
);

CREATE TABLE subscriptions (
  id            TEXT PRIMARY KEY,
  customer_id   TEXT NOT NULL REFERENCES customers(id),
  plan_id       TEXT NOT NULL REFERENCES plans(id),
  starts_at     TIMESTAMPTZ NOT NULL,
  ends_at       TIMESTAMPTZ,
  canceled_at   TIMESTAMPTZ,
  metadata      JSONB,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX subscriptions_active_by_customer ON subscriptions (customer_id) WHERE canceled_at IS NULL;

ALTER TABLE grants ADD COLUMN subscription_id TEXT REFERENCES subscriptions(id);
ALTER TABLE entitlements ADD COLUMN subscription_id TEXT REFERENCES subscriptions(id);

DROP INDEX grants_recurrence_uniq;
ALTER TABLE grants DROP COLUMN recurrence_interval;
ALTER TABLE grants DROP COLUMN recurrence_anchor;
ALTER TABLE grants DROP COLUMN parent_grant_id;
ALTER TABLE grants DROP COLUMN rollover_max;
ALTER TABLE grants DROP COLUMN rollover_type;
ALTER TABLE entitlements ADD COLUMN rollover_max BIGINT;

CREATE UNIQUE INDEX grants_subscription_emission_uniq
  ON grants (subscription_id, entitlement_id, effective_at)
  WHERE subscription_id IS NOT NULL AND voided_at IS NULL;

-- +goose Down
DROP INDEX grants_subscription_emission_uniq;
ALTER TABLE entitlements DROP COLUMN rollover_max;
ALTER TABLE grants ADD COLUMN rollover_type TEXT;
ALTER TABLE grants ADD COLUMN rollover_max BIGINT;
ALTER TABLE grants ADD COLUMN parent_grant_id TEXT REFERENCES grants(id);
ALTER TABLE grants ADD COLUMN recurrence_anchor TIMESTAMPTZ;
ALTER TABLE grants ADD COLUMN recurrence_interval TEXT;
CREATE UNIQUE INDEX grants_recurrence_uniq ON grants (parent_grant_id, effective_at) WHERE parent_grant_id IS NOT NULL;
ALTER TABLE entitlements DROP COLUMN subscription_id;
ALTER TABLE grants DROP COLUMN subscription_id;
DROP INDEX subscriptions_active_by_customer;
DROP TABLE subscriptions;
DROP TABLE plans;
