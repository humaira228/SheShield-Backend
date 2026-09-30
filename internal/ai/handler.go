package ai

import (
	"log"
	"net/http"

	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
	"github.com/zannatulmaliha/sheshield-backend/internal/middleware"
)

const maxHistoryTurns = 20 // caps request size/cost; the client only needs recent context

type Handler struct {
	client Client
}

// NewHandler takes a nil client when GROQ_API_KEY isn't set, so the route
// still exists and returns a clear error instead of the app getting a
// generic 404/network failure.
func NewHandler(client Client) *Handler {
	return &Handler{client: client}
}

func (h *Handler) Register(mux *http.ServeMux, jwtSecret string) {
	auth := middleware.RequireAuth(jwtSecret)
	mux.Handle("POST /api/v1/ai/chat", auth(http.HandlerFunc(h.chat)))
}

type chatRequest struct {
	History []Message `json:"history"`
}

type chatResponse struct {
	Reply string `json:"reply"`
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		httpx.Err(w, http.StatusServiceUnavailable, "The AI assistant isn't configured yet.")
		return
	}

	var req chatRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	if len(req.History) == 0 {
		httpx.Err(w, http.StatusBadRequest, "history must contain at least one message.")
		return
	}
	if len(req.History) > maxHistoryTurns {
		req.History = req.History[len(req.History)-maxHistoryTurns:]
	}
	for _, m := range req.History {
		if m.Role != "user" && m.Role != "assistant" {
			httpx.Err(w, http.StatusBadRequest, "each message role must be \"user\" or \"assistant\".")
			return
		}
	}

	reply, err := h.client.Reply(r.Context(), req.History)
	if err != nil {
		log.Printf("ai: provider request failed: %v", err)
		httpx.Err(w, http.StatusBadGateway, "The AI assistant couldn't respond. Please try again.")
		return
	}
	httpx.JSON(w, http.StatusOK, chatResponse{Reply: reply})
}
