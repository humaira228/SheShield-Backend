package helper

// Helper response lifecycle after an accept: live position, progress stages
// (en route -> arrived -> assisting), resolving, plus the History and
// dashboard stats derived from helper_responses (migration 016).

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/trigger"
)

const (
	ProgressEnRoute   = "en_route"
	ProgressArrived   = "arrived"
	ProgressAssisting = "assisting"
)

var progressRank = map[string]int{
	"":                0,
	ProgressEnRoute:   1,
	ProgressArrived:   2,
	ProgressAssisting: 3,
}

var (
	ErrInvalidStage        = errors.New("Unknown response stage.")
	ErrBadProgress         = errors.New("You can't move back to an earlier stage.")
	ErrNeedArrival         = errors.New("Mark yourself as arrived before resolving this alert.")
	ErrDuressBlocksResolve = errors.New("A duress signal is active. Only the person in danger or emergency services can close this alert.")
)

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- types ------------------------------------------------------------------

// MyResponse is the helper's current accepted alert plus how far along they
// are. Embeds AcceptedAlert so the JSON is a superset of the accept response.
type MyResponse struct {
	AcceptedAlert
	Progress     string    `json:"progress"`
	Trigger      string    `json:"trigger"`
	Label        string    `json:"label"`
	RiskLevel    string    `json:"riskLevel"`
	DuressActive bool      `json:"duressActive"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// LiveState is what the accepted helper polls for: the requester's current
// position (only while the alert is still 'accepted' -- access is revoked the
// moment it resolves), the helper's own stage, and the safety flags.
type LiveState struct {
	Status           string     `json:"status"` // accepted | resolved | active
	Latitude         *float64   `json:"latitude,omitempty"`
	Longitude        *float64   `json:"longitude,omitempty"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	Progress         string     `json:"progress"`
	DuressActive     bool       `json:"duressActive"`
	ConnectivityLost bool       `json:"connectivityLost"`
	ResolvedAt       *time.Time `json:"resolvedAt,omitempty"`
}

type Stats struct {
	Responses          int      `json:"responses"`
	Completed          int      `json:"completed"`
	Resolved           int      `json:"resolved"`
	SuccessRate        int      `json:"successRate"` // percent of completed responses that ended resolved
	AvgResponseMinutes *float64 `json:"avgResponseMinutes"`
}

type HistoryItem struct {
	ID              string     `json:"id"`
	AlertID         string     `json:"alertId"`
	Trigger         string     `json:"trigger"`
	Label           string     `json:"label"`
	AcceptedAt      time.Time  `json:"acceptedAt"`
	ArrivedAt       *time.Time `json:"arrivedAt,omitempty"`
	EndedAt         *time.Time `json:"endedAt,omitempty"`
	Outcome         string     `json:"outcome"` // active | resolved | released
	ResponseMinutes *float64   `json:"responseMinutes,omitempty"`
}

type SetProgressRequest struct {
	Status string `json:"status"`
}

// --- store ------------------------------------------------------------------

type ResponseStore interface {
	Current(helperUID string) (*MyResponse, error)
	Live(alertID, helperUID string) (LiveState, error)
	SetProgress(alertID, helperUID, next string, now time.Time) error
	Resolve(alertID, helperUID string, now time.Time) error
	Stats(helperUID string) (Stats, error)
	History(helperUID string, limit int) ([]HistoryItem, error)
}

func (r *Repository) Current(helperUID string) (*MyResponse, error) {
	var m MyResponse
	var accepted, updated string
	var prog sql.NullString
	err := r.db.QueryRow(`
		SELECT a.id, u.name, u.phone, u.country_code, u.uid,
		       COALESCE(a.latitude, 0), COALESCE(a.longitude, 0),
		       COALESCE(a.accepted_at, a.created_at), a.helper_progress, a.trigger_type,
		       COALESCE(a.updated_at, a.created_at),
		       EXISTS (SELECT 1 FROM duress_signals d WHERE d.sos_id = a.id)
		FROM alerts a JOIN users u ON u.uid = a.user_uid
		WHERE a.accepted_by_uid = ? AND a.status = 'accepted'
		ORDER BY a.accepted_at DESC LIMIT 1`, helperUID,
	).Scan(&m.ID, &m.UserName, &m.Phone, &m.CountryCode, &m.RequesterUID,
		&m.Latitude, &m.Longitude, &accepted, &prog, &m.Trigger, &updated, &m.DuressActive)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.AcceptedAt = parseTime(accepted)
	m.UpdatedAt = parseTime(updated)
	m.Progress = prog.String
	m.Trigger = trigger.Normalize(m.Trigger)
	m.Label = trigger.Label(m.Trigger)
	m.RiskLevel = trigger.Risk(m.Trigger, m.DuressActive)
	return &m, nil
}

