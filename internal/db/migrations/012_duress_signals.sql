-- One row per duress signal fired during an SOS. type is a fixed enum (see
-- internal/duress.Type) covering every mechanism in the spec, but only
-- three are wired to real triggers for now: 'manual_panic' and
-- 'missed_checkin' need no new client infrastructure (a button tap and a
-- timeout respectively), and 'hardware_pattern' is just a client-detected
-- button sequence calling the same endpoint. 'safeword_voice' is schema-only
-- until on-device voice detection ships -- a real, separate ML feature, not
-- a backend task.
--
-- "Duress signal active" for anyone already on the SOS is computed live as
-- `EXISTS (SELECT 1 FROM duress_signals WHERE sos_id = ?)` rather than a
-- denormalized flag on alerts -- one row is the source of truth, no second
-- place to keep in sync.
CREATE TABLE IF NOT EXISTS duress_signals (
    id           TEXT PRIMARY KEY,
    sos_id       TEXT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    type         TEXT NOT NULL,
    triggered_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_duress_signals_sos ON duress_signals(sos_id);
