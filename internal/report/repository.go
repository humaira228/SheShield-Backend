package report

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const reportColumns = `id, reporter_id, reported_id, reporter_role, category, sos_id,
	review_status, reviewer_id, resolution, created_at, reviewed_at`

func scanReport(row interface{ Scan(...any) error }) (Report, error) {
	var r Report
	var sosID, reviewerID sql.NullString
	var createdAt string
	var reviewedAt sql.NullString
	err := row.Scan(
		&r.ID, &r.ReporterID, &r.ReportedID, &r.ReporterRole, &r.Category, &sosID,
		&r.ReviewStatus, &reviewerID, &r.Resolution, &createdAt, &reviewedAt,
	)
	if err != nil {
		return Report{}, err
	}
	r.SOSID = sosID.String
	r.ReviewerID = reviewerID.String
	r.CreatedAt = parseTime(createdAt)
	if reviewedAt.Valid {
		t := parseTime(reviewedAt.String)
		r.ReviewedAt = &t
	}
	return r, nil
}

// Create files a new report -- from a person (reporterRole 'user'/'helper')
// or the system itself (see ReporterRoleSystem). Every report, whoever
// filed it, lands in the same 'pending' queue reviewed by report_status;
// there is no separate path for either direction or for automated flags.
func (r *Repository) Create(reporterID, reportedID, reporterRole, category, sosID string, now time.Time) (Report, error) {
	rep := Report{
		ID: newID(), ReporterID: reporterID, ReportedID: reportedID,
		ReporterRole: reporterRole, Category: category, SOSID: sosID,
		ReviewStatus: StatusPending, CreatedAt: now,
	}
	_, err := r.db.Exec(`
		INSERT INTO reports (id, reporter_id, reported_id, reporter_role, category, sos_id, review_status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rep.ID, rep.ReporterID, rep.ReportedID, rep.ReporterRole, rep.Category,
		nullableString(rep.SOSID), rep.ReviewStatus, now.UTC().Format(time.RFC3339),
	)
	return rep, err
}

// Get returns one report, mostly so a review call can check it exists and
// isn't already resolved.
func (r *Repository) Get(id string) (Report, error) {
	row := r.db.QueryRow(`SELECT `+reportColumns+` FROM reports WHERE id = ?`, id)
	return scanReport(row)
}

// Queue returns reports in a given status (typically 'pending'), oldest
// first -- the single shared moderation queue for both report directions
// and system flags (see the spec's §12).
func (r *Repository) Queue(status string) ([]Report, error) {
	rows, err := r.db.Query(`SELECT `+reportColumns+` FROM reports WHERE review_status = ? ORDER BY created_at ASC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Report{}
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rep)
	}
	return out, rows.Err()
}

// Review moves a report to 'actioned' or 'dismissed'. Only ever called by a
// human reviewer's request (see internal/report.Service.Review) -- there is
// no code path that calls this automatically.
func (r *Repository) Review(id, reviewerID, status, resolution string, now time.Time) error {
	_, err := r.db.Exec(`
		UPDATE reports SET review_status = ?, reviewer_id = ?, resolution = ?, reviewed_at = ?
		WHERE id = ?`,
		status, reviewerID, resolution, now.UTC().Format(time.RFC3339), id,
	)
	return err
}

// Block upserts a block -- blocking twice is a no-op, not an error.
func (r *Repository) Block(blockerID, blockedID string, now time.Time) error {
	_, err := r.db.Exec(`
		INSERT INTO blocks (blocker_id, blocked_id, created_at) VALUES (?, ?, ?)
		ON CONFLICT(blocker_id, blocked_id) DO NOTHING`,
		blockerID, blockedID, now.UTC().Format(time.RFC3339),
	)
	return err
}

// Unblock is always allowed -- reversible any time, per the spec's §5.
func (r *Repository) Unblock(blockerID, blockedID string) error {
	_, err := r.db.Exec(`DELETE FROM blocks WHERE blocker_id = ? AND blocked_id = ?`, blockerID, blockedID)
	return err
}

// IsBlocked reports whether either side has blocked the other -- used to
// keep a blocked pair from being matched/contacting each other again.
func (r *Repository) IsBlocked(userA, userB string) (bool, error) {
	var n int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM blocks
		WHERE (blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)`,
		userA, userB, userB, userA,
	).Scan(&n)
	return n > 0, err
}

// ListBlockedByMe returns everyone the caller has blocked -- the "manage
// blocks" screen's data source.
func (r *Repository) ListBlockedByMe(blockerID string) ([]Block, error) {
	rows, err := r.db.Query(`SELECT blocker_id, blocked_id, created_at FROM blocks WHERE blocker_id = ? ORDER BY created_at DESC`, blockerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Block{}
	for rows.Next() {
		var b Block
		var createdAt string
		if err := rows.Scan(&b.BlockerID, &b.BlockedID, &createdAt); err != nil {
			return nil, err
		}
		b.CreatedAt = parseTime(createdAt)
		out = append(out, b)
	}
	return out, rows.Err()
}
