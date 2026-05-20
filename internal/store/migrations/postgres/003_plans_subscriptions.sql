-- +goose Up

CREATE TABLE plans (
  id           TEXT PRIMARY KEY,
  slug         TEXT NOT NULL UNIQUE,
  name         TEXT NOT NULL,
  entries      JSONB NOT NULL DEFAULT '[]'::jsonb,
  metadata     JSONB,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  archived_at  TIMESTAMPTZ
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

-- +goose Down
ALTER TABLE entitlements DROP COLUMN subscription_id;
ALTER TABLE grants DROP COLUMN subscription_id;
DROP INDEX subscriptions_active_by_customer;
DROP TABLE subscriptions;
DROP TABLE plans;
