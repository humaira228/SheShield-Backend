package verification

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
)

// The whole request is capped so nobody can stream gigabytes at the server:
// three photos of at most MaxImageBytes each, plus a little form overhead.
const maxRequestBytes = 3*MaxImageBytes + (1 << 20)

type Handler struct {
	svc *Service

	// uid reads the authenticated user's id. A field, so tests can supply one
	// without minting a real login token.
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
	mux.Handle("POST /api/v1/verification", auth(http.HandlerFunc(h.submit)))
	mux.Handle("GET /api/v1/verification", auth(http.HandlerFunc(h.status)))
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)
	resp, err := h.svc.Status(uid)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "Could not load your verification status.")
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func readPart(r *http.Request, field string) ([]byte, error) {
	f, _, err := r.FormFile(field)
	if err != nil {
		return nil, err
	}
	defer func(f multipart.File) { _ = f.Close() }(f)
	// +1 so an oversized file is detected rather than silently truncated.
	return io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	uid, _ := h.uid(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Could not read the upload. Each photo must be under 5 MB.")
		return
	}
	defer r.MultipartForm.RemoveAll() // delete any temp files Go spooled to disk

	var files Files
	for field, dst := range map[string]*[]byte{
		"nidFront": &files.NIDFront,
		"nidBack":  &files.NIDBack,
		"selfie":   &files.Selfie,
	} {
		data, err := readPart(r, field)
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, "Please add all three photos: the front and back of your ID, and a selfie.")
			return
		}
		*dst = data
	}

	resp, err := h.svc.Submit(uid, files)
	var imgErr ImageError
	switch {
	case errors.As(err, &imgErr):
		httpx.Err(w, http.StatusBadRequest, imgErr.Error())
	case errors.Is(err, ErrNotHelper):
		httpx.Err(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrAlreadyVerified), errors.Is(err, ErrAlreadyPending):
		httpx.Err(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrTooManyAttempts):
		httpx.Err(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		httpx.Err(w, http.StatusInternalServerError, "Could not save your documents. Please try again.")
	default:
		httpx.JSON(w, http.StatusCreated, resp)
	}
}
