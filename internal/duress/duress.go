// Package duress owns duress_signals: the spec's §2a mechanisms for a
// requester to escalate an active SOS beyond the current helper. Three
// types are real triggers today (manual_panic, hardware_pattern,
// missed_checkin) -- all client-detected, calling the same Trigger with no
// new backend infrastructure needed. safeword_voice is schema-only until
// on-device voice detection ships (a standalone ML feature, not a backend
// task).
package duress

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

type Type string

const (
	TypeHardwarePattern Type = "hardware_pattern"
	TypeSafewordVoice   Type = "safeword_voice" // schema-only -- see package doc
	TypeMissedCheckin   Type = "missed_checkin"
	TypeManualPanic     Type = "manual_panic"
)

func (t Type) Valid() bool {
	switch t {
	case TypeHardwarePattern, TypeSafewordVoice, TypeMissedCheckin, TypeManualPanic:
		return true
	default:
		return false
	}
}

var ErrInvalidType = errors.New("Unknown duress signal type.")
var ErrNotOwnAlert = errors.New("You can only trigger a duress signal on your own SOS.")

type Signal struct {
	ID          string    `json:"id"`
	SOSID       string    `json:"sosId"`
	Type        Type      `json:"type"`
	TriggeredAt time.Time `json:"triggeredAt"`
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

func (r *Repository) Insert(sosID string, t Type, now time.Time) (Signal, error) {
	s := Signal{ID: newID(), SOSID: sosID, Type: t, TriggeredAt: now}
	_, err := r.db.Exec(`
		INSERT INTO duress_signals (id, sos_id, type, triggered_at)
		VALUES (?, ?, ?, ?)`,
		s.ID, s.SOSID, string(s.Type), now.UTC().Format(time.RFC3339),
	)
	return s, err
}

// Active reports whether sosID has any duress signal at all -- the "duress
// signal active" flag surfaced to anyone already on the SOS (see the
// spec's §3 disclosure table and internal/alert.PublicAlertView). Computed
// live from the one source of truth rather than a denormalized column.
func (r *Repository) Active(sosID string) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM duress_signals WHERE sos_id = ?`, sosID).Scan(&n)
	return n > 0, err
}

// ListBySOS returns every signal fired for one SOS, oldest first.
func (r *Repository) ListBySOS(sosID string) ([]Signal, error) {
	rows, err := r.db.Query(`
		SELECT id, sos_id, type, triggered_at FROM duress_signals
		WHERE sos_id = ? ORDER BY triggered_at ASC`, sosID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Signal{}
	for rows.Next() {
		var s Signal
		var typ, triggeredAt string
		if err := rows.Scan(&s.ID, &s.SOSID, &typ, &triggeredAt); err != nil {
			return nil, err
		}
		s.Type = Type(typ)
		s.TriggeredAt, _ = time.Parse(time.RFC3339, triggeredAt)
		out = append(out, s)
	}
	return out, rows.Err()
}
