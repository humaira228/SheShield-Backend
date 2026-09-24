// Package audit is the immutable trail every sensitive-data access or
// moderation action writes to. There is deliberately no Update or Delete
// here -- callers only ever Log something, and the only reader is
// internal/report's moderation queue (a case's audit trail) and any future
// admin tooling.
package audit

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

type Entry struct {
	ID        string    `json:"id"`
	ActorID   string    `json:"actorId"`
	Action    string    `json:"action"`
	TargetID  string    `json:"targetId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Common action names, so call sites can't typo a string that a future grep
// for "who accepted this SOS" would miss.
const (
	ActionSOSCreated      = "sos.created"
	ActionSOSResolved     = "sos.resolved"
	ActionSOSAccepted     = "sos.accepted"
	ActionSOSReleased     = "sos.released"
	ActionDuressTriggered = "duress.triggered"
	ActionReportFiled     = "report.filed"
	ActionReportReviewed  = "report.reviewed"
	ActionBlockCreated    = "block.created"
	ActionBlockRemoved    = "block.removed"
	ActionHelperSuspended = "helper.suspended"
	ActionFingerprintRead = "user.device_fingerprint.read"
)

type Logger struct {
	db *sql.DB
}

func NewLogger(db *sql.DB) *Logger {
	return &Logger{db: db}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Log records one immutable entry. Best-effort by design -- a failed audit
// write must never be the reason a real action (accepting an SOS, filing a
// report) fails, so callers log Log's own error and move on rather than
// propagating it. actorID is "system" for automated actions (e.g. a
// rate-limit flag), not a real user id.
func (l *Logger) Log(actorID, action, targetID string) error {
	_, err := l.db.Exec(`
		INSERT INTO audit_log (id, actor_id, action, target_id, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		newID(), actorID, action, targetID, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// ForTarget returns every logged action against one target (a user id, an
// sos id, ...), oldest first -- the audit trail a moderator reviewing a
// report would want.
func (l *Logger) ForTarget(targetID string) ([]Entry, error) {
	rows, err := l.db.Query(`
		SELECT id, actor_id, action, target_id, created_at
		FROM audit_log WHERE target_id = ? ORDER BY created_at ASC`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Entry{}
	for rows.Next() {
		var e Entry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.TargetID, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, e)
	}
	return out, rows.Err()
}
