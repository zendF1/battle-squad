-- 001_init_schema.up.sql

CREATE TABLE IF NOT EXISTS accounts (
    account_id VARCHAR(64) PRIMARY KEY,
    account_type VARCHAR(20) NOT NULL, -- guest, google, apple, linked
    status VARCHAR(20) NOT NULL, -- active, banned, deleted, pending_deletion
    role VARCHAR(20) NOT NULL DEFAULT 'player', -- player, admin
    primary_player_id VARCHAR(64),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS auth_identities (
    identity_id VARCHAR(64) PRIMARY KEY,
    account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    provider VARCHAR(20) NOT NULL, -- guest, google, apple
    provider_user_id VARCHAR(255) NOT NULL,
    email_hash VARCHAR(64),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(provider, provider_user_id)
);

CREATE TABLE IF NOT EXISTS player_profiles (
    player_id VARCHAR(64) PRIMARY KEY,
    account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    display_name VARCHAR(100) NOT NULL,
    level INT NOT NULL DEFAULT 1,
    exp INT NOT NULL DEFAULT 0,
    coin INT NOT NULL DEFAULT 0,
    gem INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS player_characters (
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    character_id VARCHAR(64) NOT NULL,
    unlocked_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (player_id, character_id)
);

CREATE TABLE IF NOT EXISTS inventory_items (
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    item_id VARCHAR(64) NOT NULL,
    quantity INT NOT NULL DEFAULT 0,
    source VARCHAR(50) NOT NULL,
    acquired_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE,
    PRIMARY KEY (player_id, item_id)
);

CREATE TABLE IF NOT EXISTS inventory_reservations (
    reservation_id VARCHAR(64) PRIMARY KEY,
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    match_id VARCHAR(64) NOT NULL,
    item_id VARCHAR(64) NOT NULL,
    quantity INT NOT NULL,
    status VARCHAR(20) NOT NULL, -- reserved, consumed, released
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS shop_offers (
    offer_id VARCHAR(64) PRIMARY KEY,
    item_id VARCHAR(64) NOT NULL,
    offer_type VARCHAR(30) NOT NULL, -- item, character_unlock, bundle
    price_currency VARCHAR(10) NOT NULL, -- coin, gem
    price_amount INT NOT NULL,
    quantity INT NOT NULL,
    limit_per_player INT,
    starts_at TIMESTAMP WITH TIME ZONE,
    ends_at TIMESTAMP WITH TIME ZONE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS shop_purchases (
    purchase_id VARCHAR(64) PRIMARY KEY,
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    offer_id VARCHAR(64) NOT NULL REFERENCES shop_offers(offer_id) ON DELETE CASCADE,
    price_currency VARCHAR(10) NOT NULL,
    price_amount INT NOT NULL,
    quantity_granted INT NOT NULL,
    status VARCHAR(20) NOT NULL, -- completed, failed, refunded
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS iap_products (
    product_id VARCHAR(64) PRIMARY KEY,
    platform_sku_id JSONB NOT NULL, -- {"android": "...", "ios": "..."}
    gem_amount INT NOT NULL,
    bonus_gem_amount INT NOT NULL DEFAULT 0,
    price_usd NUMERIC(10, 2) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS payment_transactions (
    transaction_id VARCHAR(255) PRIMARY KEY, -- store transaction ID (Apple trans ID, Google order ID)
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    product_id VARCHAR(64) NOT NULL REFERENCES iap_products(product_id) ON DELETE CASCADE,
    platform VARCHAR(20) NOT NULL, -- android, ios
    purchase_token TEXT NOT NULL,
    amount_usd NUMERIC(10, 2) NOT NULL,
    gem_granted INT NOT NULL,
    status VARCHAR(20) NOT NULL, -- pending, verified, failed, refunded
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    verified_at TIMESTAMP WITH TIME ZONE,
    refunded_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS economy_transactions (
    transaction_id VARCHAR(64) PRIMARY KEY,
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    currency VARCHAR(10) NOT NULL, -- coin, gem
    amount INT NOT NULL,
    balance_after INT NOT NULL,
    source VARCHAR(50) NOT NULL, -- match_reward, mission, iap, gift_code, shop_purchase, refund
    ref_id VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS gift_codes (
    code VARCHAR(50) PRIMARY KEY,
    reward_coin INT NOT NULL DEFAULT 0,
    reward_gem INT NOT NULL DEFAULT 0,
    reward_items JSONB NOT NULL DEFAULT '[]'::jsonb, -- array of item objects
    max_uses INT NOT NULL,
    used_count INT NOT NULL DEFAULT 0,
    expired_at TIMESTAMP WITH TIME ZONE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS gift_code_redemptions (
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    code VARCHAR(50) NOT NULL REFERENCES gift_codes(code) ON DELETE CASCADE,
    redeemed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (player_id, code)
);

CREATE TABLE IF NOT EXISTS missions (
    mission_id VARCHAR(64) PRIMARY KEY,
    type VARCHAR(20) NOT NULL, -- daily, achievement
    target VARCHAR(50) NOT NULL, -- play_match, win_match, damage_dealt, item_used, enemy_killed, terrain_destroyed
    required_value INT NOT NULL,
    reward_coin INT NOT NULL DEFAULT 0,
    reward_gem INT NOT NULL DEFAULT 0,
    reward_items JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS mission_progress (
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    mission_id VARCHAR(64) NOT NULL REFERENCES missions(mission_id) ON DELETE CASCADE,
    current_value INT NOT NULL DEFAULT 0,
    is_claimed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (player_id, mission_id)
);

CREATE TABLE IF NOT EXISTS player_reports (
    report_id VARCHAR(64) PRIMARY KEY,
    reporter_player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    target_player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    match_id VARCHAR(64),
    category VARCHAR(20) NOT NULL, -- cheat, abuse, afk, payment_fraud, other
    description TEXT,
    status VARCHAR(20) NOT NULL, -- open, reviewing, actioned, rejected
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    reviewed_at TIMESTAMP WITH TIME ZONE,
    reviewed_by VARCHAR(64)
);

CREATE TABLE IF NOT EXISTS account_bans (
    ban_id VARCHAR(64) PRIMARY KEY,
    account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    reason_code VARCHAR(50) NOT NULL,
    reason_text TEXT NOT NULL,
    source VARCHAR(30) NOT NULL, -- anti_cheat, moderator, payment_fraud, system
    starts_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    ends_at TIMESTAMP WITH TIME ZONE, -- NULL for permanent
    status VARCHAR(20) NOT NULL, -- active, expired, revoked
    evidence_ref_id VARCHAR(100),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS rank_seasons (
    season_id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    starts_at TIMESTAMP WITH TIME ZONE NOT NULL,
    ends_at TIMESTAMP WITH TIME ZONE NOT NULL,
    status VARCHAR(20) NOT NULL -- upcoming, active, ended, reward_granting, closed
);

CREATE TABLE IF NOT EXISTS player_ranks (
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    season_id VARCHAR(64) NOT NULL REFERENCES rank_seasons(season_id) ON DELETE CASCADE,
    rating INT NOT NULL DEFAULT 1000,
    tier VARCHAR(20) NOT NULL DEFAULT 'bronze', -- bronze, silver, gold, platinum, diamond, master
    division INT NOT NULL DEFAULT 3,
    wins INT NOT NULL DEFAULT 0,
    losses INT NOT NULL DEFAULT 0,
    draws INT NOT NULL DEFAULT 0,
    win_streak INT NOT NULL DEFAULT 0,
    highest_tier VARCHAR(20) NOT NULL DEFAULT 'bronze',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (player_id, season_id)
);

CREATE TABLE IF NOT EXISTS season_reward_claims (
    claim_id VARCHAR(64) PRIMARY KEY,
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    season_id VARCHAR(64) NOT NULL REFERENCES rank_seasons(season_id) ON DELETE CASCADE,
    tier VARCHAR(20) NOT NULL,
    reward_coin INT NOT NULL DEFAULT 0,
    reward_gem INT NOT NULL DEFAULT 0,
    reward_items JSONB NOT NULL DEFAULT '[]'::jsonb,
    claimed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(player_id, season_id)
);

CREATE TABLE IF NOT EXISTS match_histories (
    match_id VARCHAR(64) NOT NULL,
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    mode VARCHAR(20) NOT NULL, -- pvp_1v1, pvp_2v2
    map_id VARCHAR(64) NOT NULL,
    result VARCHAR(10) NOT NULL, -- win, loss, draw, no_contest
    damage INT NOT NULL DEFAULT 0,
    kills INT NOT NULL DEFAULT 0,
    accuracy NUMERIC(5, 2) NOT NULL DEFAULT 0.00,
    exp_gained INT NOT NULL DEFAULT 0,
    coin_gained INT NOT NULL DEFAULT 0,
    played_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (match_id, player_id)
);

CREATE TABLE IF NOT EXISTS match_snapshots (
    match_id VARCHAR(64) PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    server_node_id VARCHAR(100) NOT NULL,
    turn_index INT NOT NULL,
    current_player_id VARCHAR(64) NOT NULL,
    snapshot_version INT NOT NULL,
    state_blob JSONB NOT NULL,
    terrain_blob BYTEA NOT NULL,
    checksum VARCHAR(64) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS match_recovery_logs (
    log_id VARCHAR(64) PRIMARY KEY,
    match_id VARCHAR(64) NOT NULL,
    event_seq INT NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    event_payload JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(match_id, event_seq)
);

CREATE TABLE IF NOT EXISTS client_version_policies (
    platform VARCHAR(20) PRIMARY KEY, -- android, ios
    min_supported_version VARCHAR(20) NOT NULL,
    latest_version VARCHAR(20) NOT NULL,
    protocol_version INT NOT NULL,
    force_update BOOLEAN NOT NULL DEFAULT FALSE,
    soft_update_message TEXT,
    store_url VARCHAR(255) NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_player_ranks_rating ON player_ranks(season_id, rating DESC);
CREATE INDEX IF NOT EXISTS idx_economy_tx_player ON economy_transactions(player_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_match_history_player ON match_histories(player_id, played_at DESC);
CREATE INDEX IF NOT EXISTS idx_bans_account ON account_bans(account_id, status);
