-- Removes the default catalog rows that were never bought; purchased rows stay
-- so the cascade does not erase purchase or payment history.
DELETE FROM shop_offers o
WHERE o.offer_id IN (
    'item_medkit_x3', 'item_teleport_x3', 'item_power_shot_x3', 'item_drill_bomb_x3',
    'item_spider_net_x3', 'item_freeze_bomb_x3', 'item_air_strike_x3', 'item_wind_stopper_x3',
    'unlock_tanko_coin', 'unlock_tanko_gem', 'unlock_spark_coin', 'unlock_spark_gem',
    'unlock_flora_coin', 'unlock_flora_gem'
)
AND NOT EXISTS (SELECT 1 FROM shop_purchases p WHERE p.offer_id = o.offer_id);

DELETE FROM iap_products i
WHERE i.product_id IN ('gems_100', 'gems_550', 'gems_1200')
AND NOT EXISTS (SELECT 1 FROM payment_transactions t WHERE t.product_id = i.product_id);
