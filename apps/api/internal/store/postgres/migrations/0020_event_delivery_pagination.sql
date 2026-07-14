DROP INDEX IF EXISTS idx_event_delivery_attempts_subscription;

CREATE INDEX idx_event_delivery_attempts_subscription
  ON event_delivery_attempts(subscription_id, created_at DESC, id DESC);
