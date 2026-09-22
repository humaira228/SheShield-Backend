package verification

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func isUniqueConstraintErr(err error) bool {
	// modernc.org/sqlite reports constraint failures as text rather than a
	// typed error, so match the message (same approach as internal/auth).
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

const submissionColumns = `id, user_uid, status, nid_front, nid_back, selfie, note, created_at, reviewed_at`

func scanSubmission(sc interface{ Scan(...any) error }) (Submission, error) {
	var s Submission
	var createdAt string
	var reviewedAt sql.NullString
	if err := sc.Scan(&s.ID, &s.UserUID, &s.Status, &s.NIDFront, &s.NIDBack, &s.Selfie, &s.Note, &createdAt, &reviewedAt); err != nil {
		return Submission{}, err
	}
	s.CreatedAt = parseTime(createdAt)
	if reviewedAt.Valid {
		t := parseTime(reviewedAt.String)
		s.ReviewedAt = &t
	}
	return s, nil
}

// Create stores a new pending submission. The database allows only one
// pending row per user, so a second simultaneous upload gets ErrAlreadyPending.
func (r *Repository) Create(s Submission) error {
	_, err := r.db.Exec(`
		INSERT INTO helper_verifications (id, user_uid, status, nid_front, nid_back, selfie, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, '', ?)`,
		s.ID, s.UserUID, s.Status, s.NIDFront, s.NIDBack, s.Selfie, s.CreatedAt.Format(time.RFC3339),
	)
	if isUniqueConstraintErr(err) {
		return ErrAlreadyPending
	}
	return err
}

func (r *Repository) LatestForUser(uid string) (Submission, bool, error) {
	row := r.db.QueryRow(`
		SELECT `+submissionColumns+`
		FROM helper_verifications WHERE user_uid = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1`, uid)
	s, err := scanSubmission(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Submission{}, false, nil
	}
	if err != nil {
		return Submission{}, false, err
	}
	return s, true, nil
}

func (r *Repository) CountForUser(uid string) (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM helper_verifications WHERE user_uid = ?`, uid).Scan(&n)
	return n, err
}

// ListPending returns the queue the admin works through, oldest first.
func (r *Repository) ListPending() ([]Review, error) {
	rows, err := r.db.Query(`
		SELECT v.id, v.user_uid, v.status, v.nid_front, v.nid_back, v.selfie, v.note, v.created_at, v.reviewed_at,
		       u.name, u.email, u.country_code, u.phone, u.user_type
		FROM helper_verifications v JOIN users u ON u.uid = v.user_uid
		WHERE v.status = 'pending'
		ORDER BY v.created_at ASC, v.rowid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Review{}
	for rows.Next() {
		rv, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rv)
	}
	return out, rows.Err()
}

// GetForReview returns one submission with the applicant's details.
func (r *Repository) GetForReview(id string) (Review, error) {
	row := r.db.QueryRow(`
		SELECT v.id, v.user_uid, v.status, v.nid_front, v.nid_back, v.selfie, v.note, v.created_at, v.reviewed_at,
		       u.name, u.email, u.country_code, u.phone, u.user_type
		FROM helper_verifications v JOIN users u ON u.uid = v.user_uid
		WHERE v.id = ?`, id)
	rv, err := scanReview(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Review{}, ErrNotFound
	}
	return rv, err
}

func scanReview(sc interface{ Scan(...any) error }) (Review, error) {
	var rv Review
	var createdAt, countryCode, phone string
	var reviewedAt sql.NullString
	if err := sc.Scan(
		&rv.ID, &rv.UserUID, &rv.Status, &rv.NIDFront, &rv.NIDBack, &rv.Selfie, &rv.Note, &createdAt, &reviewedAt,
		&rv.UserName, &rv.UserEmail, &countryCode, &phone, &rv.UserType,
	); err != nil {
		return Review{}, err
	}
	rv.CreatedAt = parseTime(createdAt)
	if reviewedAt.Valid {
		t := parseTime(reviewedAt.String)
		rv.ReviewedAt = &t
	}
	rv.UserPhone = countryCode + phone
	return rv, nil
}

// Decide approves or rejects a PENDING submission. Approving also marks the
// user as a verified helper, in the same transaction, so the two can never
// disagree. Deciding an already-decided submission is refused.
func (r *Repository) Decide(id string, approve bool, note string, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op once Commit succeeds

	status := StatusRejected
	if approve {
		status = StatusApproved
	}

	res, err := tx.Exec(`
		UPDATE helper_verifications
		SET status = ?, note = ?, reviewed_at = ?
		WHERE id = ? AND status = 'pending'`,
		status, note, now.UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either no such submission, or it was already decided.
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM helper_verifications WHERE id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		return ErrNotPending
	}

	if approve {
		if _, err := tx.Exec(`
			UPDATE users SET is_helper_verified = 1
			WHERE uid = (SELECT user_uid FROM helper_verifications WHERE id = ?)`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
