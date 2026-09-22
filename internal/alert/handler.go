package alert

import (
	"context"
	"errors"
	"net/http"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux, jwtSecret string) {
	auth := middleware.RequireAuth(jwtSecret)
	mux.Handle("POST /api/v1/alerts", auth(http.HandlerFunc(h.create)))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())

	var req CreateAlertRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	// If the phone loses signal or the app is closed mid-request, the texts
	// must still go out -- so sending must not be tied to the request's
	// lifetime.
	ctx := context.WithoutCancel(r.Context())

	alert, err := h.svc.Trigger(ctx, uid, req)
	switch {
	case errors.Is(err, ErrNoContacts), errors.Is(err, ErrBadLocation):
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not send the alert.")
		return
	}
	httpx.JSON(w, http.StatusCreated, alert)
}
