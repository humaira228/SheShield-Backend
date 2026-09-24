-- device_fingerprint: multi-account abuse detection only (e.g. a banned
-- user re-registering) -- never used for behavior scoring, matching, or
-- shown to any party. Reading it is logged to audit_log like any other
-- sensitive field access (see internal/user.GetDeviceFingerprint).
ALTER TABLE users ADD COLUMN device_fingerprint TEXT NOT NULL DEFAULT '';

-- Requester-side half of the double opt-in mutual-connection match (see
-- 011_sos_matches.sql's neighbor migration for the helper-side column
-- below). Defaults false and must be explicitly turned on outside the
-- pressure of an active emergency.
ALTER TABLE users ADD COLUMN discoverable_via_mutual_connections INTEGER NOT NULL DEFAULT 0;

-- Helper-side half. Lives on helper_status (this app's per-helper settings
-- table) rather than a new helper_profiles table -- restricted to
-- is_helper_verified helpers by the service layer; a real tier system
-- (Community/Verified/Professional) is unbuilt, so "Tier 3 only" isn't
-- enforced by this column alone yet.
ALTER TABLE helper_status ADD COLUMN mutual_connection_opt_in INTEGER NOT NULL DEFAULT 0;

-- Recorded when a requester cancels their own SOS (the 10s cancelable
-- countdown, or "I'm Safe" used as a cancel) -- feeds sos_rate_limits'
-- cancelled_count. NULL means never cancelled.
ALTER TABLE alerts ADD COLUMN cancel_reason TEXT;

-- Per-SOS AV recording consent, captured from the real-time consent prompt
-- (never pre-checked). av_recording_ref/av_retention_expiry stay unset
-- until actual recording storage ships -- that needs legal sign-off on
-- two-party consent law first (see the spec's §7), not just this column.
ALTER TABLE alerts ADD COLUMN av_consent INTEGER NOT NULL DEFAULT 0;
ALTER TABLE alerts ADD COLUMN av_recording_ref TEXT;
ALTER TABLE alerts ADD COLUMN av_retention_expiry TEXT;
