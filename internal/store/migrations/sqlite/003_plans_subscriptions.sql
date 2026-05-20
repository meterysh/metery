-- +goose Up

CREATE TABLE plans (
  id           TEXT PRIMARY KEY,
  slug         TEXT NOT NULL UNIQUE,
  name         TEXT NOT NULL,
  entries      TEXT NOT NULL DEFAULT '[]',
  metadata     TEXT,
  created_at   DATETIME NOT NULL DEFAULT current_timestamp,
  archived_at  DATETIME
);

CREATE TABLE subscriptions (
  id            TEXT PRIMARY KEY,
  customer_id   TEXT NOT NULL REFERENCES customers(id),
  plan_id       TEXT NOT NULL REFERENCES plans(id),
  starts_at     DATETIME NOT NULL,
  ends_at       DATETIME,
  canceled_at   DATETIME,
  metadata      TEXT,
  created_at    DATETIME NOT NULL DEFAULT current_timestamp
);
CREATE INDEX subscriptions_active_by_customer ON subscriptions (customer_id) WHERE canceled_at IS NULL;

-- Subscription ownership: NULL ⇒ manually granted; non-NULL ⇒ materialised from a plan.
-- Cancel voids the grants by subscription_id; entitlements stay (usage history retained).
ALTER TABLE grants ADD COLUMN subscription_id TEXT REFERENCES subscriptions(id);
ALTER TABLE entitlements ADD COLUMN subscription_id TEXT REFERENCES subscriptions(id);

-- +goose Down
ALTER TABLE entitlements DROP COLUMN subscription_id;
ALTER TABLE grants DROP COLUMN subscription_id;
DROP INDEX subscriptions_active_by_customer;
DROP TABLE subscriptions;
DROP TABLE plans;
