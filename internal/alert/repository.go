package alert

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

// ErrNotFound covers "no such alert" and "alert belongs to someone else" --
// the two cases a caller can't (and doesn't need to) tell apart, mirroring
// contact.Repository.Delete. ErrAlertNotActive means the alert exists and is
// the caller's, but is no longer 'active' (already resolved or accepted), so
// its location can't be updated and it can't be resolved again.
var (
	ErrNotFound       = errors.New("alert not found")
	ErrAlertNotActive = errors.New("alert is not active")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func nullable(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// nullableString is nullable's counterpart for share_token: an empty string
// must be stored as NULL, not "", so a rare token-generation failure on two
// different alerts never collides with the unique index on share_token.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

const shareTokenBytes = 16 // 128 bits -> ~22 URL-safe base64 chars

// NewShareToken returns a fresh, unguessable token for the /track/<token>
// public page, generated (and checked for uniqueness) before the alert it
// belongs to exists -- Trigger needs it up front, to put the tracking link
// in the SMS body it sends before ever calling Save. A collision at 128 bits
// of randomness is astronomically unlikely; the retry loop is cheap
// insurance, same spirit as contact.Repository.Exists' check-before-insert.
func (r *Repository) NewShareToken() (string, error) {
	for i := 0; i < 5; i++ {
		b := make([]byte, shareTokenBytes)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		token := base64.RawURLEncoding.EncodeToString(b)

		var n int
		if err := r.db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE share_token = ?`, token).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return token, nil
		}
	}
	return "", errors.New("could not generate a unique share token")
}

// Save writes the alert and all its deliveries atomically.
func (r *Repository) Save(a Alert) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op once Commit succeeds

	at := a.CreatedAt.Format(time.RFC3339)
	updatedAt := a.UpdatedAt.Format(time.RFC3339)
	if _, err := tx.Exec(`
		INSERT INTO alerts (id, user_uid, latitude, longitude, accuracy_m, created_at, share_token, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.UserUID, nullable(a.Latitude), nullable(a.Longitude), nullable(a.AccuracyMeters), at,
		nullableString(a.ShareToken), updatedAt,
	); err != nil {
		return err
	}

	for _, d := range a.Deliveries {
		if _, err := tx.Exec(`
			INSERT INTO alert_deliveries (id, alert_id, contact_id, name, phone, channel, status, error, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newID(), a.ID, d.ContactID, d.Name, d.Phone, d.Channel, d.Status, d.Error, at,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// notActiveOrNotFound tells apart the two reasons a conditional
// "... WHERE id = ? AND user_uid = ? AND status = 'active'" update can affect
// zero rows: the alert doesn't exist (or isn't this caller's), vs. it exists
// and is theirs but has already moved out of 'active'.
func (r *Repository) notActiveOrNotFound(alertID, ownerUID string) error {
	var status string
	err := r.db.QueryRow(
		`SELECT status FROM alerts WHERE id = ? AND user_uid = ?`, alertID, ownerUID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrAlertNotActive
}

// UpdateLocation refreshes an active alert's last-known position, e.g. from
// the app's periodic PATCH while an SOS is in progress. Only the alert's own
// owner can move it, and only while it is still 'active' -- once resolved or
// accepted, the location is frozen.
func (r *Repository) UpdateLocation(alertID, ownerUID string, lat, lng, accuracy *float64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.Exec(`
		UPDATE alerts
		SET latitude = ?, longitude = ?, accuracy_m = ?, updated_at = ?
		WHERE id = ? AND user_uid = ? AND status = 'active'`,
		nullable(lat), nullable(lng), nullable(accuracy), now, alertID, ownerUID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	return r.notActiveOrNotFound(alertID, ownerUID)
}

// Resolve marks an alert 'resolved' -- the sender saying they're safe. Like
// UpdateLocation, it only ever moves an alert out of 'active', so resolving
// twice is safe: the second call simply finds the alert no longer active and
// returns ErrAlertNotActive, without touching resolved_at again.
func (r *Repository) Resolve(alertID, ownerUID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.Exec(`
		UPDATE alerts
		SET status = 'resolved', resolved_at = ?, updated_at = ?
		WHERE id = ? AND user_uid = ? AND status = 'active'`,
		now, now, alertID, ownerUID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	return r.notActiveOrNotFound(alertID, ownerUID)
}

// GetByShareToken is the one read the public, no-login tracking page is
// allowed: it joins to users only for a first name, and never selects phone,
// email, or any other column that could identify the sender further.
func (r *Repository) GetByShareToken(token string) (*PublicAlertView, error) {
	row := r.db.QueryRow(`
		SELECT a.latitude, a.longitude, a.accuracy_m, a.status, a.updated_at, u.name
		FROM alerts a
		JOIN users u ON u.uid = a.user_uid
		WHERE a.share_token = ?`, token,
	)

	var v PublicAlertView
	var lat, lng, acc sql.NullFloat64
	var updatedAt sql.NullString
	var fullName string
	if err := row.Scan(&lat, &lng, &acc, &v.Status, &updatedAt, &fullName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if lat.Valid {
		x := lat.Float64
		v.Latitude = &x
	}
	if lng.Valid {
		x := lng.Float64
		v.Longitude = &x
	}
	if acc.Valid {
		x := acc.Float64
		v.AccuracyMeters = &x
	}
	if updatedAt.Valid {
		v.UpdatedAt = parseTime(updatedAt.String)
	}
	v.FirstName = firstName(fullName)
	return &v, nil
}
