-- +goose Up

-- A customer may hold at most one active (non-cancelled) subscription
-- per plan. Two active subscriptions to the same plan would stack grants
-- on the shared (customer, feature) entitlement — doubling credits every
-- recurrence cycle. Cancelled subscriptions are excluded, so a customer
-- can re-subscribe after cancelling.
CREATE UNIQUE INDEX subscriptions_active_plan_uniq
  ON subscriptions (customer_id, plan_id)
  WHERE canceled_at IS NULL;

-- +goose Down
DROP INDEX subscriptions_active_plan_uniq;
