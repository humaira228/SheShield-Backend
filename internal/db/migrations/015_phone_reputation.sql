-- Schema-only for now, same status as av_recording_ref (see
-- 014_trust_safety_fields.sql): the spec lists phone_reputation as one input
-- to the §5 behavior-scoring pipeline, but names no actual data source
-- (a third-party scam-number lookup service would need to be chosen and
-- contracted). Table exists so that integration is additive later, not a
-- schema change; nothing in this codebase writes or reads it yet.
CREATE TABLE IF NOT EXISTS phone_reputation (
    number       TEXT PRIMARY KEY,
    scam_score   REAL NOT NULL DEFAULT 0,
    provider_ref TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL
);
