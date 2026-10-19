-- Removes the sell rate only while it still holds 022's value; the code falls
-- back to 0.25 without it.

DELETE FROM game_settings WHERE key = 'equipment.sell_rate' AND value = '0.25';
