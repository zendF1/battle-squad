CREATE TABLE IF NOT EXISTS match_event_logs (
    id          BIGSERIAL PRIMARY KEY,
    match_id    TEXT NOT NULL,
    seq         BIGINT NOT NULL,
    event_type  TEXT NOT NULL,
    player_id   TEXT,
    data        JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_match_event_logs_match_id ON match_event_logs (match_id);
CREATE INDEX IF NOT EXISTS idx_match_event_logs_match_seq ON match_event_logs (match_id, seq);
