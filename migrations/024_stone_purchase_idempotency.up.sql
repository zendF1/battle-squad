-- POST /shop/stones/buy idempotency: a retried request with the same key
-- returns the first purchase instead of charging again.
CREATE TABLE IF NOT EXISTS stone_purchases (
    player_id       VARCHAR(64)  NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    idempotency_key VARCHAR(64)  NOT NULL,
    stone_level     INT          NOT NULL,
    quantity        INT          NOT NULL,
    price_currency  VARCHAR(10)  NOT NULL DEFAULT '',
    price_amount    INT          NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (player_id, idempotency_key)
);
