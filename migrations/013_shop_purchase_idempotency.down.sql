DROP INDEX IF EXISTS idx_shop_purchases_player_idempotency;
ALTER TABLE shop_purchases DROP COLUMN IF EXISTS idempotency_key;