func (r *Repository) Live(alertID, helperUID string) (LiveState, error) {
	var (
		ls       LiveState
		lat, lng sql.NullFloat64
		updated  string
		prog     sql.NullString
		resolved sql.NullString
	)
	err := r.db.QueryRow(`
		SELECT a.status, a.latitude, a.longitude, COALESCE(a.updated_at, a.created_at),
		       a.helper_progress, a.resolved_at,
		       EXISTS (SELECT 1 FROM duress_signals d WHERE d.sos_id = a.id)
		FROM alerts a WHERE a.id = ? AND a.accepted_by_uid = ?`, alertID, helperUID,
	).Scan(&ls.Status, &lat, &lng, &updated, &prog, &resolved, &ls.DuressActive)
	if errors.Is(err, sql.ErrNoRows) {
		return LiveState{}, ErrNotYourMatch
	}
	if err != nil {
		return LiveState{}, err
	}
	ls.UpdatedAt = parseTime(updated)
	ls.Progress = prog.String
	if resolved.Valid {
		t := parseTime(resolved.String)
		ls.ResolvedAt = &t
	}
	// Precise position is only ever handed out while the helper still holds
	// the lock (spec §2: RESOLVED -> all access auto-revoked).
	if ls.Status == "accepted" {
		if lat.Valid && lng.Valid {
			la, lo := lat.Float64, lng.Float64
			ls.Latitude, ls.Longitude = &la, &lo
		}
		ls.ConnectivityLost = time.Since(ls.UpdatedAt) > safetyConnectivityLostAfter
	}
	return ls, nil
}

