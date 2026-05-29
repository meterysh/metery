-- +goose Up

-- Plans own the recurrence cadence: the worker walks
-- subscriptions × plan.entries to emit fresh grants per cycle.
-- recurrence_interval NULL ⇒ one-shot plan (initial grants only).
CREATE TABLE plans (
  id                  TEXT PRIMARY KEY,
  slug                TEXT NOT NULL UNIQUE,
  name                TEXT NOT NULL,
  entries             TEXT NOT NULL DEFAULT '[]',
  recurrence_interval TEXT,
  recurrence_anchor   DATETIME,
  metadata            TEXT,
  created_at          DATETIME NOT NULL DEFAULT current_timestamp,
  archived_at         DATETIME
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

-- Recurrence moved from Grant to Plan. The grant columns added by 001
-- (recurrence_interval, recurrence_anchor, parent_grant_id) and the
-- recurrence uniqueness index they backed are no longer used.
DROP INDEX grants_recurrence_uniq;
ALTER TABLE grants DROP COLUMN recurrence_interval;
ALTER TABLE grants DROP COLUMN recurrence_anchor;
ALTER TABLE grants DROP COLUMN parent_grant_id;

-- Rollover is period-boundary policy: lives on the entitlement next
-- to usage_period. Plan-driven materialisation copies PlanEntry.rollover
-- here at subscribe time. NULL ⇒ unused carries over uncapped.
ALTER TABLE grants DROP COLUMN rollover_max;
ALTER TABLE grants DROP COLUMN rollover_type;
ALTER TABLE entitlements ADD COLUMN rollover_max INTEGER;

-- Worker idempotency for subscription-emitted grants: one *active*
-- grant per (subscription, entitlement, period boundary). Voided rows
-- are excluded so ChangeSubscription can void+remate­rialise at the
-- same effective_at without colliding.
CREATE UNIQUE INDEX grants_subscription_emission_uniq
  ON grants (subscription_id, entitlement_id, effective_at)
  WHERE subscription_id IS NOT NULL AND voided_at IS NULL;

-- +goose Down
DROP INDEX grants_subscription_emission_uniq;
ALTER TABLE entitlements DROP COLUMN rollover_max;
ALTER TABLE grants ADD COLUMN rollover_type TEXT;
ALTER TABLE grants ADD COLUMN rollover_max INTEGER;
ALTER TABLE grants ADD COLUMN parent_grant_id TEXT REFERENCES grants(id);
ALTER TABLE grants ADD COLUMN recurrence_anchor DATETIME;
ALTER TABLE grants ADD COLUMN recurrence_interval TEXT;
CREATE UNIQUE INDEX grants_recurrence_uniq ON grants (parent_grant_id, effective_at) WHERE parent_grant_id IS NOT NULL;
ALTER TABLE entitlements DROP COLUMN subscription_id;
ALTER TABLE grants DROP COLUMN subscription_id;
DROP INDEX subscriptions_active_by_customer;
DROP TABLE subscriptions;
DROP TABLE plans;
