-- Rolling-window counters feeding the review-gated (never auto-restrict)
-- abuse signal described in the spec's bias safeguard: a user who cancels or
-- gets flagged 'false' on N+ SOS events in a window is queued for human
-- review (see internal/ratelimit), never automatically limited. One row per
-- (user, window_start) -- window_start is the window's own start time
-- truncated to a day, so "rolling 7-day" is approximated as a sliding set of
-- daily buckets summed over the last 7, rather than one row edited forever
-- (which would make "last 7 days" impossible to compute without a separate
-- event log).
CREATE TABLE IF NOT EXISTS sos_rate_limits (
    user_id          TEXT NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    window_start     TEXT NOT NULL,  -- date, truncated to day (YYYY-MM-DD)
    cancelled_count  INTEGER NOT NULL DEFAULT 0,
    false_count      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, window_start)
);
