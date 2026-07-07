DROP TABLE IF EXISTS player_loadouts;
DELETE FROM game_settings WHERE key IN ('matchmaking', 'elo', 'bot_difficulty');
