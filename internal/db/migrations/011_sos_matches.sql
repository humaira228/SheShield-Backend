-- Tracks every helper an SOS was surfaced to, not just the one who wins.
-- A row is upserted 'pending' the first time a helper's nearby-alerts poll
-- includes this alert (see internal/helper.Service.NearbyAlerts), which is
-- this app's existing "dispatch" mechanism -- no separate push fan-out
-- needed to start building the locking model on top of it.
--
-- status: 'pending' | 'locked' | 'released'. Exactly one row per sos_id may
-- ever be 'locked' at a time, enforced by internal/alert.Service.Accept's
-- transaction (first UPDATE ... WHERE status = 'active' wins, same
-- compare-and-swap alerts.accepted_by_uid already relied on -- this table
-- adds the *other* helpers' outcome, which nothing recorded before).
-- Releasing the locked row (decline-after-accept, timeout, or a fast-track
-- suspension -- see internal/report) flips it back to 'released' and
-- reopens the alert, so the next poll naturally re-surfaces it to the
-- standby helpers already sitting on 'released' rows.
--
-- relay_number/relay_expires_at/access_level are deliberately nullable and
-- unset for now -- issuing a real proxy phone number needs a telephony
-- vendor decision (Twilio Proxy or similar) that hasn't been made yet; the
-- column exists so that integration is additive, not a schema change.
CREATE TABLE IF NOT EXISTS sos_matches (
    id                TEXT PRIMARY KEY,
    sos_id            TEXT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    helper_id         TEXT NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    status            TEXT NOT NULL DEFAULT 'pending',
    access_level      TEXT NOT NULL DEFAULT 'none',  -- 'none' | 'precise' -- precise once locked
    relay_number      TEXT,
    relay_expires_at  TEXT,
    created_at        TEXT NOT NULL,
    accepted_at       TEXT,
    declined_at       TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sos_matches_sos_helper ON sos_matches(sos_id, helper_id);
CREATE INDEX IF NOT EXISTS idx_sos_matches_sos_status ON sos_matches(sos_id, status);
CREATE INDEX IF NOT EXISTS idx_sos_matches_helper ON sos_matches(helper_id);
