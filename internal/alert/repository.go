package alert

import (
	"database/sql"
	"time"
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

// Save writes the alert and all its deliveries atomically.
func (r *Repository) Save(a Alert) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op once Commit succeeds

	at := a.CreatedAt.Format(time.RFC3339)
	if _, err := tx.Exec(`
		INSERT INTO alerts (id, user_uid, latitude, longitude, accuracy_m, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, a.UserUID, nullable(a.Latitude), nullable(a.Longitude), nullable(a.AccuracyMeters), at,
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
