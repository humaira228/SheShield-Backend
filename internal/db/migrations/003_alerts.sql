-- One row per SOS the user triggers, plus one row per contact we tried to reach.
CREATE TABLE IF NOT EXISTS alerts (
    id          TEXT PRIMARY KEY,
    user_uid    TEXT NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    latitude    REAL,
    longitude   REAL,
    accuracy_m  REAL,
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_alerts_user_uid ON alerts(user_uid);

-- contact_id is deliberately NOT a foreign key: if the user later deletes the
-- contact, the record of who was alerted (and when) must survive. name/phone
-- are copied here for the same reason.
CREATE TABLE IF NOT EXISTS alert_deliveries (
    id          TEXT PRIMARY KEY,
    alert_id    TEXT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    contact_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    phone       TEXT NOT NULL,
    channel     TEXT NOT NULL,  -- 'device' (texted from the user's own SIM) | 'server'
    status      TEXT NOT NULL,  -- 'sent' | 'simulated' | 'failed'
    error       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_alert_deliveries_alert_id ON alert_deliveries(alert_id);
