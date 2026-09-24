-- Immutable trail of every sensitive-data access/mutation (accepting an SOS,
-- reviewing a report, reading a device_fingerprint, resolving a moderation
-- case, ...). Application code only ever INSERTs into this table -- there is
-- deliberately no UPDATE/DELETE path anywhere in internal/audit, which is
-- the only enforcement SQLite can offer short of a separate WORM store.
CREATE TABLE IF NOT EXISTS audit_log (
    id          TEXT PRIMARY KEY,
    actor_id    TEXT NOT NULL,  -- not a FK: an actor can be 'system' (automated flags)
    action      TEXT NOT NULL,
    target_id   TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_log_actor ON audit_log(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_target ON audit_log(target_id);
