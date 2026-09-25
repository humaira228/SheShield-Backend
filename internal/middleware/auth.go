package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zannatulmaliha/sheshield-backend/internal/httpx"
)

type contextKey string

const userUIDKey contextKey = "userUID"

// IssueToken creates a signed JWT for uid, valid for ttlHours.
func IssueToken(secret string, uid string, ttlHours int) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   uid,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(ttlHours) * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// RequireAuth validates the Bearer token and puts the user's uid on the
// request context. Every handler that needs "who is calling" reads it via
// UIDFromContext — no handler parses the token itself.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				httpx.Err(w, http.StatusUnauthorized, "Missing or malformed Authorization header.")
				return
			}
			raw := strings.TrimPrefix(header, "Bearer ")

			claims := &jwt.RegisteredClaims{}
			token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
				return []byte(secret), nil
			}, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil || !token.Valid {
				httpx.Err(w, http.StatusUnauthorized, "Session expired. Please sign in again.")
				return
			}

			ctx := context.WithValue(r.Context(), userUIDKey, claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UIDFromContext(ctx context.Context) (string, bool) {
	uid, ok := ctx.Value(userUIDKey).(string)
	return uid, ok
}

// RequireAdminKey gates the moderation-dashboard routes (internal/adminapi)
// behind a single static, operator-issued key -- a different, deliberately
// simpler scheme than RequireAuth's JWTs. There is no "admin" role on the
// users table; a regular user's Bearer token, however long-lived or
// privileged the account, is never accepted here. If key is empty (the
// config default), every request is rejected -- the admin HTTP surface is
// opt-in, not opt-out, matching cmd/admin's original "no network endpoint
// unless someone deliberately wires one up" posture.
//
// Constant-time comparison (crypto/subtle) so responding to a wrong key
// takes the same time as a right one -- a timing side-channel is a real
// attack surface for a single shared secret checked on every request.
func RequireAdminKey(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == "" {
				httpx.Err(w, http.StatusNotFound, "Not found.")
				return
			}
			given := r.Header.Get("X-Admin-Key")
			if given == "" || subtle.ConstantTimeCompare([]byte(given), []byte(key)) != 1 {
				httpx.Err(w, http.StatusUnauthorized, "Invalid admin key.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}