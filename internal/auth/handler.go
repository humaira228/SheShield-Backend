package auth

import (
	"errors"
	"net/http"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register wires this feature's routes onto mux. Contacts/SOS features do
// the same in their own Register — main.go just calls each one.
func (h *Handler) Register(mux *http.ServeMux, jwtSecret string) {
	mux.HandleFunc("POST /api/v1/auth/signup", h.signUp)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	requireAuth := middleware.RequireAuth(jwtSecret)
	mux.Handle("GET /api/v1/auth/me", requireAuth(http.HandlerFunc(h.me)))
	mux.Handle("PATCH /api/v1/auth/me", requireAuth(http.HandlerFunc(h.updateMe)))
	mux.Handle("PATCH /api/v1/auth/fcm-token", requireAuth(http.HandlerFunc(h.updateFCMToken)))
}

func (h *Handler) signUp(w http.ResponseWriter, r *http.Request) {
	var req SignUpRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	resp, err := h.service.SignUp(req)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req SignInRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	resp, err := h.service.SignIn(req)
	if err != nil {
		httpx.Err(w, http.StatusUnauthorized, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	user, err := h.service.Me(uid)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "User not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())

	var req UpdateProfileRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	user, err := h.service.UpdateProfile(uid, req)
	var invalid ValidationError
	switch {
	case errors.As(err, &invalid):
		httpx.Err(w, http.StatusBadRequest, invalid.Error())
		return
	case errors.Is(err, ErrNotFound):
		httpx.Err(w, http.StatusNotFound, "User not found.")
		return
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not update your profile.")
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

func (h *Handler) updateFCMToken(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	var req UpdateFCMTokenRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if err := h.service.UpdateFCMToken(uid, req.Token); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not save push token.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"updated": true})
}
