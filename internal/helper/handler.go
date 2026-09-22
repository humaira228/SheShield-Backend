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
