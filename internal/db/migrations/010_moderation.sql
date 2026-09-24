-- One shared queue for both report directions (helper-reports-user and
-- user-reports-helper) -- reporter_role records which side filed it, but
-- review handling is identical either way. sos_id is optional: not every
-- report is tied to a specific SOS.
--
-- review_status: 'pending' | 'reviewing' | 'actioned' | 'dismissed'.
-- 'system' is a valid reporter_id for automated rate-limit flags (see
-- 013_rate_limits.sql / internal/ratelimit) -- these enter the exact same
-- queue human-filed reports do, never a separate auto-action path.
CREATE TABLE IF NOT EXISTS reports (
    id             TEXT PRIMARY KEY,
    reporter_id    TEXT NOT NULL,
    reported_id    TEXT NOT NULL,
    reporter_role  TEXT NOT NULL,  -- 'user' | 'helper' | 'system'
    category       TEXT NOT NULL,
    sos_id         TEXT,
    review_status  TEXT NOT NULL DEFAULT 'pending',
    reviewer_id    TEXT,
    resolution     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    reviewed_at    TEXT
);

CREATE INDEX IF NOT EXISTS idx_reports_reported ON reports(reported_id);
CREATE INDEX IF NOT EXISTS idx_reports_status ON reports(review_status);

-- One-tap, either direction, no explanation required, reversible by the
-- blocker at any time -- so this is a plain existence row, not a soft-delete
-- flag. (blocker_id, blocked_id) is the primary key: blocking twice is a
-- no-op, not an error, so the service layer can always "INSERT OR IGNORE".
CREATE TABLE IF NOT EXISTS blocks (
    blocker_id  TEXT NOT NULL,
    blocked_id  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (blocker_id, blocked_id)
);
