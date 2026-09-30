// Package adminapi is the HTTP twin of cmd/admin/reports.go: the same
// queue/show/review/suspend/fingerprint operations, reachable over the
// network instead of only from a shell with file access to the server.
// It exists specifically so a real reviewer UI (the Flutter admin
// dashboard) can be built -- see internal/report/handler.go's Register,
// whose doc comment named this package before it existed.
//
// Nothing in here loosens any rule the rest of the backend enforces: the
// bias safeguard (only a human ever moves a report past 'pending', see
// internal/report.Service.Review), the fast-track suspension's own
// immediate-then-reviewed shape, and the audit trail are all unchanged --
// this package only adds a second, narrower-gated transport for the exact
// same operations cmd/admin already offered.
package adminapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zannatulmaliha/sheshield-backend/internal/audit"
	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
	"github.com/zannatulmaliha/sheshield-backend/internal/report"
	"github.com/zannatulmaliha/sheshield-backend/internal/verification"
)

// SuspendFunc performs the spec's §6 fast-track: immediately suspend a
// helper and force-release any SOS they currently hold. A plain function
// type (not an interface) so cmd/api/main.go can close over the several
// repositories it already constructs there -- the exact same composition
// cmd/admin/reports.go's suspendHelper does -- without this package
// importing internal/auth, internal/helper and internal/matching just to
// call three methods once.
type SuspendFunc func(uid, reason, actorID string) (releasedSOSID string, err error)

type Handler struct {
	reports       *report.Service
	audit         *audit.Logger
	suspend       SuspendFunc
	verifications *verification.Repository
	uploadDir     string
}

func NewHandler(reports *report.Service, auditLog *audit.Logger, suspend SuspendFunc, verifications *verification.Repository, uploadDir string) *Handler {
	return &Handler{reports: reports, audit: auditLog, suspend: suspend, verifications: verifications, uploadDir: uploadDir}
}

// Register wires every /api/v1/admin/* route behind RequireAdminKey. Call
// this only when an admin key is actually configured -- cmd/api/main.go
// skips it entirely otherwise, so an unconfigured deployment has no admin
// HTTP surface at all, not one that exists but always 401s.
func (h *Handler) Register(mux *http.ServeMux, adminKey string) {
	admin := middleware.RequireAdminKey(adminKey)
	mux.Handle("GET /api/v1/admin/reports", admin(http.HandlerFunc(h.queue)))
	mux.Handle("GET /api/v1/admin/reports/{id}", admin(http.HandlerFunc(h.show)))
	mux.Handle("POST /api/v1/admin/reports/{id}/review", admin(http.HandlerFunc(h.review)))
	mux.Handle("POST /api/v1/admin/helpers/{uid}/suspend", admin(http.HandlerFunc(h.suspendHelper)))
	mux.Handle("GET /api/v1/admin/verifications", admin(http.HandlerFunc(h.verificationQueue)))
	mux.Handle("GET /api/v1/admin/verifications/{id}", admin(http.HandlerFunc(h.verificationDetail)))
	mux.Handle("GET /api/v1/admin/verifications/{id}/images/{kind}", admin(http.HandlerFunc(h.verificationImage)))
	mux.Handle("POST /api/v1/admin/verifications/{id}/decision", admin(http.HandlerFunc(h.verificationDecision)))
}

// queue returns the pending moderation queue -- one shared list regardless
// of reporter_role, same as report.Service.Queue.
func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	list, err := h.reports.Queue()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not load the queue.")
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

// reportDetail is what GET /admin/reports/{id} returns: the report itself
// plus the reported account's audit trail -- exactly what a reviewer needs
// on one screen, same content cmd/admin/reports.go's `show` prints to a
// terminal.
type reportDetail struct {
	Report     report.Report `json:"report"`
	AuditTrail []audit.Entry `json:"auditTrail"`
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rep, err := h.reports.Get(id)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "No report with that id.")
		return
	}
	trail, err := h.audit.ForTarget(rep.ReportedID)
	if err != nil {
		trail = nil // best-effort: a missing trail must not hide the report itself
	}
	httpx.JSON(w, http.StatusOK, reportDetail{Report: rep, AuditTrail: trail})
}

