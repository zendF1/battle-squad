-- Gem -> coin exchange packs (offer_type coin_pack, item_id coin, paid in gem).
-- Base rate 1 gem = 100 coin, no bonus, no per-player limit. Stable ids;
-- ON CONFLICT DO NOTHING so reruns keep admin edits.
INSERT INTO shop_offers (offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player, is_active) VALUES
    ('coin_pack_10',  'coin', 'coin_pack', 'gem', 10,  1000,  NULL, TRUE),
    ('coin_pack_50',  'coin', 'coin_pack', 'gem', 50,  5000,  NULL, TRUE),
    ('coin_pack_100', 'coin', 'coin_pack', 'gem', 100, 10000, NULL, TRUE),
    ('coin_pack_500', 'coin', 'coin_pack', 'gem', 500, 50000, NULL, TRUE)
ON CONFLICT (offer_id) DO NOTHING;
