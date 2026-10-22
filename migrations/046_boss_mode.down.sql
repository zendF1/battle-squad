-- Undoes 046_boss_mode: the boss modes, the runs and the boss configs.
DELETE FROM config_game_modes WHERE mode_id IN ('boss_steel_robot', 'boss_lava_crab', 'boss_ice_witch',
    'boss:steel_robot', 'boss:lava_crab', 'boss:ice_witch');
DROP TABLE IF EXISTS player_boss_runs;
DROP TABLE IF EXISTS config_bosses;
