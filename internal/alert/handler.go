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
	mux.Handle("PATCH /api/v1/alerts/{id}/location", auth(http.HandlerFunc(h.updateLocation)))
	mux.Handle("PATCH /api/v1/alerts/{id}/resolve", auth(http.HandlerFunc(h.resolve)))
}

// RegisterPublic wires the routes a trusted contact opens with no login: the
// JSON feed the tracking page polls, and the tracking page itself. Kept
// separate from Register (and called directly against mux in cmd/api/main.go,
// never through middleware.RequireAuth) so it is obvious at the call site
// that these two are intentionally open to anyone with the link.
func (h *Handler) RegisterPublic(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/public/alerts/{token}", h.publicView)
	mux.HandleFunc("GET /track/{token}", h.trackingPage)
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

// writeTrackingError maps the sentinel errors UpdateLocation/Resolve can
// return to the status codes the Flutter app expects: 404 when the alert
// doesn't exist or isn't the caller's (the two are indistinguishable on
// purpose, see ErrNotFound), 409 once it's no longer active.
func writeTrackingError(w http.ResponseWriter, err error, notActiveMessage string) {
	switch {
	case errors.Is(err, ErrBadLocation):
		httpx.Err(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.Err(w, http.StatusNotFound, "Alert not found.")
	case errors.Is(err, ErrAlertNotActive):
		httpx.Err(w, http.StatusConflict, notActiveMessage)
	default:
		httpx.Err(w, http.StatusInternalServerError, "Something went wrong. Please try again.")
	}
}

func (h *Handler) updateLocation(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	id := r.PathValue("id")

	var req UpdateLocationRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}

	if err := h.svc.UpdateLocation(id, uid, req.Latitude, req.Longitude, req.AccuracyMeters); err != nil {
		writeTrackingError(w, err, "This SOS is no longer active.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	uid, _ := middleware.UIDFromContext(r.Context())
	id := r.PathValue("id")

	if err := h.svc.Resolve(id, uid); err != nil {
		writeTrackingError(w, err, "This SOS was already resolved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// publicView is the JSON feed the tracking page polls every few seconds.
// No auth: the share token itself is the credential.
func (h *Handler) publicView(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	view, err := h.svc.PublicView(token)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.Err(w, http.StatusNotFound, "Tracking link not found.")
		return
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not load tracking info.")
		return
	}
	httpx.JSON(w, http.StatusOK, view)
}

// trackingPage serves the plain HTML page a contact opens from the SMS link.
// It does not itself look up the alert -- the page's own JS calls publicView
// above -- so an unknown token still renders the page (which then shows its
// own "not found" state) rather than a bare 404 with no explanation.
func (h *Handler) trackingPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderTrackingPage(token)))
}
