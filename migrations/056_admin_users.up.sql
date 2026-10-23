-- Admin dashboard (cmd/admin) logins. cmd/migrate creates the first one from
-- ADMIN_BOOTSTRAP_EMAIL / ADMIN_BOOTSTRAP_PASSWORD when the table is empty.
CREATE TABLE IF NOT EXISTS admin_users (
    admin_id BIGSERIAL PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,          -- lower case (auth.NormalizeAdminEmail)
    password_hash TEXT NOT NULL,         -- bcrypt (auth.HashAdminPassword)
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMPTZ
);
