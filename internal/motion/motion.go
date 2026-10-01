// Package motion stores the DERIVED events the phone's on-device movement
// detectors report (fall, sprint, struggle, post-fall inactivity).
//
// Privacy and safety rules this package enforces:
//   - Raw accelerometer/gyroscope samples never reach the server; only a
//     typed event with a confidence score does.
//   - Events are transparency/audit data for the user. Nothing here counts
//     or scores events to restrict anyone: frequency-based signals must
//     never escalate past a silent flag without a human (spec §5). The
//     hourly cap below protects the database, not the user's SOS access.
//   - The user can list and delete their own history, and rows are purged
//     after a retention window (spec §13).
package motion

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
)

type Type string

const (
	TypeFall       Type = "fall"
	TypeSprint     Type = "sprint"
	TypeStruggle   Type = "struggle"
	TypeInactivity Type = "inactivity"
)

func (t Type) Valid() bool {
	switch t {
	case TypeFall, TypeSprint, TypeStruggle, TypeInactivity:
		return true
	}
	return false
}

// UserResponse is what the person did when the phone asked "are you OK?".
type UserResponse string

const (
	ResponseNone    UserResponse = "none"
	ResponseOK      UserResponse = "ok"      // "I'm fine" -> a false positive, useful for tuning
	ResponseHelp    UserResponse = "help"    // asked for help
	ResponseTimeout UserResponse = "timeout" // no answer -> SOS was sent
)

func (r UserResponse) Valid() bool {
	switch r {
	case ResponseNone, ResponseOK, ResponseHelp, ResponseTimeout:
		return true
	}
	return false
}

var (
	ErrInvalidType       = errors.New("Unknown motion event type.")
	ErrInvalidConfidence = errors.New("Confidence must be between 0 and 1.")
	ErrInvalidResponse   = errors.New("Unknown user response.")
	ErrBadLocation       = errors.New("Invalid location.")
	ErrTooMany           = errors.New("Too many motion events. Please try again later.")
)

// maxEventsPerHour caps writes per user so a buggy or hostile client cannot
// fill the table. It is NOT a behaviour score.
const maxEventsPerHour = 60

// maxClockSkew / maxEventAge bound the client-supplied occurredAt: events
// queued while offline are accepted for a day, anything else falls back to
// the server's clock.
const (
	maxClockSkew = 5 * time.Minute
	maxEventAge  = 24 * time.Hour
)

type ReportRequest struct {
	Type         Type         `json:"type"`
	Confidence   float64      `json:"confidence"`
	UserResponse UserResponse `json:"userResponse"`
	SOSID        string       `json:"sosId"`
	Latitude     *float64     `json:"latitude"`
	Longitude    *float64     `json:"longitude"`
	OccurredAt   *time.Time   `json:"occurredAt"`
}

type Event struct {
	ID           string       `json:"id"`
	Type         Type         `json:"type"`
	Confidence   float64      `json:"confidence"`
	UserResponse UserResponse `json:"userResponse"`
	SOSID        string       `json:"sosId,omitempty"`
	OccurredAt   time.Time    `json:"occurredAt"`
}

