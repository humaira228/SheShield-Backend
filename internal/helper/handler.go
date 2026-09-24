package helper

import (
	"errors"
	"net/http"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
)

type Handler struct {
	svc *Service

	// uid reads the authenticated user's id. A field, so tests can supply
	// one without minting a real login token.
	uid func(r *http.Request) (string, bool)
}

func NewHandler(svc *Service) *Handler {
	return &Handler{
		svc: svc,
		uid: func(r *http.Request) (string, bool) { return middleware.UIDFromContext(r.Context()) },
	}
}

func (h *Handler) Register(mux *http.ServeMux, jwtSecret string) {
	auth := middleware.RequireAuth(jwtSecret)
	mux.Handle("GET /api/v1/helper/status", auth(http.HandlerFunc(h.getStatus)))
	mux.Handle("PUT /api/v1/helper/status", auth(http.HandlerFunc(h.setStatus)))
	mux.Handle("GET /api/v1/helper/alerts/nearby", auth(http.HandlerFunc(h.nearby)))
	mux.Handle("POST /api/v1/helper/alerts/{id}/accept", auth(http.HandlerFunc(h.accept)))
	mux.Handle("POST /api/v1/helper/alerts/{id}/release", auth(http.HandlerFunc(h.release)))
	mux.Handle("GET /api/v1/helper/alerts/{id}/safety-status", auth(http.HandlerFunc(h.safetyStatus)))
}

func writeSvcError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotHelper):
		httpx.Err(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrNotVerified):
		httpx.Err(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrLocationNeeded), errors.Is(err, ErrInvalidRadius):
		httpx.Err(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotActive):
		httpx.Err(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNotYourMatch):
		httpx.Err(w, http.StatusConflict, err.Error())
	default:
		httpx.Err(w, http.StatusInternalServerError, "Something went wrong. Please try again.")
	}
}

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	st, err := h.svc.Status(uid)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)

	var req SetStatusRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	st, err := h.svc.SetStatus(uid, req)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

func (h *Handler) nearby(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	alerts, err := h.svc.NearbyAlerts(uid)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, alerts)
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	alertID := r.PathValue("id")

	accepted, err := h.svc.Accept(uid, alertID)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	if accepted == nil {
		httpx.Err(w, http.StatusConflict, ErrAlreadyAccepted.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, accepted)
}

// release lets the accepted helper back out of an SOS they're currently
// holding -- "can decline/back out if the situation seems unsafe or
// suspicious" per the spec's §2. Reopens the alert for the standby helpers.
func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	alertID := r.PathValue("id")

	if err := h.svc.Release(uid, alertID); err != nil {
		writeSvcError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// safetyStatus lets the accepted helper poll the live duress/connectivity
// signals for the alert they're responding to -- see the spec's §8.
func (h *Handler) safetyStatus(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	alertID := r.PathValue("id")

	duressActive, connectivityLost, err := h.svc.SafetyStatus(uid, alertID)
	if err != nil {
		writeSvcError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{
		"duressActive":     duressActive,
		"connectivityLost": connectivityLost,
	})
}
