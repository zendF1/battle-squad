-- Player loadout (character + items selection, shared across all modes)
CREATE TABLE IF NOT EXISTS player_loadouts (
    player_id    VARCHAR(64) PRIMARY KEY REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    character_id VARCHAR(64) NOT NULL DEFAULT 'rookie',
    items        JSONB       NOT NULL DEFAULT '[]'::jsonb,
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Seed matchmaking config into game_settings
INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('matchmaking', '{"tickInterval":3,"baseRatingRange":100,"expandInterval":10,"expandStep":50,"maxRatingRange":300,"maxWaitTime":60,"botRatingModifier":0.5,"partyRatingStrategy":"max","weightedRatio":0.7}', 'json', 'Matchmaking queue configuration', 'matchmaking'),
    ('elo', '{"kFactor":32,"ratingFloor":0,"defaultRating":1000}', 'json', 'Elo rating configuration', 'matchmaking'),
    ('bot_difficulty', '{"tiers":{"bronze":{"accuracyError":15,"powerError":12,"decisionNoise":30,"useItemChance":0.3,"movementSmart":0.3},"silver":{"accuracyError":12,"powerError":10,"decisionNoise":25,"useItemChance":0.4,"movementSmart":0.4},"gold":{"accuracyError":9,"powerError":8,"decisionNoise":20,"useItemChance":0.55,"movementSmart":0.55},"platinum":{"accuracyError":6,"powerError":5,"decisionNoise":15,"useItemChance":0.7,"movementSmart":0.7},"diamond":{"accuracyError":4,"powerError":3,"decisionNoise":8,"useItemChance":0.85,"movementSmart":0.85},"master":{"accuracyError":2,"powerError":2,"decisionNoise":5,"useItemChance":0.9,"movementSmart":0.95}}}', 'json', 'Bot difficulty per rank tier', 'matchmaking')
ON CONFLICT (key) DO NOTHING;