// review is the HTTP equivalent of `admin reports action|action-false|dismiss`.
// The reviewer identity comes from the admin key holder's own name, sent by
// the client -- there's no per-reviewer login yet (see the Flutter admin
// feature's "reviewer name" field), only a single shared operator key, so
// this is the one place that identity has to be self-reported rather than
// derived from a session.
func (h *Handler) review(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Status       string `json:"status"`
		Resolution   string `json:"resolution"`
		MarkFalseSOS bool   `json:"markFalseSos"`
		ReviewerName string `json:"reviewerName"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	reviewer := req.ReviewerName
	if reviewer == "" {
		reviewer = "admin-dashboard"
	}

	err := h.reports.Review(id, reviewer, report.ReviewRequest{
		Status:       req.Status,
		Resolution:   req.Resolution,
		MarkFalseSOS: req.MarkFalseSOS,
	})
	switch {
	case errors.Is(err, report.ErrNotFound):
		httpx.Err(w, http.StatusNotFound, "No report with that id.")
		return
	case errors.Is(err, report.ErrAlreadyReviewed):
		httpx.Err(w, http.StatusConflict, "That report was already reviewed.")
		return
	case errors.Is(err, report.ErrInvalidReview):
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not save the review.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// suspendHelper is the §6 fast-track exception surfaced over HTTP: a
// credible report of a helper endangering a requester suspends them
// immediately, before any report review completes, and force-releases any
// SOS they currently hold back to the standby queue.
func (h *Handler) suspendHelper(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	var req struct {
		Reason       string `json:"reason"`
		ReviewerName string `json:"reviewerName"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.Reason == "" {
		httpx.Err(w, http.StatusBadRequest, "A reason is required.")
		return
	}
	actor := req.ReviewerName
	if actor == "" {
		actor = "admin-dashboard"
	}

	if h.suspend == nil {
		httpx.Err(w, http.StatusInternalServerError, "Suspension is not wired up on this server.")
		return
	}
	released, err := h.suspend(uid, req.Reason, actor)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not suspend that helper.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"releasedSosId": released})
}

func (h *Handler) verificationQueue(w http.ResponseWriter, r *http.Request) {
	if h.verifications == nil {
		httpx.Err(w, http.StatusInternalServerError, "Helper verification is not wired up on this server.")
		return
	}
	list, err := h.verifications.ListAll()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not load helper verification status.")
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (h *Handler) verificationDetail(w http.ResponseWriter, r *http.Request) {
	if h.verifications == nil {
		httpx.Err(w, http.StatusInternalServerError, "Helper verification is not wired up on this server.")
		return
	}
	rv, err := h.verifications.GetForReview(r.PathValue("id"))
	if errors.Is(err, verification.ErrNotFound) {
		httpx.Err(w, http.StatusNotFound, "No verification with that id.")
		return
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not load helper verification.")
		return
	}
	httpx.JSON(w, http.StatusOK, rv)
}

func (h *Handler) verificationImage(w http.ResponseWriter, r *http.Request) {
	if h.verifications == nil {
		httpx.Err(w, http.StatusInternalServerError, "Helper verification is not wired up on this server.")
		return
	}
	rv, err := h.verifications.GetForReview(r.PathValue("id"))
	if errors.Is(err, verification.ErrNotFound) {
		httpx.Err(w, http.StatusNotFound, "No verification with that id.")
		return
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not load helper verification.")
		return
	}
	name := ""
	switch strings.ToLower(r.PathValue("kind")) {
	case "front":
		name = rv.NIDFront
	case "back":
		name = rv.NIDBack
	case "selfie":
		name = rv.Selfie
	default:
		httpx.Err(w, http.StatusBadRequest, "Unknown verification image.")
		return
	}
	data, err := os.ReadFile(filepath.Join(h.uploadDir, rv.ID, name))
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "Verification image not found.")
		return
	}
	contentType := "image/jpeg"
	if strings.HasSuffix(strings.ToLower(name), ".png") {
		contentType = "image/png"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) verificationDecision(w http.ResponseWriter, r *http.Request) {
	if h.verifications == nil {
		httpx.Err(w, http.StatusInternalServerError, "Helper verification is not wired up on this server.")
		return
	}
	var req struct {
		Approved     bool   `json:"approved"`
		Note         string `json:"note"`
		ReviewerName string `json:"reviewerName"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if !req.Approved && strings.TrimSpace(req.Note) == "" {
		httpx.Err(w, http.StatusBadRequest, "A rejection reason is required.")
		return
	}
	err := h.verifications.Decide(r.PathValue("id"), req.Approved, strings.TrimSpace(req.Note), time.Now().UTC())
	switch {
	case errors.Is(err, verification.ErrNotFound):
		httpx.Err(w, http.StatusNotFound, "No verification with that id.")
	case errors.Is(err, verification.ErrNotPending):
		httpx.Err(w, http.StatusConflict, "That verification was already decided.")
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not save the verification decision.")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
