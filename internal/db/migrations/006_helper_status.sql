-- One row per account tracking whether it is currently available to receive
-- nearby SOS alerts, and where/how far it should look. A missing row means
-- "never configured" -- the service treats that the same as inactive.
CREATE TABLE IF NOT EXISTS helper_status (
    user_uid    TEXT PRIMARY KEY REFERENCES users(uid) ON DELETE CASCADE,
    is_active   INTEGER NOT NULL DEFAULT 0,
    radius_km   REAL NOT NULL DEFAULT 3.0,
    latitude    REAL,
    longitude   REAL,
    updated_at  TEXT NOT NULL
);

-- Extend the existing alerts table (rather than a competing one) so helper
-- mode reads/writes the same SOS rows the alert package already owns.
-- 'active' is the only status alerts are created with today (see
-- internal/alert.Repository.Save); this just gives that implicit state a
-- name and adds the two states a helper accept can move it to.
ALTER TABLE alerts ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE alerts ADD COLUMN accepted_by_uid TEXT REFERENCES users(uid);
ALTER TABLE alerts ADD COLUMN accepted_at TEXT;

CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status);
