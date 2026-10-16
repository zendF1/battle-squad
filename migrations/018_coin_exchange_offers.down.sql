-- Removes the coin packs exactly as migration 018 seeded them and never bought:
-- rows an admin edited stay, and bought rows stay so the cascade does not
-- erase purchase history.
DELETE FROM shop_offers o
USING (VALUES
    ('coin_pack_10',  10,  1000),
    ('coin_pack_50',  50,  5000),
    ('coin_pack_100', 100, 10000),
    ('coin_pack_500', 500, 50000)
) AS seeded(offer_id, price_amount, quantity)
WHERE o.offer_id = seeded.offer_id
  AND o.item_id = 'coin'
  AND o.offer_type = 'coin_pack'
  AND o.price_currency = 'gem'
  AND o.price_amount = seeded.price_amount
  AND o.quantity = seeded.quantity
  AND o.limit_per_player IS NULL
  AND o.starts_at IS NULL
  AND o.ends_at IS NULL
  AND o.is_active
  AND NOT EXISTS (SELECT 1 FROM shop_purchases p WHERE p.offer_id = o.offer_id);
