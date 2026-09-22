-- One row per verification attempt by a helper. A rejected helper may submit
-- again, so a user can have several rows over time. Only the server (via the
-- admin command) can move a row out of 'pending'.
CREATE TABLE IF NOT EXISTS helper_verifications (
    id          TEXT PRIMARY KEY,
    user_uid    TEXT NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'pending',  -- 'pending' | 'approved' | 'rejected'
    nid_front   TEXT NOT NULL,                    -- file names inside <upload dir>/<id>/
    nid_back    TEXT NOT NULL,
    selfie      TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',         -- reviewer's reason when rejected
    created_at  TEXT NOT NULL,
    reviewed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_helper_verifications_user ON helper_verifications(user_uid);
CREATE INDEX IF NOT EXISTS idx_helper_verifications_status ON helper_verifications(status);

-- At most ONE pending submission per user, enforced by the database itself,
-- so two simultaneous uploads can't both get in.
CREATE UNIQUE INDEX IF NOT EXISTS idx_helper_verifications_one_pending
    ON helper_verifications(user_uid) WHERE status = 'pending';
