// Package sosmsg is the in-app channel between a requester and the one
// helper currently holding their SOS. Spec §5: "in-app contact only by
// default" -- neither side ever needs the other's real phone number to talk.
//
// Access rules (server-enforced, not UI-hidden):
//   - the requester may read/write while the alert is active or accepted, and
//     read (only) for a short time after it resolves;
//   - the helper may read/write only while they hold the alert
//     (status 'accepted' and accepted_by_uid = them). Resolving or backing
//     out revokes it immediately.
//   - senders are only ever labelled "requester" / "helper".
package sosmsg

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
)

const (
	MaxBodyRunes     = 500
	maxPerMinute     = 20
	readAfterResolve = 30 * time.Minute
)

var (
	ErrNoAccess  = errors.New("You don't have access to this conversation.")
	ErrEmpty     = errors.New("Message can't be empty.")
	ErrTooLong   = errors.New("Message is too long.")
	ErrRateLimit = errors.New("You're sending messages too fast.")
	ErrClosed    = errors.New("This emergency has ended, so the chat is closed.")
)

type Message struct {
	Seq       int64     `json:"seq"`
	ID        string    `json:"id"`
	From      string    `json:"from"` // "requester" | "helper"
	Mine      bool      `json:"mine"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type SendRequest struct {
	Body string `json:"body"`
}

var digitRun = regexp.MustCompile(`(?:\d[\s\-().]*){7,}`)
var handleLike = regexp.MustCompile(`(?i)(@[a-z0-9_.]{3,}|whats\s*app|telegram|imo\b|messenger|facebook|instagram)`)

// LooksLikeContactInfo flags messages that try to move the conversation out
// of the app (phone numbers, social handles). It only sets a silent flag for
// human review -- the message is still delivered (spec §5: silent flag first,
// never an automatic block).
func LooksLikeContactInfo(body string) bool {
	return digitRun.MatchString(body) || handleLike.MatchString(body)
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

type role int

const (
	roleNone role = iota
	roleRequester
	roleHelper
)

// access decides who uid is for this SOS and whether they may write.
func (r *Repository) access(sosID, uid string, now time.Time) (who role, canWrite bool, err error) {
	var owner, status string
	var holder, resolvedAt sql.NullString
	err = r.db.QueryRow(`
		SELECT user_uid, status, accepted_by_uid, resolved_at FROM alerts WHERE id = ?`, sosID,
	).Scan(&owner, &status, &holder, &resolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return roleNone, false, nil
	}
	if err != nil {
		return roleNone, false, err
	}
	live := status == "active" || status == "accepted"
	switch {
	case uid == owner:
		if live {
			return roleRequester, true, nil
		}
		if resolvedAt.Valid {
			if t, perr := time.Parse(time.RFC3339, resolvedAt.String); perr == nil && now.Sub(t) <= readAfterResolve {
				return roleRequester, false, nil
			}
		}
		return roleNone, false, nil
	case holder.Valid && holder.String == uid && status == "accepted":
		return roleHelper, true, nil
	}
	return roleNone, false, nil
}

func (r *Repository) Send(sosID, uid, body string, now time.Time) (Message, error) {
	who, canWrite, err := r.access(sosID, uid, now)
	if err != nil {
		return Message{}, err
	}
	if who == roleNone {
		return Message{}, ErrNoAccess
	}
	if !canWrite {
		return Message{}, ErrClosed
	}

	var n int
	if err := r.db.QueryRow(`
		SELECT COUNT(*) FROM sos_messages WHERE sender_uid = ? AND created_at > ?`,
		uid, now.Add(-time.Minute).UTC().Format(time.RFC3339),
	).Scan(&n); err != nil {
		return Message{}, err
	}
	if n >= maxPerMinute {
		return Message{}, ErrRateLimit
	}

	flagged := 0
	if LooksLikeContactInfo(body) {
		flagged = 1
	}
	m := Message{ID: newID(), Mine: true, Body: body, CreatedAt: now}
	if who == roleHelper {
		m.From = "helper"
	} else {
		m.From = "requester"
	}
	res, err := r.db.Exec(`
		INSERT INTO sos_messages (id, sos_id, sender_uid, body, flagged, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, sosID, uid, body, flagged, now.UTC().Format(time.RFC3339))
	if err != nil {
		return Message{}, err
	}
	m.Seq, _ = res.LastInsertId()
	return m, nil
}

// List returns messages with seq > after, oldest first.
func (r *Repository) List(sosID, uid string, after int64, now time.Time) ([]Message, error) {
	who, _, err := r.access(sosID, uid, now)
	if err != nil {
		return nil, err
	}
	if who == roleNone {
		return nil, ErrNoAccess
	}
	rows, err := r.db.Query(`
		SELECT m.seq, m.id, m.sender_uid, m.body, m.created_at, a.user_uid
		FROM sos_messages m JOIN alerts a ON a.id = m.sos_id
		WHERE m.sos_id = ? AND m.seq > ?
		ORDER BY m.seq ASC LIMIT 200`, sosID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Message{}
	for rows.Next() {
		var m Message
		var sender, at, owner string
		if err := rows.Scan(&m.Seq, &m.ID, &sender, &m.Body, &at, &owner); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, at)
		m.Mine = sender == uid
		if sender == owner {
			m.From = "requester"
		} else {
			m.From = "helper"
		}
		out = append(out, m)
	}
	return out, rows.Err()
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
	mux.Handle("GET /api/v1/sos/{id}/messages", auth(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/v1/sos/{id}/messages", auth(http.HandlerFunc(h.send)))
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoAccess):
		httpx.Err(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrClosed):
		httpx.Err(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrRateLimit):
		httpx.Err(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, ErrEmpty), errors.Is(err, ErrTooLong):
		httpx.Err(w, http.StatusBadRequest, err.Error())
	default:
		httpx.Err(w, http.StatusInternalServerError, "Something went wrong. Please try again.")
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	msgs, err := h.repo.List(r.PathValue("id"), uid, after, h.now())
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, msgs)
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	var req SendRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeErr(w, ErrEmpty)
		return
	}
	if utf8.RuneCountInString(body) > MaxBodyRunes {
		writeErr(w, ErrTooLong)
		return
	}
	m, err := h.repo.Send(r.PathValue("id"), uid, body, h.now())
	if err != nil {
		writeErr(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, m)
}
