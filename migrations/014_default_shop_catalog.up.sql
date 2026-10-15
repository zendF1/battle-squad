-- Default shop catalog and IAP products. Stable ids; ON CONFLICT DO NOTHING so
-- reruns keep admin edits to price, limits or is_active.

-- Battle items: 3 per purchase, paid in coin, no per-player limit.
INSERT INTO shop_offers (offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player, is_active) VALUES
    ('item_medkit_x3',       'medkit',       'item', 'coin', 300, 3, NULL, TRUE),
    ('item_teleport_x3',     'teleport',     'item', 'coin', 400, 3, NULL, TRUE),
    ('item_power_shot_x3',   'power_shot',   'item', 'coin', 400, 3, NULL, TRUE),
    ('item_drill_bomb_x3',   'drill_bomb',   'item', 'coin', 450, 3, NULL, TRUE),
    ('item_spider_net_x3',   'spider_net',   'item', 'coin', 450, 3, NULL, TRUE),
    ('item_freeze_bomb_x3',  'freeze_bomb',  'item', 'coin', 500, 3, NULL, TRUE),
    ('item_air_strike_x3',   'air_strike',   'item', 'coin', 600, 3, NULL, TRUE),
    ('item_wind_stopper_x3', 'wind_stopper', 'item', 'coin', 300, 3, NULL, TRUE)
ON CONFLICT (offer_id) DO NOTHING;

-- Character unlocks: each character sold for coin or gem, once per player.
INSERT INTO shop_offers (offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player, is_active) VALUES
    ('unlock_tanko_coin', 'tanko', 'character_unlock', 'coin', 5000, 1, 1, TRUE),
    ('unlock_tanko_gem',  'tanko', 'character_unlock', 'gem',  300,  1, 1, TRUE),
    ('unlock_spark_coin', 'spark', 'character_unlock', 'coin', 5000, 1, 1, TRUE),
    ('unlock_spark_gem',  'spark', 'character_unlock', 'gem',  300,  1, 1, TRUE),
    ('unlock_flora_coin', 'flora', 'character_unlock', 'coin', 5000, 1, 1, TRUE),
    ('unlock_flora_gem',  'flora', 'character_unlock', 'gem',  300,  1, 1, TRUE)
ON CONFLICT (offer_id) DO NOTHING;

-- Gem packs.
INSERT INTO iap_products (product_id, platform_sku_id, gem_amount, bonus_gem_amount, price_usd, is_active) VALUES
    ('gems_100',  '{"android": "com.battlesquad.gems_100",  "ios": "com.battlesquad.gems_100"}',  100,  0,   0.99, TRUE),
    ('gems_550',  '{"android": "com.battlesquad.gems_550",  "ios": "com.battlesquad.gems_550"}',  500,  50,  4.99, TRUE),
    ('gems_1200', '{"android": "com.battlesquad.gems_1200", "ios": "com.battlesquad.gems_1200"}', 1000, 200, 9.99, TRUE)
ON CONFLICT (product_id) DO NOTHING;
