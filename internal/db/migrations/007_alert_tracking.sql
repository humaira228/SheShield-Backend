-- Adds live-tracking to alerts: a public share token so a trusted contact can
-- open a no-login tracking page (see internal/alert.Handler.trackingPage),
-- an updated_at the app refreshes while the SOS is in progress, and the
-- 'resolved' status the sender reaches by marking themselves safe.
--
-- share_token is TEXT, not the numeric/hex alert id, specifically so the
-- public URL can't be used to enumerate other people's alerts -- see
-- internal/alert.Repository.NewShareToken.
ALTER TABLE alerts ADD COLUMN share_token TEXT;
ALTER TABLE alerts ADD COLUMN updated_at TEXT;
ALTER TABLE alerts ADD COLUMN resolved_at TEXT;

-- Alerts created before this migration have no token (NULL), and several of
-- those must be allowed to coexist, so this is a partial index: only rows
-- that do have a token need to be unique.
CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_share_token
    ON alerts(share_token) WHERE share_token IS NOT NULL;

-- 'resolved' joins 'active' and 'accepted' (006_helper_status.sql) as a
-- status alerts can reach -- set once the sender marks themselves safe (see
-- internal/alert.Repository.Resolve). There is no CHECK constraint on this
-- column, matching every other status column in this codebase.
