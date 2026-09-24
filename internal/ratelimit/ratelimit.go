// Package ratelimit tracks sos_rate_limits: the rolling-window cancelled/
// false-flagged SOS counters behind the spec's bias safeguard (§5/§5a).
// Crossing the threshold only ever files a system report for human review
// (see internal/report.Service.FileSystemFlag) -- nothing in this package
// can restrict SOS access on its own.
package ratelimit

import (
	"database/sql"
	"time"
)

// Threshold is intentionally low-friction to trip (routes to review, not
// restriction) but still a real signal -- tunable, per the spec's own
// "propose N=5" note.
const Threshold = 5

// WindowDays is the rolling window width. Implemented as a sum over that
// many daily buckets rather than one row edited forever, so "last N days"
// stays a plain range query instead of needing a separate event log.
const WindowDays = 7

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func dayBucket(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func (r *Repository) bump(userID, column string, now time.Time) error {
	_, err := r.db.Exec(`
		INSERT INTO sos_rate_limits (user_id, window_start, `+column+`)
		VALUES (?, ?, 1)
		ON CONFLICT(user_id, window_start) DO UPDATE SET `+column+` = `+column+` + 1`,
		userID, dayBucket(now),
	)
	return err
}

// MarkCancelled records a requester cancelling their own SOS (the 10s
// countdown cancel, or an immediate "I'm Safe" used as a cancel) -- called
// from internal/alert.Service.Resolve when the reason indicates a cancel,
// never automatically anything beyond incrementing this counter.
func (r *Repository) MarkCancelled(userID string, now time.Time) error {
	return r.bump(userID, "cancelled_count", now)
}

// MarkFalse records a report reviewer marking an SOS as false -- the one
// path into this counter, always human-gated (see internal/report.Service.Review).
func (r *Repository) MarkFalse(userID string, now time.Time) error {
	return r.bump(userID, "false_count", now)
}

// windowSum totals one column over the last WindowDays daily buckets,
// including today.
func (r *Repository) windowSum(userID, column string, now time.Time) (int, error) {
	since := dayBucket(now.AddDate(0, 0, -(WindowDays - 1)))
	var total int
	err := r.db.QueryRow(`
		SELECT COALESCE(SUM(`+column+`), 0) FROM sos_rate_limits
		WHERE user_id = ? AND window_start >= ?`,
		userID, since,
	).Scan(&total)
	return total, err
}

// NeedsReview reports whether userID has crossed Threshold cancelled-or-false
// SOS events in the rolling window -- the sole question this package answers.
// The caller (internal/alert.Service) decides what to do with a true result,
// which per the spec is always "file a system report," never a restriction.
func (r *Repository) NeedsReview(userID string, now time.Time) (bool, error) {
	cancelled, err := r.windowSum(userID, "cancelled_count", now)
	if err != nil {
		return false, err
	}
	falseCount, err := r.windowSum(userID, "false_count", now)
	if err != nil {
		return false, err
	}
	return cancelled+falseCount >= Threshold, nil
}