// Validate normalises and checks a request. now is injected for tests.
func (r *ReportRequest) Validate(now time.Time) (occurred time.Time, err error) {
	if !r.Type.Valid() {
		return time.Time{}, ErrInvalidType
	}
	if r.Confidence != r.Confidence || r.Confidence < 0 || r.Confidence > 1 { // NaN-safe
		return time.Time{}, ErrInvalidConfidence
	}
	if r.UserResponse == "" {
		r.UserResponse = ResponseNone
	}
	if !r.UserResponse.Valid() {
		return time.Time{}, ErrInvalidResponse
	}
	if (r.Latitude == nil) != (r.Longitude == nil) {
		return time.Time{}, ErrBadLocation
	}
	if r.Latitude != nil && (*r.Latitude < -90 || *r.Latitude > 90 || *r.Longitude < -180 || *r.Longitude > 180) {
		return time.Time{}, ErrBadLocation
	}
	occurred = now
	if r.OccurredAt != nil {
		t := r.OccurredAt.UTC()
		if t.Before(now.Add(maxClockSkew)) && t.After(now.Add(-maxEventAge)) {
			occurred = t
		}
	}
	return occurred, nil
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func nullable(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// Insert stores one event. sosID is only kept if it is this user's own alert
// (otherwise it is silently dropped -- a client must not be able to attach
// events to someone else's SOS).
func (r *Repository) Insert(uid string, req ReportRequest, occurred, now time.Time) (Event, error) {
	var n int
	if err := r.db.QueryRow(`
		SELECT COUNT(*) FROM motion_events WHERE user_uid = ? AND created_at > ?`,
		uid, now.Add(-time.Hour).UTC().Format(time.RFC3339),
	).Scan(&n); err != nil {
		return Event{}, err
	}
	if n >= maxEventsPerHour {
		return Event{}, ErrTooMany
	}

	var sos any
	if req.SOSID != "" {
		var owner string
		err := r.db.QueryRow(`SELECT user_uid FROM alerts WHERE id = ?`, req.SOSID).Scan(&owner)
		if err == nil && owner == uid {
			sos = req.SOSID
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Event{}, err
		}
	}

	e := Event{
		ID: newID(), Type: req.Type, Confidence: req.Confidence,
		UserResponse: req.UserResponse, OccurredAt: occurred,
	}
	if s, ok := sos.(string); ok {
		e.SOSID = s
	}
	_, err := r.db.Exec(`
		INSERT INTO motion_events
			(id, user_uid, sos_id, type, confidence, user_response, latitude, longitude, occurred_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, uid, sos, string(e.Type), e.Confidence, string(e.UserResponse),
		nullable(req.Latitude), nullable(req.Longitude),
		occurred.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339),
	)
	return e, err
}

// ListMine returns the caller's own events, newest first.
func (r *Repository) ListMine(uid string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(`
		SELECT id, type, confidence, user_response, COALESCE(sos_id, ''), occurred_at
		FROM motion_events WHERE user_uid = ?
		ORDER BY occurred_at DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var e Event
		var typ, resp, at string
		if err := rows.Scan(&e.ID, &typ, &e.Confidence, &resp, &e.SOSID, &at); err != nil {
			return nil, err
		}
		e.Type, e.UserResponse = Type(typ), UserResponse(resp)
		e.OccurredAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteMine erases every motion event of the caller (data-subject control).
func (r *Repository) DeleteMine(uid string) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM motion_events WHERE user_uid = ?`, uid)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeOlderThan enforces the retention window; returns rows removed.
func (r *Repository) PurgeOlderThan(cutoff time.Time) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM motion_events WHERE created_at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- HTTP -------------------------------------------------------------------

type Handler struct {
	repo *Repository
	now  func() time.Time
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (h *Handler) Register(mux *http.ServeMux, jwtSecret string) {
	auth := middleware.RequireAuth(jwtSecret)
	mux.Handle("POST /api/v1/motion/events", auth(http.HandlerFunc(h.report)))
	mux.Handle("GET /api/v1/motion/events", auth(http.HandlerFunc(h.list)))
	mux.Handle("DELETE /api/v1/motion/events", auth(http.HandlerFunc(h.deleteAll)))
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	var req ReportRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	now := h.now()
	occurred, err := req.Validate(now)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := h.repo.Insert(uid, req, occurred, now)
	switch {
	case errors.Is(err, ErrTooMany):
		httpx.Err(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not record the event.")
	default:
		httpx.JSON(w, http.StatusCreated, e)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := h.repo.ListMine(uid, limit)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Something went wrong. Please try again.")
		return
	}
	httpx.JSON(w, http.StatusOK, events)
}

func (h *Handler) deleteAll(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	if _, err := h.repo.DeleteMine(uid); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Something went wrong. Please try again.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
