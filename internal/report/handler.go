package report

import (
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

// Register wires the person-facing routes. The moderation queue/review
// routes are deliberately not here -- see RegisterAdmin, gated separately
// so a regular user token can never reach them.
func (h *Handler) Register(mux *http.ServeMux, jwtSecret string) {
	auth := middleware.RequireAuth(jwtSecret)
	mux.Handle("POST /api/v1/reports", auth(http.HandlerFunc(h.file)))
	mux.Handle("POST /api/v1/blocks", auth(http.HandlerFunc(h.block)))
	mux.Handle("DELETE /api/v1/blocks/{userId}", auth(http.HandlerFunc(h.unblock)))
	mux.Handle("GET /api/v1/blocks", auth(http.HandlerFunc(h.listBlocks)))
}

func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())

	var req CreateReportRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	// role: whichever side the reporter is filing as. The caller states it
	// (a dual-role userHelper account can file either way); the server
	// doesn't infer it from user_type, since the report is about a specific
	// interaction, not the account's general type.
	role := r.URL.Query().Get("role")
	if role != "helper" {
		role = "user"
	}

	rep, err := h.svc.File(uid, role, req)
	switch {
	case errors.Is(err, ErrInvalidCategory), errors.Is(err, ErrCannotReportSelf):
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not file the report.")
		return
	}
	httpx.JSON(w, http.StatusCreated, rep)
}

func (h *Handler) block(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	var req struct {
		UserID string `json:"userId"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.UserID == "" {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if err := h.svc.Block(uid, req.UserID); err != nil {
		if errors.Is(err, ErrCannotReportSelf) {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.Err(w, http.StatusInternalServerError, "Could not block that account.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unblock(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	blockedID := r.PathValue("userId")
	if err := h.svc.Unblock(uid, blockedID); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not remove the block.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listBlocks(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	blocks, err := h.svc.ListMyBlocks(uid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not load your blocked list.")
		return
	}
	httpx.JSON(w, http.StatusOK, blocks)
}
