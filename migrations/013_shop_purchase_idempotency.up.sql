-- Shop purchase idempotency lives in the DB (was Redis): a retried request with
-- the same key returns the committed purchase instead of charging again.
ALTER TABLE shop_purchases
    ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS idx_shop_purchases_player_idempotency
    ON shop_purchases (player_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
