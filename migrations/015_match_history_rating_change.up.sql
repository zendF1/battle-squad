-- Rating change per match, read by GET /player/match-history
ALTER TABLE match_histories ADD COLUMN IF NOT EXISTS rating_change INT NOT NULL DEFAULT 0;