func (r *Repository) SetProgress(alertID, helperUID, next string, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var cur sql.NullString
	err = tx.QueryRow(`
		SELECT helper_progress FROM alerts
		WHERE id = ? AND accepted_by_uid = ? AND status = 'accepted'`, alertID, helperUID,
	).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotYourMatch
	}
	if err != nil {
		return err
	}
	switch {
	case progressRank[next] == progressRank[cur.String]:
		return nil // idempotent: a double tap must not be an error
	case progressRank[next] < progressRank[cur.String]:
		return ErrBadProgress
	}

	ts := now.UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
		UPDATE alerts SET helper_progress = ?, helper_progress_at = ?
		WHERE id = ?`, next, ts, alertID); err != nil {
		return err
	}
	if progressRank[next] >= progressRank[ProgressArrived] {
		if _, err := tx.Exec(`
			UPDATE helper_responses SET arrived_at = ?
			WHERE alert_id = ? AND helper_uid = ? AND outcome = 'active' AND arrived_at IS NULL`,
			ts, alertID, helperUID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) Resolve(alertID, helperUID string, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var cur sql.NullString
	var duress bool
	err = tx.QueryRow(`
		SELECT a.helper_progress,
		       EXISTS (SELECT 1 FROM duress_signals d WHERE d.sos_id = a.id)
		FROM alerts a
		WHERE a.id = ? AND a.accepted_by_uid = ? AND a.status = 'accepted'`, alertID, helperUID,
	).Scan(&cur, &duress)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotYourMatch
	}
	if err != nil {
		return err
	}
	if progressRank[cur.String] < progressRank[ProgressArrived] {
		return ErrNeedArrival
	}
	if duress {
		return ErrDuressBlocksResolve
	}

	ts := now.UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
		UPDATE alerts SET status = 'resolved', resolved_at = ?, updated_at = ?, resolved_by = 'helper'
		WHERE id = ?`, ts, ts, alertID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE helper_responses SET outcome = 'resolved', ended_at = ?
		WHERE alert_id = ? AND helper_uid = ? AND outcome = 'active'`, ts, alertID, helperUID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE sos_matches SET status = 'released', access_level = 'none', declined_at = ?
		WHERE sos_id = ? AND helper_id = ? AND status = 'locked'`, ts, alertID, helperUID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) Stats(helperUID string) (Stats, error) {
	var st Stats
	var released int
	if err := r.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN outcome = 'resolved' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN outcome = 'released' THEN 1 ELSE 0 END), 0)
		FROM helper_responses WHERE helper_uid = ?`, helperUID,
	).Scan(&st.Responses, &st.Resolved, &released); err != nil {
		return Stats{}, err
	}
	st.Completed = st.Resolved + released
	if st.Completed > 0 {
		st.SuccessRate = int(math.Round(float64(st.Resolved) * 100 / float64(st.Completed)))
	}

	rows, err := r.db.Query(`
		SELECT accepted_at, arrived_at FROM helper_responses
		WHERE helper_uid = ? AND arrived_at IS NOT NULL`, helperUID)
	if err != nil {
		return Stats{}, err
	}
	defer rows.Close()
	var total float64
	var n int
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return Stats{}, err
		}
		if d := parseTime(b).Sub(parseTime(a)); d >= 0 {
			total += d.Minutes()
			n++
		}
	}
	if err := rows.Err(); err != nil {
		return Stats{}, err
	}
	if n > 0 {
		avg := math.Round(total/float64(n)*10) / 10
		st.AvgResponseMinutes = &avg
	}
	return st, nil
}

func (r *Repository) History(helperUID string, limit int) ([]HistoryItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(`
		SELECT id, alert_id, trigger_type, accepted_at, arrived_at, ended_at, outcome
		FROM helper_responses WHERE helper_uid = ?
		ORDER BY accepted_at DESC LIMIT ?`, helperUID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HistoryItem{}
	for rows.Next() {
		var h HistoryItem
		var acc string
		var arr, end sql.NullString
		if err := rows.Scan(&h.ID, &h.AlertID, &h.Trigger, &acc, &arr, &end, &h.Outcome); err != nil {
			return nil, err
		}
		h.Trigger = trigger.Normalize(h.Trigger)
		h.Label = trigger.Label(h.Trigger)
		h.AcceptedAt = parseTime(acc)
		if arr.Valid {
			t := parseTime(arr.String)
			h.ArrivedAt = &t
			m := math.Round(t.Sub(h.AcceptedAt).Minutes()*10) / 10
			if m >= 0 {
				h.ResponseMinutes = &m
			}
		}
		if end.Valid {
			t := parseTime(end.String)
			h.EndedAt = &t
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// --- service ----------------------------------------------------------------

// WithResponses wires the response-lifecycle store (same *Repository as the
// other stores in cmd/api/main.go).
func (s *Service) WithResponses(rs ResponseStore) *Service {
	s.responses = rs
	return s
}

func (s *Service) verifiedHelper(uid string) error {
	user, err := s.users.FindByUID(uid)
	if err != nil {
		return err
	}
	if !isHelper(user) || !user.IsHelperVerified {
		return ErrNotHelper
	}
	return nil
}

func (s *Service) CurrentResponse(uid string) (*MyResponse, error) {
	if err := s.verifiedHelper(uid); err != nil {
		return nil, err
	}
	return s.responses.Current(uid)
}

func (s *Service) Live(uid, alertID string) (LiveState, error) {
	if err := s.verifiedHelper(uid); err != nil {
		return LiveState{}, err
	}
	return s.responses.Live(alertID, uid)
}

func (s *Service) SetProgress(uid, alertID, stage string) error {
	if progressRank[stage] == 0 {
		return ErrInvalidStage
	}
	if err := s.verifiedHelper(uid); err != nil {
		return err
	}
	return s.responses.SetProgress(alertID, uid, stage, s.now())
}

func (s *Service) ResolveAsHelper(uid, alertID string) error {
	if err := s.verifiedHelper(uid); err != nil {
		return err
	}
	return s.responses.Resolve(alertID, uid, s.now())
}

func (s *Service) Stats(uid string) (Stats, error) {
	if err := s.verifiedHelper(uid); err != nil {
		return Stats{}, err
	}
	return s.responses.Stats(uid)
}

func (s *Service) History(uid string, limit int) ([]HistoryItem, error) {
	if err := s.verifiedHelper(uid); err != nil {
		return nil, err
	}
	return s.responses.History(uid, limit)
}

// --- http -------------------------------------------------------------------

func (h *Handler) registerResponses(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/helper/stats", auth(http.HandlerFunc(h.stats)))
	mux.Handle("GET /api/v1/helper/history", auth(http.HandlerFunc(h.history)))
	mux.Handle("GET /api/v1/helper/responses/current", auth(http.HandlerFunc(h.current)))
	mux.Handle("GET /api/v1/helper/alerts/{id}/live", auth(http.HandlerFunc(h.live)))
	mux.Handle("POST /api/v1/helper/alerts/{id}/progress", auth(http.HandlerFunc(h.progress)))
	mux.Handle("POST /api/v1/helper/alerts/{id}/resolve", auth(http.HandlerFunc(h.resolve)))
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	st, err := h.svc.Stats(uid)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.History(uid, limit)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, items)
}

// current returns the helper's in-progress response, or an empty envelope
// (no "data" key) when they aren't holding an alert.
func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	m, err := h.svc.CurrentResponse(uid)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	if m == nil {
		httpx.JSON(w, http.StatusOK, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, m)
}

func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	ls, err := h.svc.Live(uid, r.PathValue("id"))
	if err != nil {
		writeSvcError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ls)
}

func (h *Handler) progress(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	var req SetProgressRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if err := h.svc.SetProgress(uid, r.PathValue("id"), req.Status); err != nil {
		if errors.Is(err, ErrInvalidStage) {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
		writeSvcError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	if err := h.svc.ResolveAsHelper(uid, r.PathValue("id")); err != nil {
		writeSvcError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
