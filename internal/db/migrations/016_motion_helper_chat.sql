-- 016: movement detection, helper response tracking, in-app SOS chat.
--
-- alerts.trigger_type   what fired the SOS (manual button, voice, or one of
--                       the on-device motion detectors). Helpers see a
--                       plain-language label + a coarse risk level derived
--                       from it -- never the requester's identity.
-- alerts.helper_progress  the accepted helper's self-reported stage
--                       (en_route -> arrived -> assisting), shown to the
--                       requester's side; strictly forward-only.
-- alerts.resolved_by    'requester' | 'helper'.
ALTER TABLE alerts ADD COLUMN trigger_type TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE alerts ADD COLUMN helper_progress TEXT;
ALTER TABLE alerts ADD COLUMN helper_progress_at TEXT;
ALTER TABLE alerts ADD COLUMN resolved_by TEXT;

-- One row per time a helper wins an accept race. This is the durable record
-- behind helper History and the dashboard stats: alerts.accepted_by_uid is
-- cleared when a helper backs out, so it cannot answer "what did I do".
-- No requester identity is stored here (pseudonymity, spec core principle).
-- outcome: 'active' | 'resolved' | 'released'
CREATE TABLE IF NOT EXISTS helper_responses (
    id           TEXT PRIMARY KEY,
    alert_id     TEXT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    helper_uid   TEXT NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    accepted_at  TEXT NOT NULL,
    arrived_at   TEXT,
    ended_at     TEXT,
    outcome      TEXT NOT NULL DEFAULT 'active'
);
CREATE INDEX IF NOT EXISTS idx_helper_responses_helper ON helper_responses(helper_uid, accepted_at);
CREATE INDEX IF NOT EXISTS idx_helper_responses_alert ON helper_responses(alert_id);

-- Motion events reported by the on-device detectors (fall / sprint /
-- struggle / post-fall inactivity). ONLY derived events are stored -- raw
-- accelerometer/gyro samples never leave the phone. These rows are audit
-- and transparency data for the user; nothing in the backend counts them to
-- restrict anyone (spec: frequency signals never escalate past a silent
-- flag). Purged after MOTION_RETENTION_DAYS (default 30).
CREATE TABLE IF NOT EXISTS motion_events (
    id            TEXT PRIMARY KEY,
    user_uid      TEXT NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    sos_id        TEXT REFERENCES alerts(id) ON DELETE SET NULL,
    type          TEXT NOT NULL,
    confidence    REAL NOT NULL,
    user_response TEXT NOT NULL DEFAULT 'none',  -- none | ok | help | timeout
    latitude      REAL,
    longitude     REAL,
    occurred_at   TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_motion_events_user ON motion_events(user_uid, created_at);

-- In-app contact between a requester and the helper who currently holds
-- their SOS (spec §5: in-app contact only by default). seq is the cursor
-- the clients poll with. flagged=1 means the body looked like a phone
-- number / handle -- a silent signal for human review, never a block.
CREATE TABLE IF NOT EXISTS sos_messages (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    id         TEXT NOT NULL UNIQUE,
    sos_id     TEXT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    sender_uid TEXT NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    flagged    INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sos_messages_sos ON sos_messages(sos_id, seq);
