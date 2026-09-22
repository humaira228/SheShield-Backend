package helper

import (
	"database/sql"
	"errors"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func nullableFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// GetStatus returns the helper's saved status, or the zero-value default
// (inactive, DefaultRadiusKm) if they've never set one -- a missing row is
// not an error here, it's just "never configured".
func (r *Repository) GetStatus(uid string) (Status, error) {
	row := r.db.QueryRow(`
		SELECT is_active, radius_km, latitude, longitude, updated_at
		FROM helper_status WHERE user_uid = ?`, uid)

	var s Status
	var lat, lng sql.NullFloat64
	var updatedAt string
	err := row.Scan(&s.IsActive, &s.RadiusKm, &lat, &lng, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{IsActive: false, RadiusKm: DefaultRadiusKm}, nil
	}
	if err != nil {
		return Status{}, err
	}
	if lat.Valid {
		v := lat.Float64
		s.Latitude = &v
	}
	if lng.Valid {
		v := lng.Float64
		s.Longitude = &v
	}
	s.UpdatedAt = parseTime(updatedAt)
	return s, nil
}

// SetStatus upserts the helper's status row.
func (r *Repository) SetStatus(uid string, s Status) error {
	_, err := r.db.Exec(`
		INSERT INTO helper_status (user_uid, is_active, radius_km, latitude, longitude, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_uid) DO UPDATE SET
			is_active = excluded.is_active,
			radius_km = excluded.radius_km,
			latitude = excluded.latitude,
			longitude = excluded.longitude,
			updated_at = excluded.updated_at`,
		uid, s.IsActive, s.RadiusKm, nullableFloat(s.Latitude), nullableFloat(s.Longitude),
		s.UpdatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// ActiveAlerts returns every alert still open for a response, newest data
// first isn't required here -- the service sorts by computed distance, not
// creation order. Alerts with no location can't be matched against a
// helper's radius, so they're excluded here rather than filtered later.
func (r *Repository) ActiveAlerts() ([]activeAlert, error) {
	rows, err := r.db.Query(`
		SELECT id, user_uid, latitude, longitude, created_at
		FROM alerts
		WHERE status = 'active' AND latitude IS NOT NULL AND longitude IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []activeAlert{}
	for rows.Next() {
		var a activeAlert
		var lat, lng float64
		var createdAt string
		if err := rows.Scan(&a.ID, &a.UserUID, &lat, &lng, &createdAt); err != nil {
			return nil, err
		}
		a.Latitude, a.Longitude = &lat, &lng
		a.CreatedAt = parseTime(createdAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// acceptedRow is everything the handler needs to build an AcceptedAlert,
// read back from the requester's own account -- never the accepting
// helper's.
type acceptedRow struct {
	Latitude    float64
	Longitude   float64
	UserName    string
	Phone       string
	CountryCode string
}

// Accept is the one atomic operation in this package: exactly one concurrent
// caller can move an alert from 'active' to 'accepted'. It uses the same
// `UPDATE ... WHERE status = 'active'` + RowsAffected pattern already proven
// in verification.Repository.Decide -- correct here specifically because
// internal/db opens SQLite with SetMaxOpenConns(1), so the single connection
// genuinely serializes concurrent requests; the losing goroutines simply
// queue for the connection and see status != 'active' when their turn comes.
//
// Returns (row, true, nil) on a win, (zero, false, nil) if someone else
// already won or the alert doesn't exist / isn't active (the caller can't
// tell those apart, and doesn't need to -- both are just "not available").
func (r *Repository) Accept(alertID, helperUID string, now time.Time) (acceptedRow, bool, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return acceptedRow{}, false, err
	}
	defer tx.Rollback() // no-op once Commit succeeds

	res, err := tx.Exec(`
		UPDATE alerts
		SET status = 'accepted', accepted_by_uid = ?, accepted_at = ?
		WHERE id = ? AND status = 'active'`,
		helperUID, now.UTC().Format(time.RFC3339), alertID,
	)
	if err != nil {
		return acceptedRow{}, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return acceptedRow{}, false, nil
	}

	var row acceptedRow
	err = tx.QueryRow(`
		SELECT a.latitude, a.longitude, u.name, u.phone, u.country_code
		FROM alerts a JOIN users u ON u.uid = a.user_uid
		WHERE a.id = ?`, alertID,
	).Scan(&row.Latitude, &row.Longitude, &row.UserName, &row.Phone, &row.CountryCode)
	if err != nil {
		return acceptedRow{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return acceptedRow{}, false, err
	}
	return row, true, nil
}
