CREATE TABLE IF NOT EXISTS users (
    uid                 TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    email               TEXT NOT NULL UNIQUE,
    password_hash       TEXT NOT NULL,
    phone               TEXT NOT NULL,
    country_code        TEXT NOT NULL,
    gender              TEXT NOT NULL,
    user_type           TEXT NOT NULL DEFAULT 'user',
    is_helper_verified  INTEGER NOT NULL DEFAULT 0,
    fcm_token           TEXT,
    created_at          TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS trusted_contacts (
    id           TEXT PRIMARY KEY,
    user_uid     TEXT NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    phone        TEXT NOT NULL,
    country_code TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_trusted_contacts_user_uid ON trusted_contacts(user_uid);
