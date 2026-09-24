// Package matching owns sos_matches: which helpers an SOS was surfaced to,
// and which one (if any) currently holds the precise-access lock. See
// 011_sos_matches.sql for the full design rationale.
package matching

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

const (
	StatusPending  = "pending"
	StatusLocked   = "locked"
	StatusReleased = "released"

	AccessNone    = "none"
	AccessPrecise = "precise"
)

type Match struct {
	ID          string     `json:"id"`
	SOSID       string     `json:"sosId"`
	HelperID    string     `json:"helperId"`
	Status      string     `json:"status"`
	AccessLevel string     `json:"accessLevel"`
	CreatedAt   time.Time  `json:"createdAt"`
	AcceptedAt  *time.Time `json:"acceptedAt,omitempty"`
	DeclinedAt  *time.Time `json:"declinedAt,omitempty"`
}

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

// UpsertPending records that helperID was shown sosID -- called every time
// a helper's nearby-alerts poll includes this alert (see
// internal/helper.Service.NearbyAlerts). A no-op if a row already exists for
// this pair, whatever its current status: a helper who already declined or
// lost the race shouldn't silently flip back to 'pending' just because the
// alert is still showing up in someone's radius.
func (r *Repository) UpsertPending(sosID, helperID string) error {
	_, err := r.db.Exec(`
		INSERT INTO sos_matches (id, sos_id, helper_id, status, access_level, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(sos_id, helper_id) DO NOTHING`,
		newID(), sosID, helperID, StatusPending, AccessNone, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// Lock marks helperID's row 'locked' with precise access -- called right
// after internal/alert.Repository.Accept's compare-and-swap wins. Upserts
// rather than requiring a prior UpsertPending row to exist, since a helper
// could in principle accept before their own poll ever ran.
func (r *Repository) Lock(sosID, helperID string, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	_, err := r.db.Exec(`
		INSERT INTO sos_matches (id, sos_id, helper_id, status, access_level, created_at, accepted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(sos_id, helper_id) DO UPDATE SET
			status = ?, access_level = ?, accepted_at = ?`,
		newID(), sosID, helperID, StatusLocked, AccessPrecise, ts, ts,
		StatusLocked, AccessPrecise, ts,
	)
	return err
}

// ReleaseOthers flips every other pending row for sosID to 'released' --
// called alongside Lock so the helpers who lost the race see "Already
// matched" on their next poll instead of a stale 'pending' row.
func (r *Repository) ReleaseOthers(sosID, winningHelperID string, now time.Time) error {
	_, err := r.db.Exec(`
		UPDATE sos_matches
		SET status = ?, declined_at = ?
		WHERE sos_id = ? AND helper_id != ? AND status = ?`,
		StatusReleased, now.UTC().Format(time.RFC3339), sosID, winningHelperID, StatusPending,
	)
	return err
}

// Release moves the currently locked helper's row back to 'released' --
// called when that helper backs out (declines after accepting), a
// check-in/duress timeout, or a fast-track suspension force-releases them
// (see internal/report). Returns the released helper's id (empty if no row
// was locked) so the caller can notify them their access just ended.
func (r *Repository) Release(sosID string, now time.Time) (string, error) {
	var helperID string
	err := r.db.QueryRow(
		`SELECT helper_id FROM sos_matches WHERE sos_id = ? AND status = ?`,
		sosID, StatusLocked,
	).Scan(&helperID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	_, err = r.db.Exec(`
		UPDATE sos_matches SET status = ?, access_level = ?, declined_at = ?
		WHERE sos_id = ? AND helper_id = ?`,
		StatusReleased, AccessNone, now.UTC().Format(time.RFC3339), sosID, helperID,
	)
	return helperID, err
}

// LockedSOSForHelper returns the sos_id this helper currently holds the
// lock on, or "" if none -- used by the fast-track suspension flow (see the
// spec's §6) to find what needs force-releasing before the helper account
// itself is suspended.
func (r *Repository) LockedSOSForHelper(helperID string) (string, error) {
	var sosID string
	err := r.db.QueryRow(
		`SELECT sos_id FROM sos_matches WHERE helper_id = ? AND status = ?`,
		helperID, StatusLocked,
	).Scan(&sosID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return sosID, err
}

// ListBySOS returns every match row for one SOS, most recently created
// first -- the "who was this dispatched to, and what happened" view a
// moderator or the requester's own history would want.
func (r *Repository) ListBySOS(sosID string) ([]Match, error) {
	rows, err := r.db.Query(`
		SELECT id, sos_id, helper_id, status, access_level, created_at, accepted_at, declined_at
		FROM sos_matches WHERE sos_id = ? ORDER BY created_at DESC`, sosID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Match{}
	for rows.Next() {
		var m Match
		var createdAt string
		var acceptedAt, declinedAt sql.NullString
		if err := rows.Scan(&m.ID, &m.SOSID, &m.HelperID, &m.Status, &m.AccessLevel, &createdAt, &acceptedAt, &declinedAt); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if acceptedAt.Valid {
			t, _ := time.Parse(time.RFC3339, acceptedAt.String)
			m.AcceptedAt = &t
		}
		if declinedAt.Valid {
			t, _ := time.Parse(time.RFC3339, declinedAt.String)
			m.DeclinedAt = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
