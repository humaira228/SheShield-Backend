package contact

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
)

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) Register(mux *http.ServeMux, jwtSecret string) {
	auth := middleware.RequireAuth(jwtSecret)
	mux.Handle("GET /api/v1/contacts", auth(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/v1/contacts", auth(http.HandlerFunc(h.create)))
	mux.Handle("DELETE /api/v1/contacts/{id}", auth(http.HandlerFunc(h.delete)))
	mux.Handle("POST /api/v1/contacts/{id}/invite", auth(http.HandlerFunc(h.invite)))
	mux.Handle("POST /api/v1/contacts/accept-invite", auth(http.HandlerFunc(h.acceptInvite)))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	contacts, err := h.repo.ListForUser(uid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not load contacts.")
		return
	}
	httpx.JSON(w, http.StatusOK, contacts)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	var req CreateContactRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	req, msg := normalize(req)
	if msg != "" {
		httpx.Err(w, http.StatusBadRequest, msg)
		return
	}
	count, err := h.repo.CountForUser(uid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not save contact.")
		return
	}
	if count >= MaxContacts {
		httpx.Err(w, http.StatusBadRequest, fmt.Sprintf("You can add up to %d trusted contacts.", MaxContacts))
		return
	}
	dup, err := h.repo.Exists(uid, req.CountryCode, req.Phone)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not save contact.")
		return
	}
	if dup {
		httpx.Err(w, http.StatusConflict, "That number is already in your contacts.")
		return
	}
	c, err := h.repo.Create(uid, req)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not save contact.")
		return
	}
	httpx.JSON(w, http.StatusCreated, c)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	id := r.PathValue("id")
	if err := h.repo.Delete(uid, id); err != nil {
		httpx.Err(w, http.StatusNotFound, "Contact not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// invite issues a short code the contact enters (after installing SheShield
// and signing up) to link their own account -- see acceptInvite. Until they
// do, this contact can only be reached by SMS.
func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	id := r.PathValue("id")
	resp, err := h.repo.CreateInvite(uid, id)
	if errors.Is(err, ErrNotFound) {
		httpx.Err(w, http.StatusNotFound, "Contact not found.")
		return
	}
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not create invite.")
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) acceptInvite(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	var req AcceptInviteRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if code == "" {
		httpx.Err(w, http.StatusBadRequest, "Enter the invite code.")
		return
	}
	contactID, _, err := h.repo.AcceptInvite(code, uid)
	switch {
	case errors.Is(err, ErrInviteInvalid):
		httpx.Err(w, http.StatusBadRequest, "This invite code is invalid or has expired.")
		return
	case errors.Is(err, ErrCannotAcceptOwnInvite):
		httpx.Err(w, http.StatusBadRequest, "You cannot accept your own invite.")
		return
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not accept invite.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"contactId": contactID})
}
